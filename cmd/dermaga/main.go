// Package main is the entry point for dermaga.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dermaga/internal/auth"
	"dermaga/internal/config"
	"dermaga/internal/crypto"
	"dermaga/internal/server"
	"dermaga/internal/store"
)

// Version and Commit are set at build time via -ldflags.
var (
	Version = "0.1.0-dev"
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
		err = runAdmin(args)
	case "panel":
		err = runPanel(args)
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
  version              Print version information

Internal Helpers:
  hook-relay           Forward TCP webhook traffic to local Unix domain socket
  probe                Perform internal HTTP health probe via custom DNS
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

	srv, err := server.NewServer(server.Config{
		ListenAddr: *addr,
		SocketPath: *sockPath,
		Store:      st,
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

	select {
	case sig := <-sigChan:
		fmt.Printf("\nReceived signal %v, shutting down gracefully...\n", sig)
		return srv.Close()
	case err := <-errChan:
		if err != nil && !strings.Contains(err.Error(), "Server closed") {
			return err
		}
		return nil
	}
}

func runAdmin(args []string) error {
	if len(args) == 0 {
		fmt.Println("Usage: dermaga admin <set-password|totp disable>")
		return nil
	}

	paths := config.ResolvePaths()

	switch args[0] {
	case "set-password":
		var password string
		fs := flag.NewFlagSet("set-password", flag.ContinueOnError)
		fs.StringVar(&password, "password", "", "New admin password")
		_ = fs.Parse(args[1:])

		if password == "" && len(fs.Args()) > 0 {
			password = fs.Args()[0]
		}
		if password == "" {
			fmt.Print("Enter new admin password: ")
			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading password: %w", err)
			}
			password = strings.TrimSpace(input)
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

func runPanel(args []string) error {
	paths := config.ResolvePaths()

	// dermaga panel requires the daemon (SRV-2)
	resp, err := callDaemon(paths.DaemonSock, server.RecoveryRequest{Action: "status"})
	if err != nil || !resp.Success {
		fmt.Printf("The dermaga daemon is not reachable at %s.\n", paths.DaemonSock)
		fmt.Println("Check daemon status or start it with:")
		fmt.Println("  systemctl --user status dermaga")
		fmt.Println("  systemctl --user start dermaga")
		return nil
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "server"
	}

	fmt.Println("=== dermaga Panel Access ===")
	fmt.Println()
	fmt.Println("1. On your laptop, add or verify this configuration in ~/.ssh/config:")
	fmt.Println()
	fmt.Println("   Host dermaga")
	fmt.Printf("       HostName %s\n", hostname)
	fmt.Println("       User server")
	fmt.Println("       Port 4222")
	fmt.Println("       LocalForward 7070 127.0.0.1:7070")
	fmt.Println("       ServerAliveInterval 30")
	fmt.Println("       ServerAliveCountMax 3")
	fmt.Println()
	fmt.Println("2. Connect from your laptop:")
	fmt.Println("   ssh dermaga")
	fmt.Println()
	fmt.Println("3. Open the panel in your browser:")
	fmt.Println("   http://localhost:7070")
	fmt.Println()
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
func runHookRelay(args []string) error {
	fs := flag.NewFlagSet("hook-relay", flag.ExitOnError)
	listenAddr := fs.String("listen", ":8080", "TCP address to listen on")
	sockPath := fs.String("socket", "/run/dermaga/hooks.sock", "Unix socket destination")
	_ = fs.Parse(args)

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
				_, _ = io.Copy(unixConn, c)
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
