package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/r3p-dev/dermaga/internal/api"
	"github.com/r3p-dev/dermaga/internal/backup"
	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/name"
	"github.com/r3p-dev/dermaga/internal/quadlet"
)

func cmdDB(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintf(os.Stdout, "usage: %s db [list | create <name> --engine=… | show <name> [--reveal] | link <name> <app> | unlink <name> <app> | shell <name> | proxy <name> [--port P] | restart <name> | backup <name> | restore <name> [--snapshot S] [--into N] [-y] | restore-test <name> | remove <name> [--purge]]\n", name.Name)
		return flag.ErrHelp
	}
	switch args[0] {
	case "list":
		return dbList(args[1:])
	case "create":
		return dbCreate(args[1:])
	case "show":
		return dbShow(args[1:])
	case "link":
		return dbLink(args[1:])
	case "unlink":
		return dbUnlink(args[1:])
	case "shell":
		return dbShell(args[1:])
	case "proxy":
		return dbProxy(args[1:])
	case "restart":
		return dbRestart(args[1:])
	case "backup":
		return dbBackup(args[1:])
	case "restore":
		return dbRestore(args[1:])
	case "restore-test":
		return dbRestoreTest(args[1:])
	case "remove":
		return dbRemove(args[1:])
	}
	fmt.Fprintf(os.Stderr, "usage: %s db [list | create <name> --engine=… | show <name> [--reveal] | link <name> <app> | unlink <name> <app> | shell <name> | proxy <name> [--port P] | restart <name> | backup <name> | restore <name> [--snapshot S] [--into N] [-y] | restore-test <name> | remove <name> [--purge]]\n", name.Name)
	return errQuiet
}

func dbList(args []string) error {
	fs := flag.NewFlagSet("db list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parse(fs, args); err != nil {
		return err
	}
	var list []api.DatabaseView
	if err := call("GET", "/api/databases", nil, &list); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(list)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tENGINE\tVERSION\tSTATUS\tVOLUME\tLINKED")
	for _, d := range list {
		linked := "-"
		if len(d.LinkedApps) > 0 {
			linked = strings.Join(d.LinkedApps, ",")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", d.Name, d.Engine, d.Version, d.Status, d.Volume, linked)
	}
	return tw.Flush()
}

func dbCreate(args []string) error {
	dbName, rest, err := sub("db create", args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("db create", flag.ContinueOnError)
	engine := fs.String("engine", "", "database engine (postgres, mariadb, valkey)")
	version := fs.String("version", "", "image version tag (defaults to pinned major)")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errQuiet
	}
	if *engine == "" {
		fmt.Fprintln(os.Stderr, "error: --engine is required (postgres, mariadb, valkey)")
		return errQuiet
	}

	req := api.CreateDatabaseRequest{
		Name:    dbName,
		Engine:  *engine,
		Version: *version,
	}
	var view api.DatabaseView
	if err := call("POST", "/api/databases", req, &view); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(view)
	}

	fmt.Printf("Created database %s (%s %s)\n", view.Name, view.Engine, view.Version)
	fmt.Printf("Volume: %s\n\n", view.Volume)
	if view.Credentials != nil {
		fmt.Println("Credentials (stored encrypted):")
		if view.Credentials.Username != "" {
			fmt.Printf("  User:     %s\n", view.Credentials.Username)
		}
		fmt.Printf("  Password: %s\n", view.Credentials.Password)
		fmt.Printf("  Port:     %d\n", view.Port)
		if view.Credentials.Database != "" {
			fmt.Printf("  Database: %s\n", view.Credentials.Database)
		}
		if view.URL != "" {
			fmt.Printf("  URL:      %s\n\n", view.URL)
		}
	}
	if view.Snippet != "" {
		fmt.Println(view.Snippet)
	}
	return nil
}

