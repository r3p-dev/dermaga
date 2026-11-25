// Package main is the entry point for dermaga.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"dermaga/internal/access"
	"dermaga/internal/auth"
	"dermaga/internal/config"
	"dermaga/internal/crypto"
	"dermaga/internal/doctor"
	"dermaga/internal/host"
	"dermaga/internal/reconciler"
	"dermaga/internal/server"
	"dermaga/internal/store"
	"dermaga/internal/webhook"
)

// Version and Commit are set at build time via -ldflags.
// Default "dev" indicates a non-release build fallback.
var (
	Version = "dev"
	Commit  = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "daemon":
		err = runDaemon(args)
	case "admin":
		err = runAdmin(args, os.Stdin)
	case "panel":
		err = runPanel(args)
	case "doctor":
		err = runDoctor()
	case "install-settings":
		err = runInstallSettings(args)
	case "reconcile":
		err = runReconcile(args)
	case "version", "--version", "-v":
		fmt.Printf("dermaga version %s (commit %s)\n", Version, Commit)
	case "hook-relay":
		err = runHookRelay(args)
	case "probe":
		err = runProbe(args)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`dermaga - autonomous server platform and web panel (version %s)

Usage:
  dermaga <command> [arguments]

Commands:
  daemon               Run the long-running dermaga daemon and panel listener
  admin set-password   Set or reset the admin password (recovery)
  admin totp disable   Disable TOTP two-factor authentication (recovery)
  panel                Display laptop connection instructions and setup link
  reconcile            Reconcile live units and routing configuration per DEP-10
  doctor               Run the host health checks (DOC-1)
  version              Print version information

Internal Helpers:
  hook-relay           Forward TCP webhook traffic to local Unix domain socket
  probe                Perform internal HTTP health probe via custom DNS
  install-settings     Record the SSH address, port and login key (used by the installer)
`, Version)
}

func runDaemon(args []string) error {
	paths := config.ResolvePaths()

	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7070", "Address to listen for HTTP panel access")
	dbPath := fs.String("db", paths.DBPath, "Path to SQLite database")
	keyPath := fs.String("key", paths.KeyPath, "Path to master encryption key")
	sockPath := fs.String("socket", paths.DaemonSock, "Path to recovery Unix domain socket")
	_ = fs.Parse(args)

	// SEC-1: Refuse to run as root
	if err := server.EnsureNonRoot(); err != nil {
		return err
	}

	fmt.Printf("Starting dermaga daemon (version %s, commit %s)...\n", Version, Commit)

	// Load master encryption key
	masterKey, err := crypto.LoadOrCreateMasterKey(*keyPath)
	if err != nil {
		return fmt.Errorf("loading master key: %w", err)
	}

	// Open database and run migrations
	st, err := store.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer st.Close()

	// DEP-10: Self-healing startup reconciliation
	execPath, _ := os.Executable()
	rec := reconciler.New(host.NewRealHost(), st, reconciler.Config{
		QuadletDir:         paths.QuadletDir,
		TraefikDynamicDir:  paths.TraefikDynamicDir,
		UserUnitDir:        paths.UserUnitDir,
		TraefikDir:         paths.TraefikDir,
		HookRelayRootfsDir: filepath.Join(filepath.Dir(*dbPath), "hook-relay-rootfs"),
		BinaryPath:         execPath,
		SocketDir:          paths.SocketDir,
	})
	if recReport, err := rec.Reconcile(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: startup reconciliation encountered error: %v\n", err)
	} else if len(recReport.OrphansRemoved) > 0 || len(recReport.FilesReRendered) > 0 || len(recReport.UnitsStarted) > 0 || len(recReport.VerifyRoutersCleaned) > 0 {
		fmt.Printf("Startup reconciliation complete: %d orphan(s) removed, %d file(s) re-rendered, %d unit(s) started, %d verify router(s) cleaned\n",
			len(recReport.OrphansRemoved), len(recReport.FilesReRendered), len(recReport.UnitsStarted), len(recReport.VerifyRoutersCleaned))
	}

	srv, err := server.NewServer(server.Config{
		ListenAddr: *addr,
		SocketPath: *sockPath,
		Store:      st,
		Reconciler: rec,
		MasterKey:  masterKey,
		Version:    Version,
		Commit:     Commit,
	})
	if err != nil {
		return fmt.Errorf("initializing server: %w", err)
	}

	// Record daemon startup in audit log
	_ = st.RecordAudit(context.Background(), "daemon", "system", "daemon_start",
		fmt.Sprintf("Daemon started (version %s, commit %s)", Version, Commit), nil, nil)

	hookSrv := webhook.New(webhook.Config{
		SocketPath: paths.HookSock,
		Store:      st,
		MasterKey:  masterKey,
	})

	// Signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	errChan := make(chan error, 1)
	go func() {
		fmt.Printf("Panel listening on http://%s (local only, SEC-2)\n", *addr)
		if *sockPath != "" {
			fmt.Printf("Recovery socket listening on %s (0600, SRV-2)\n", *sockPath)
		}
		errChan <- srv.Start()
	}()
	go func() {
		if paths.HookSock != "" {
			fmt.Printf("Webhook socket listening on %s (0600, SEC-11)\n", paths.HookSock)
			if err := hookSrv.Start(); err != nil && !strings.Contains(err.Error(), "Server closed") {
				errChan <- err
			}
		}
	}()

	select {
	case sig := <-sigChan:
		fmt.Printf("\nReceived signal %v, shutting down gracefully...\n", sig)
		_ = hookSrv.Close()
		return srv.Close()
	case err := <-errChan:
		_ = hookSrv.Close()
		if err != nil && !strings.Contains(err.Error(), "Server closed") {
			return err
		}
		return nil
	}
}

