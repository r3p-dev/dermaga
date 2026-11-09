// Package daemon runs `dermaga daemon`: the systemd user service that owns the
// database, the CLI socket and the panel listener.
package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/r3p-dev/dermaga/internal/api"
	"github.com/r3p-dev/dermaga/internal/apps"
	"github.com/r3p-dev/dermaga/internal/audit"
	"github.com/r3p-dev/dermaga/internal/auth"
	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/databases"
	"github.com/r3p-dev/dermaga/internal/deploy"
	"github.com/r3p-dev/dermaga/internal/host"
	"github.com/r3p-dev/dermaga/internal/name"
	"github.com/r3p-dev/dermaga/internal/quadlet"
	"github.com/r3p-dev/dermaga/internal/reconcile"
	"github.com/r3p-dev/dermaga/internal/secrets"
	"github.com/r3p-dev/dermaga/internal/store"
	"github.com/r3p-dev/dermaga/internal/traefik"
	"github.com/r3p-dev/dermaga/internal/webhook"
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
	for _, d := range []string{p.ConfigDir, p.DataDir, p.RuntimeDir, p.StateDir} {
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

	// Claim the CLI socket first: it is also the "one daemon only" lock.
	sockLn, err := listenUnix(p.Socket())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.HooksDir(), 0o700); err != nil {
		sockLn.Close()
		return err
	}
	hookLn, err := listenUnix(p.HookSocket())
	if err != nil {
		sockLn.Close()
		return err
	}
	panelLn, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Panel.Port)))
	if err != nil {
		sockLn.Close()
		hookLn.Close()
		return err
	}

	appStore := apps.Store{DB: db, Box: box}
	dbStore := databases.Store{DB: db, Box: box}
	site := traefik.Site{} // shortcut: built-in Site defaults until the installer (M8) stores host settings
	self, err := os.Executable()
	if err != nil {
		return err
	}
	engine := &deploy.Engine{Apps: appStore, Databases: dbStore, Host: deploy.NewSystem(p, self), Paths: p, Site: site}
	startup(appStore, p, site, self)

	srv := &api.Server{DB: db, Auth: auth.New(db, box), Port: cfg.Panel.Port, Started: time.Now(),
		Apps: appStore, Databases: dbStore, Engine: engine, Paths: p,
		OnSettings: func(ctx context.Context) error { return writeGlobal(ctx, appStore, p, site) }}
	hooks := &webhook.Handler{
		Lookup: appStore.ByID,
		Record: appStore.RecordDelivery,
		Deploy: func(ctx context.Context, app, ref string) error {
			_, err := engine.Enqueue(ctx, app, "webhook", ref)
			return err
		},
		Audit: func(action, target, details string) {
			if err := audit.Log(db, "webhook", action, target, details); err != nil {
				slog.Error("audit log write failed", "action", action, "err", err)
			}
		},
	}
	servers := []*http.Server{api.NewHTTPServer(srv.Socket()), api.NewHTTPServer(srv.Panel()), api.NewHTTPServer(hooks.Mux())}
	listeners := []net.Listener{sockLn, panelLn, hookLn}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	go engine.Run(ctx)
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

// startup runs before the listeners serve: it fails releases a crash left
// unfinished, reconciles units and routes with the database (D32), keeps
// dermaga's Traefik files current and (re)starts the hook relay. Failures are
// logged; the daemon still starts, so the panel and CLI stay usable to fix them.
func startup(store apps.Store, p config.Paths, site traefik.Site, self string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if rs, err := store.Unfinished(ctx); err != nil {
		slog.Error("startup: unfinished releases", "err", err)
	} else {
		for _, r := range rs {
			r.Status, r.Error = "failed", "the daemon stopped during this deploy"
			if err := store.Update(ctx, r); err != nil {
				slog.Error("startup: failing release", "rel8", r.Rel8, "err", err)
			}
		}
	}
	list, err := store.List(ctx)
	if err != nil {
		slog.Error("reconcile: loading apps", "err", err)
		return
	}
	var desired []reconcile.App
	for _, a := range list {
		d := reconcile.App{Spec: a.Spec}
		if a.Live != nil {
			d.Live = a.Live.Rel8
		}
		desired = append(desired, d)
	}
	r := reconcile.Run(ctx, host.Host{QuadletDir: p.QuadletDir, DynamicDir: p.DynamicDir()}, desired, site)
	for _, s := range r.Repaired {
		slog.Info("reconcile: repaired", "what", s)
	}
	for _, s := range r.Drift {
		slog.Warn("reconcile: drift", "what", s)
	}
	for _, s := range r.Errors {
		slog.Error("reconcile: failed", "what", s)
	}
	slog.Info("reconcile: done", "apps", len(desired), "repaired", len(r.Repaired), "drift", len(r.Drift), "errors", len(r.Errors))
	if err := writeGlobal(ctx, store, p, site); err != nil {
		slog.Error("startup: Traefik files", "err", err)
	}
	if err := ensureRelay(ctx, p, self); err != nil {
		slog.Error("startup: hook relay", "err", err)
	}
}