func dbShow(args []string) error {
	dbName, rest, err := sub("db show", args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("db show", flag.ContinueOnError)
	reveal := fs.Bool("reveal", false, "display decrypted credentials and URL")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errQuiet
	}
	path := "/api/databases/" + url.PathEscape(dbName)
	if *reveal || *asJSON {
		path += "?reveal=true"
	}

	var view api.DatabaseView
	if err := call("GET", path, nil, &view); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(view)
	}

	fmt.Printf("Name:     %s\n", view.Name)
	fmt.Printf("Engine:   %s %s\n", view.Engine, view.Version)
	fmt.Printf("Status:   %s\n", view.Status)
	fmt.Printf("Volume:   %s\n", view.Volume)
	fmt.Printf("Created:  %s\n", view.CreatedAt.Format(time.DateTime))
	linked := "none"
	if len(view.LinkedApps) > 0 {
		linked = strings.Join(view.LinkedApps, ", ")
	}
	fmt.Printf("Linked:   %s\n\n", linked)

	if view.Credentials != nil {
		fmt.Println("Credentials:")
		fmt.Printf("  Host:     %s\n", quadlet.DBContainerName(view.Name))
		fmt.Printf("  Port:     %d\n", view.Port)
		if view.Credentials.Username != "" {
			fmt.Printf("  User:     %s\n", view.Credentials.Username)
		}
		if view.Credentials.Database != "" {
			fmt.Printf("  Database: %s\n", view.Credentials.Database)
		}
		fmt.Printf("  Password: %s\n", view.Credentials.Password)
		if view.URL != "" {
			fmt.Printf("  URL:      %s\n\n", view.URL)
		}
	} else {
		fmt.Println("Credentials: (hidden, pass --reveal to display)")
		fmt.Printf("  Host:     %s\n", quadlet.DBContainerName(view.Name))
		fmt.Printf("  Port:     %d\n\n", view.Port)
	}

	if view.Snippet != "" {
		fmt.Println(view.Snippet)
	}
	return nil
}

func dbLink(args []string) error {
	dbName, rest, err := sub("db link", args)
	if err != nil {
		return err
	}
	appName, rest, err := sub("db link "+dbName, rest)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("db link", flag.ContinueOnError)
	var envFlags list
	fs.Var(&envFlags, "env", "custom env mapping KEY=ALIAS (repeatable)")
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errQuiet
	}

	var envNames map[string]string
	if len(envFlags) > 0 {
		envNames = make(map[string]string)
		for _, e := range envFlags {
			k, v, ok := strings.Cut(e, "=")
			if !ok {
				fmt.Fprintf(os.Stderr, "invalid -env %q: must be KEY=ALIAS\n", e)
				return errQuiet
			}
			envNames[k] = v
		}
	}

	req := api.LinkDatabaseRequest{
		App:      appName,
		EnvNames: envNames,
	}
	var resp struct {
		Database       string            `json:"database"`
		App            string            `json:"app"`
		EnvNames       map[string]string `json:"env_names"`
		RedeployQueued bool              `json:"redeployQueued"`
	}
	path := "/api/databases/" + url.PathEscape(dbName) + "/link"
	if err := call("POST", path, req, &resp); err != nil {
		return err
	}

	fmt.Printf("Linked database %s to %s.\n", dbName, appName)
	if resp.RedeployQueued {
		fmt.Printf("Queued zero-downtime redeploy of %s.\n", appName)
	}
	return nil
}

func dbUnlink(args []string) error {
	dbName, rest, err := sub("db unlink", args)
	if err != nil {
		return err
	}
	appName, _, err := sub("db unlink "+dbName, rest)
	if err != nil {
		return err
	}

	req := api.UnlinkDatabaseRequest{App: appName}
	var resp struct {
		Database       string `json:"database"`
		App            string `json:"app"`
		RedeployQueued bool   `json:"redeployQueued"`
	}
	path := "/api/databases/" + url.PathEscape(dbName) + "/unlink"
	if err := call("POST", path, req, &resp); err != nil {
		return err
	}

	fmt.Printf("Unlinked database %s from %s.\n", dbName, appName)
	if resp.RedeployQueued {
		fmt.Printf("Queued zero-downtime redeploy of %s.\n", appName)
	}
	return nil
}

func dbRestart(args []string) error {
	dbName, _, err := sub("db restart", args)
	if err != nil {
		return err
	}
	path := "/api/databases/" + url.PathEscape(dbName) + "/restart"
	var resp map[string]string
	if err := call("POST", path, nil, &resp); err != nil {
		return err
	}
	fmt.Printf("Restarted database %s.\n", dbName)
	return nil
}