func readPassword(in io.Reader) (string, error) {
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Print("Enter new admin password: ")
		passBytes, err := term.ReadPassword(int(f.Fd()))
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("reading password: %w", err)
		}
		password := strings.TrimRight(string(passBytes), "\r\n")
		if password == "" {
			return "", errors.New("password cannot be empty")
		}

		fmt.Print("Confirm password: ")
		confirmBytes, err := term.ReadPassword(int(f.Fd()))
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("reading confirmation: %w", err)
		}
		confirm := strings.TrimRight(string(confirmBytes), "\r\n")
		if password != confirm {
			return "", errors.New("passwords do not match")
		}
		return password, nil
	}

	// Non-TTY / piped input: read one line from stdin
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", fmt.Errorf("reading password from stdin: %w", err)
	}
	password := strings.TrimRight(line, "\r\n")
	if password == "" {
		return "", errors.New("password cannot be empty")
	}
	return password, nil
}

func runAdmin(args []string, in io.Reader) error {
	if len(args) == 0 {
		fmt.Println("Usage: dermaga admin <set-password|totp disable>")
		return nil
	}

	paths := config.ResolvePaths()

	switch args[0] {
	case "set-password":
		if len(args) > 1 {
			return fmt.Errorf("set-password takes no arguments; input password via prompt or stdin")
		}

		password, err := readPassword(in)
		if err != nil {
			return err
		}

		if len(password) < 8 {
			return fmt.Errorf("password must be at least 8 characters long")
		}

		// Try communicating over daemon socket first (SRV-2)
		resp, err := callDaemon(paths.DaemonSock, server.RecoveryRequest{
			Action:   "set-password",
			Password: password,
		})
		if err == nil && resp.Success {
			fmt.Printf("Success: %s\n", resp.Message)
			return nil
		}

		// Fallback to direct SQLite access (SRV-2)
		fmt.Println("Daemon inactive or unreachable over socket; accessing SQLite database directly (SRV-2 fallback)...")
		st, err := store.Open(paths.DBPath)
		if err != nil {
			return fmt.Errorf("opening database directly: %w", err)
		}
		defer st.Close()

		if err := auth.SetAdminPassword(context.Background(), st, "server_command", password); err != nil {
			return fmt.Errorf("setting password: %w", err)
		}
		fmt.Println("Success: Admin password updated and all sessions revoked.")
		return nil

	case "totp":
		if len(args) > 1 && args[1] == "disable" {
			// Try communicating over daemon socket first (SRV-2)
			resp, err := callDaemon(paths.DaemonSock, server.RecoveryRequest{
				Action: "totp-disable",
			})
			if err == nil && resp.Success {
				fmt.Printf("Success: %s\n", resp.Message)
				return nil
			}

			// Fallback to direct SQLite access
			fmt.Println("Daemon inactive or unreachable over socket; accessing SQLite database directly (SRV-2 fallback)...")
			st, err := store.Open(paths.DBPath)
			if err != nil {
				return fmt.Errorf("opening database directly: %w", err)
			}
			defer st.Close()

			if err := auth.DisableAdminTOTP(context.Background(), st, "server_command"); err != nil {
				return fmt.Errorf("disabling TOTP: %w", err)
			}
			fmt.Println("Success: TOTP two-factor authentication disabled.")
			return nil
		}
		return fmt.Errorf("unknown admin totp command: %v", args)

	default:
		return fmt.Errorf("unknown admin command: %s", args[0])
	}
}