// writeGlobal keeps 00-global.yml equal to its rendering and creates the
// 99-custom.yml placeholder once. While vps-setup's 00-vps.yml is still there
// (migration, M10), 00-global.yml is not written: both define secure etc.
func writeGlobal(ctx context.Context, store apps.Store, p config.Paths, site traefik.Site) error {
	dir := p.DynamicDir()
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil // no Traefik installed (yet)
	}
	if err := traefik.WriteCustomPlaceholder(dir); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "00-vps.yml")); err == nil {
		slog.Warn("vps-setup's 00-vps.yml is present: not writing 00-global.yml (see the migration guide)")
		return nil
	}
	hook, err := store.HookHost(ctx)
	if err != nil {
		return err
	}
	site.HookHost = hook
	b, err := traefik.Global(site)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, traefik.GlobalFile)
	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, b) {
		return nil
	}
	return host.WriteFile(path, b, 0o644)
}

// ensureRelay keeps dg-hooks running the current binary (D1, D28): the binary
// is copied into the relay's rootfs, the unit rendered, and the container
// restarted when either changed.
func ensureRelay(ctx context.Context, p config.Paths, self string) error {
	if _, err := exec.LookPath("podman"); err != nil {
		return nil // development machine without Podman
	}
	changed := false
	relayBin := filepath.Join(p.RelayDir(), name.Name)
	if !sameFile(self, relayBin) {
		if err := os.MkdirAll(p.RelayDir(), 0o755); err != nil {
			return err
		}
		if err := copyFile(self, relayBin); err != nil {
			return err
		}
		changed = true
	}
	fn, unit, err := quadlet.Hooks(p.RelayDir(), p.HooksDir())
	if err != nil {
		return err
	}
	if cur, err := os.ReadFile(filepath.Join(p.QuadletDir, fn)); err != nil || !bytes.Equal(cur, unit) {
		if err := quadlet.DryRun(ctx, p.QuadletDir, map[string][]byte{fn: unit}); err != nil {
			return err
		}
		if err := host.WriteFile(filepath.Join(p.QuadletDir, fn), unit, 0o644); err != nil {
			return err
		}
		changed = true
	}
	h := host.Host{QuadletDir: p.QuadletDir}
	svc := strings.TrimSuffix(fn, ".container") + ".service"
	if changed {
		if err := h.DaemonReload(ctx); err != nil {
			return err
		}
		_, err := host.Run(ctx, 30*time.Second, "systemctl", "--user", "restart", "--no-block", svc)
		return err
	}
	if state, err := h.ActiveState(ctx, svc); err == nil && state != "active" && state != "activating" {
		return h.Start(ctx, svc)
	}
	return nil
}

// sameFile compares two files by size and SHA-256, streaming: reading the
// 12 MB binary into memory would cost the daemon's RSS budget (BRIEF §9).
func sameFile(a, b string) bool {
	sum := func(p string) (int64, [sha256.Size]byte, error) {
		var out [sha256.Size]byte
		f, err := os.Open(p)
		if err != nil {
			return 0, out, err
		}
		defer f.Close()
		h := sha256.New()
		n, err := io.Copy(h, f)
		copy(out[:], h.Sum(nil))
		return n, out, err
	}
	na, ha, err1 := sum(a)
	nb, hb, err2 := sum(b)
	return err1 == nil && err2 == nil && na == nb && ha == hb
}

// copyFile copies src to dst atomically (temp file, fsync, rename), streaming.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+name.Name+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