func dbRemove(args []string) error {
	dbName, rest, err := sub("db remove", args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("db remove", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "permanently delete the volume and all data")
	force := fs.Bool("force", false, "force removal even if linked to apps")
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errQuiet
	}
	v := url.Values{}
	if *purge {
		v.Set("purge", "true")
	}
	if *force {
		v.Set("force", "true")
	}
	q := v.Encode()
	path := "/api/databases/" + url.PathEscape(dbName)
	if q != "" {
		path += "?" + q
	}
	if err := call("DELETE", path, nil, nil); err != nil {
		return err
	}
	if *purge {
		fmt.Printf("Removed database %s and purged volume.\n", dbName)
	} else {
		fmt.Printf("Removed database %s (volume retained).\n", dbName)
	}
	return nil
}

func dbShell(args []string) error {
	dbName, _, err := sub("db shell", args)
	if err != nil {
		return err
	}
	var view api.DatabaseView
	if err := call("GET", "/api/databases/"+url.PathEscape(dbName)+"?reveal=true", nil, &view); err != nil {
		return err
	}
	if view.Credentials == nil {
		return errors.New("failed to retrieve database credentials")
	}

	ctrName := quadlet.DBContainerName(view.Name)
	var cmd *exec.Cmd
	switch view.Engine {
	case "postgres":
		cmd = exec.Command("podman", "exec", "-it",
			"-e", "PGPASSWORD="+view.Credentials.Password,
			ctrName, "psql", "-U", view.Credentials.Username, "-d", view.Credentials.Database)
	case "mariadb":
		cmd = exec.Command("podman", "exec", "-it",
			ctrName, "mariadb", "-u", view.Credentials.Username, "-p"+view.Credentials.Password, view.Credentials.Database)
	case "valkey":
		cmd = exec.Command("podman", "exec", "-it",
			ctrName, "valkey-cli", "-a", view.Credentials.Password)
	default:
		return fmt.Errorf("shell not supported for engine %q", view.Engine)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func dbProxy(args []string) error {
	dbName, rest, err := sub("db proxy", args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("db proxy", flag.ContinueOnError)
	port := fs.Int("port", 0, "local port to listen on (defaults to engine standard port)")
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errQuiet
	}
	var view api.DatabaseView
	if err := call("GET", "/api/databases/"+url.PathEscape(dbName)+"?reveal=true", nil, &view); err != nil {
		return err
	}
	if view.Credentials == nil {
		return errors.New("failed to retrieve database credentials")
	}

	ctrName := quadlet.DBContainerName(view.Name)
	// Inspect container IP on its network
	ipOut, err := exec.Command("podman", "inspect", ctrName, "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}").Output()
	if err != nil {
		return fmt.Errorf("inspecting container %s: %w", ctrName, err)
	}
	ip := strings.TrimSpace(string(ipOut))
	if ip == "" {
		return fmt.Errorf("database container %s is not running or has no IP", ctrName)
	}

	listenPort := view.Port
	if *port > 0 {
		listenPort = *port
	}

	p, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	sockDir := filepath.Join(p.RuntimeDir, "proxy")
	_ = os.MkdirAll(sockDir, 0o700)
	sockPath := filepath.Join(sockDir, dbName+".sock")
	_ = os.Remove(sockPath)

	self, err := os.Executable()
	if err != nil {
		return err
	}

	targetAddr := net.JoinHostPort(ip, strconv.Itoa(view.Port))
	// Run bridge inside rootless network namespace
	bridgeCmd := exec.Command("podman", "unshare", "--rootless-netns", self, "db-bridge", targetAddr, sockPath)
	bridgeCmd.Stderr = os.Stderr
	if err := bridgeCmd.Start(); err != nil {
		return fmt.Errorf("starting netns bridge: %w", err)
	}
	defer func() {
		if bridgeCmd.Process != nil {
			_ = bridgeCmd.Process.Kill()
		}
		_ = os.Remove(sockPath)
	}()

	// Wait up to 5s for the socket to be ready
	ready := false
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		c, err := net.Dial("unix", sockPath)
		if err == nil {
			c.Close()
			ready = true
			break
		}
	}
	if !ready {
		return errors.New("timed out waiting for rootless-netns proxy bridge")
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", listenPort))
	if err != nil {
		return fmt.Errorf("listening on 127.0.0.1:%d: %w", listenPort, err)
	}
	defer ln.Close()

	fmt.Printf("Proxying 127.0.0.1:%d -> %s (%s)\n", listenPort, ctrName, targetAddr)
	fmt.Println("Ready for connections. Press Ctrl+C to exit.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		ln.Close()
	}()

	for {
		client, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go func(c net.Conn) {
			defer c.Close()
			target, err := net.Dial("unix", sockPath)
			if err != nil {
				return
			}
			defer target.Close()

			done := make(chan struct{}, 2)
			go func() {
				_, _ = io.Copy(target, c)
				done <- struct{}{}
			}()
			go func() {
				_, _ = io.Copy(c, target)
				done <- struct{}{}
			}()
			<-done
		}(client)
	}
}

// cmdDBBridge runs inside the rootless network namespace, bridging a Unix domain
// socket to a container's internal TCP address.
func cmdDBBridge(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: db-bridge <target-ip:port> <sock-path>")
	}
	targetAddr := args[0]
	sockPath := args[1]

	_ = os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return err
	}
	defer ln.Close()
	defer os.Remove(sockPath)
	_ = os.Chmod(sockPath, 0o600)

	for {
		uconn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func(uc net.Conn) {
			defer uc.Close()
			tc, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
			if err != nil {
				return
			}
			defer tc.Close()

			done := make(chan struct{}, 2)
			go func() {
				_, _ = io.Copy(tc, uc)
				done <- struct{}{}
			}()
			go func() {
				_, _ = io.Copy(uc, tc)
				done <- struct{}{}
			}()
			<-done
		}(uconn)
	}
}