// accessTarget reads the server address, SSH port and login key that the installer stored (INST-6).
func accessTarget(ctx context.Context, st *store.Store) (access.Target, error) {
	var t access.Target
	var err error
	if t.Address, err = st.GetSetting(ctx, access.SettingAddress); err != nil {
		return t, fmt.Errorf("the server address is not recorded yet; run the installer again: %w", err)
	}
	port, err := st.GetSetting(ctx, access.SettingPort)
	if err != nil {
		return t, fmt.Errorf("the SSH port is not recorded yet; run the installer again: %w", err)
	}
	if t.Port, err = strconv.Atoi(port); err != nil {
		return t, fmt.Errorf("stored SSH port %q is not a number", port)
	}
	t.KeyFingerprint, _ = st.GetSetting(ctx, access.SettingKeyFingerprint) // optional
	if t.ServerHostname, err = shortHostname(); err != nil {
		return t, err
	}
	return t, t.Validate()
}

// shortHostname is the lowercase first label of the hostname, used in the dermaga-<hostname> alias.
func shortHostname() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("reading the hostname: %w", err)
	}
	return strings.ToLower(strings.SplitN(hostname, ".", 2)[0]), nil
}

// runInstallSettings is called by the installer (as user server) to record what it asked (INST-6).
func runInstallSettings(args []string) error {
	paths := config.ResolvePaths()
	fs := flag.NewFlagSet("install-settings", flag.ContinueOnError)
	address := fs.String("ssh-address", "", "public IP or DNS name of the server")
	port := fs.Int("ssh-port", 0, "SSH port")
	fingerprint := fs.String("login-key-fingerprint", "", "SHA256 fingerprint of the login key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	hostname, err := shortHostname()
	if err != nil {
		return err
	}
	t := access.Target{Address: *address, Port: *port, ServerHostname: hostname, KeyFingerprint: *fingerprint}
	if err := t.Validate(); err != nil {
		return err
	}
	st, err := store.Open(paths.DBPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer st.Close()
	ctx := context.Background()
	for key, value := range map[string]string{
		access.SettingAddress:        *address,
		access.SettingPort:           strconv.Itoa(*port),
		access.SettingKeyFingerprint: *fingerprint,
	} {
		if err := st.SetSetting(ctx, key, value); err != nil {
			return fmt.Errorf("saving %s: %w", key, err)
		}
	}
	return nil
}

// runPanel prints the laptop command (LAP-1), setup link if uninitialized (SETUP-3), and panel URL (INST-12, SRV-1).
func runPanel(args []string) error {
	paths := config.ResolvePaths()

	// dermaga panel requires the daemon (SRV-2)
	resp, err := callDaemon(paths.DaemonSock, server.RecoveryRequest{Action: "status"})
	if err != nil || !resp.Success {
		fmt.Printf("The dermaga daemon is not reachable at %s.\n", paths.DaemonSock)
		fmt.Println("Check daemon status or start it with:")
		fmt.Println("  systemctl --user status dermaga")
		fmt.Println("  systemctl --user start dermaga")
		return fmt.Errorf("dermaga daemon is not reachable at %s", paths.DaemonSock)
	}

	st, err := store.Open(paths.DBPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer st.Close()
	t, err := accessTarget(context.Background(), st)
	if err != nil {
		return err
	}
	cmd, err := access.LaptopCommand(t)
	if err != nil {
		return err
	}

	fmt.Println("On your laptop, paste this command once (bash, zsh or fish):")
	fmt.Println()
	fmt.Println(cmd)
	fmt.Println()
	fmt.Printf("It adds a \"Host dermaga\" entry for server@%s port %d to ~/.ssh/config.\n", t.Address, t.Port)

	tokenResp, tokenErr := callDaemon(paths.DaemonSock, server.RecoveryRequest{Action: "setup-token"})
	if tokenErr == nil && tokenResp.Success && tokenResp.Token != "" {
		fmt.Printf("From then on: ssh dermaga, then open the first-run setup link (expires in 24 h):\n")
		fmt.Printf("  %s/setup?token=%s\n", access.PanelURL, tokenResp.Token)
	} else {
		fmt.Printf("From then on: ssh dermaga, then open %s in your browser.\n", access.PanelURL)
	}

	fmt.Println()
	fmt.Println("Lost this? Run `dermaga panel` on the server.")
	return nil
}

func callDaemon(sockPath string, req server.RecoveryRequest) (*server.RecoveryResponse, error) {
	conn, err := net.DialTimeout("unix", sockPath, 1*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return nil, err
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}

	var resp server.RecoveryResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// runHookRelay implements the internal hook relay proxy (ported from Spike 3.1).
// It parses nothing and forwards raw bytes, capping inbound bytes per connection
// at 25 MiB + 64 KiB (matching GitHub's 25 MiB payload limit plus HTTP headers).
func runHookRelay(args []string) error {
	fs := flag.NewFlagSet("hook-relay", flag.ExitOnError)
	listenAddr := fs.String("listen", ":8080", "TCP address to listen on")
	sockPath := fs.String("socket", "/run/dermaga/hooks.sock", "Unix socket destination")
	_ = fs.Parse(args)

	const maxRelayBytes = (25 << 20) + (64 << 10)

	l, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", *listenAddr, err)
	}
	defer l.Close()

	fmt.Printf("Hook relay proxy forwarding %s -> %s\n", *listenAddr, *sockPath)

	for {
		clientConn, err := l.Accept()
		if err != nil {
			return err
		}

		go func(c net.Conn) {
			defer c.Close()
			unixConn, err := net.Dial("unix", *sockPath)
			if err != nil {
				return
			}
			defer unixConn.Close()

			done := make(chan struct{}, 2)
			go func() {
				_, _ = io.Copy(unixConn, io.LimitReader(c, maxRelayBytes))
				done <- struct{}{}
			}()
			go func() {
				_, _ = io.Copy(c, unixConn)
				done <- struct{}{}
			}()
			<-done
		}(clientConn)
	}
}

// runProbe implements the rootless container network namespace probe (ported from Spike 3.2).
func runProbe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	dnsServer := fs.String("dns", "10.89.0.1:53", "DNS server to resolve container names (aardvark-dns)")
	timeout := fs.Duration("timeout", 5*time.Second, "probe timeout")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: dermaga probe [-dns ip:53] <url>")
	}
	targetURL := fs.Arg(0)

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 2 * time.Second}
			return d.DialContext(ctx, "udp", *dnsServer)
		},
	}

	dialer := &net.Dialer{
		Resolver:  resolver,
		Timeout:   3 * time.Second,
		KeepAlive: -1,
	}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ResponseHeaderTimeout: 3 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   *timeout,
	}

	start := time.Now()
	resp, err := client.Get(targetURL)
	if err != nil {
		return fmt.Errorf("probe request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	duration := time.Since(start)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	fmt.Printf("HTTP %d (%s) in %v\n", resp.StatusCode, resp.Status, duration)
	fmt.Printf("Body: %s\n", string(body))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("health probe returned non-2xx status: %d", resp.StatusCode)
}

// runDoctor prints the health checks and fails when one of them fails (INST-11).
func runDoctor() error {
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("reading the current user: %w", err)
	}
	paths := config.ResolvePaths()
	var st *store.Store
	if _, statErr := os.Stat(paths.DBPath); statErr == nil {
		if opened, openErr := store.Open(paths.DBPath); openErr == nil {
			st = opened
			defer st.Close()
		}
	}
	results := doctor.Checker{Host: host.NewRealHost(), Store: st, User: u.Username, Home: u.HomeDir}.Run(context.Background())
	fmt.Print(doctor.Print(results))
	if doctor.Failed(results) {
		return errors.New("some checks failed; see the hints above")
	}
	return nil
}

