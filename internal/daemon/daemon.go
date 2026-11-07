// Package daemon runs `dermaga daemon`: the systemd user service that owns the
// database, the CLI socket and the panel listener.
package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/r3p-dev/dermaga/internal/api"
	"github.com/r3p-dev/dermaga/internal/auth"
	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/host"
	"github.com/r3p-dev/dermaga/internal/name"
	"github.com/r3p-dev/dermaga/internal/reconcile"
	"github.com/r3p-dev/dermaga/internal/secrets"
	"github.com/r3p-dev/dermaga/internal/store"
	"github.com/r3p-dev/dermaga/internal/traefik"
)

func refuseRoot(euid int) error {
	if euid == 0 {
		return fmt.Errorf("refusing to run as root: run %s as the single unprivileged user (systemctl --user)", name.Name)
	}
	return nil
}

func Run() error {
	if err := refuseRoot(os.Geteuid()); err != nil {
		return err
	}
	p, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	for _, d := range []string{p.ConfigDir, p.DataDir, p.RuntimeDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(d, 0o700); err != nil {
			return err
		}
	}
	cfg, err := config.Load(p.ConfigFile())
	if err != nil {
		return err
	}
	key, err := secrets.LoadOrCreateKey(p.MasterKey())
	if err != nil {
		return err
	}
	box, err := secrets.New(key)
	if err != nil {
		return err
	}
	db, err := store.Open(p.DB())
	if err != nil {
		return err
	}
	defer db.Close()
	reconcileAtStart(db, p)

	sockLn, err := listenUnix(p.Socket())
	if err != nil {
		return err
	}
	panelLn, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Panel.Port)))
	if err != nil {
		sockLn.Close()
		return err
	}

	srv := &api.Server{DB: db, Auth: auth.New(db, box), Port: cfg.Panel.Port, Started: time.Now()}
	servers := []*http.Server{api.NewHTTPServer(srv.Socket()), api.NewHTTPServer(srv.Panel())}
	listeners := []net.Listener{sockLn, panelLn}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	errc := make(chan error, len(servers))
	for i := range servers {
		go func() { errc <- servers[i].Serve(listeners[i]) }()
	}
	slog.Info("started", "version", name.Version(), "panel", panelLn.Addr().String(), "socket", p.Socket())

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
	case err = <-errc:
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		s.Shutdown(sctx) // also closes the Unix listener, which unlinks the socket
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

// listenUnix creates the CLI socket with mode 0600. A socket that still
// answers belongs to a running daemon; a dead one is stale and replaced.
func listenUnix(path string) (net.Listener, error) {
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		c.Close()
		return nil, fmt.Errorf("%s: another %s daemon is running", path, name.Name)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// the parent directory is 0700, so the socket is private before this too
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// reconcileAtStart repairs drift between the database and the generated units
// and Traefik files before anything else runs. Failures are logged; the
// daemon still starts, so the panel and CLI stay usable to fix them.
func reconcileAtStart(db *sql.DB, p config.Paths) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	apps, err := reconcile.LoadApps(ctx, db)
	if err != nil {
		slog.Error("reconcile: loading apps", "err", err)
		return
	}
	// shortcut: built-in Site defaults until the installer (M8) stores host settings
	r := reconcile.Run(ctx, host.Host{QuadletDir: p.QuadletDir, DynamicDir: p.DynamicDir()}, apps, traefik.Site{})
	for _, s := range r.Repaired {
		slog.Info("reconcile: repaired", "what", s)
	}
	for _, s := range r.Drift {
		slog.Warn("reconcile: drift", "what", s)
	}
	for _, s := range r.Errors {
		slog.Error("reconcile: failed", "what", s)
	}
	slog.Info("reconcile: done", "apps", len(apps), "repaired", len(r.Repaired), "drift", len(r.Drift), "errors", len(r.Errors))
}