func dbBackup(args []string) error {
	dbName, _, err := sub("db backup", args)
	if err != nil {
		return err
	}
	var snap backup.Snapshot
	if err := call("POST", "/api/databases/"+dbName+"/backup", nil, &snap); err != nil {
		return err
	}
	fmt.Printf("Database %s backed up successfully (snapshot %s, %s)\n", dbName, snap.ShortID, humanBytes(snap.SizeBytes))
	return nil
}

func dbRestore(args []string) error {
	dbName, rest, err := sub("db restore", args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("db restore", flag.ContinueOnError)
	snapshot := fs.String("snapshot", "", "snapshot ID to restore (defaults to latest)")
	into := fs.String("into", "", "restore into a new database name")
	yes := fs.Bool("yes", false, "skip interactive confirmation")
	fs.BoolVar(yes, "y", false, "skip interactive confirmation")
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errQuiet
	}

	target := dbName
	if *into != "" {
		target = *into
	}

	if !*yes {
		fmt.Fprintf(os.Stderr, "WARNING: This will restore snapshot %q into database %q.\nType %q to continue: ", *snapshot, target, target)
		var confirm string
		if _, err := fmt.Fscanln(os.Stdin, &confirm); err != nil || confirm != target {
			fmt.Fprintln(os.Stderr, "restore cancelled")
			return errQuiet
		}
	}

	req := api.RestoreDatabaseRequest{
		SnapshotID: *snapshot,
		Into:       *into,
		Confirm:    true,
	}
	var resp map[string]string
	if err := callTimeout("POST", "/api/databases/"+dbName+"/restore", 5*time.Minute, req, &resp); err != nil {
		return err
	}
	fmt.Printf("Database %s restored successfully\n", target)
	return nil
}

func dbRestoreTest(args []string) error {
	dbName, _, err := sub("db restore-test", args)
	if err != nil {
		return err
	}
	fmt.Printf("Testing restore for database %s in isolated sandbox...\n", dbName)
	var res backup.TestRestoreResult
	if err := callTimeout("POST", "/api/databases/"+dbName+"/restore-test", 5*time.Minute, nil, &res); err != nil {
		return err
	}
	fmt.Printf("Test restore PASSED (snapshot %s, took %s, sanity check: %s)\n", res.SnapshotID, res.Duration, res.SanityOut)
	return nil
}