func runReconcile(args []string) error {
	paths := config.ResolvePaths()

	fs := flag.NewFlagSet("reconcile", flag.ExitOnError)
	dbPath := fs.String("db", paths.DBPath, "Path to SQLite database")
	quadletDir := fs.String("quadlet-dir", paths.QuadletDir, "Path to Quadlet units directory")
	traefikDir := fs.String("traefik-dir", paths.TraefikDynamicDir, "Path to Traefik dynamic configuration directory")
	userUnitDir := fs.String("user-unit-dir", paths.UserUnitDir, "Path to systemd user units (Traefik sockets); empty skips the Traefik step")
	traefikConfigDir := fs.String("traefik-config-dir", paths.TraefikDir, "Path to Traefik static config and acme.json; empty skips the Traefik step")
	prune := fs.Bool("prune", false, "Allow pruning of orphan units even if database contains 0 active apps")
	_ = fs.Parse(args)

	st, err := store.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer st.Close()

	rec := reconciler.New(host.NewRealHost(), st, reconciler.Config{
		QuadletDir:        *quadletDir,
		TraefikDynamicDir: *traefikDir,
		AllowEmptyDBPrune: *prune,
		UserUnitDir:       *userUnitDir,
		TraefikDir:        *traefikConfigDir,
	})

	report, err := rec.Reconcile(context.Background())
	if err != nil {
		return fmt.Errorf("reconciliation failed: %w", err)
	}

	fmt.Println("=== Reconciliation Report (DEP-10) ===")
	fmt.Printf("Orphans removed:        %d %v\n", len(report.OrphansRemoved), report.OrphansRemoved)
	fmt.Printf("Files re-rendered:      %d %v\n", len(report.FilesReRendered), report.FilesReRendered)
	fmt.Printf("Units started:          %d %v\n", len(report.UnitsStarted), report.UnitsStarted)
	fmt.Printf("Verify routers cleaned: %d %v\n", len(report.VerifyRoutersCleaned), report.VerifyRoutersCleaned)
	fmt.Printf("Foreign files reported: %d %v\n", len(report.ForeignFilesReported), report.ForeignFilesReported)
	if report.SafeDefaultTriggered {
		fmt.Println("Safe default active:    preserved managed units/configs because database has 0 active apps")
	}
	return nil
}
