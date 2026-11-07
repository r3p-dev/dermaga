package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/r3p-dev/dermaga/internal/api"
	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/name"
)

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok, warn, fail
	Detail string `json:"detail"`
}

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parse(fs, args); err != nil {
		return err
	}
	checks := doctorChecks()
	if *asJSON {
		printJSON(checks)
	} else {
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, c := range checks {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", strings.ToUpper(c.Status), c.Name, c.Detail)
		}
		tw.Flush()
	}
	for _, c := range checks {
		if c.Status == "fail" {
			return errQuiet
		}
	}
	return nil
}

func doctorChecks() []check {
	u, err := user.Current()
	if err != nil {
		return []check{{"user", "fail", err.Error()}}
	}
	cs := []check{
		checkLinger(u.Username),
		checkSubIDs("subuid", "/etc/subuid", u),
		checkSubIDs("subgid", "/etc/subgid", u),
	}
	cs = append(cs, checkDaemon()...)
	cs = append(cs, checkUnits("traefik-http.socket", "traefik-https.socket"), checkNetworkOnline(), checkClock(), checkDisk())
	return cs
}

func run(prog string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, prog, args...).Output()
	return strings.TrimSpace(string(out)), err
}

func checkLinger(username string) check {
	if _, err := os.Stat("/var/lib/systemd/linger/" + username); err != nil {
		return check{"lingering", "fail", "off: run sudo loginctl enable-linger " + username + " (services stop at logout otherwise)"}
	}
	return check{"lingering", "ok", "enabled for " + username}
}

// subIDCount returns the size of the user's range in an /etc/subuid-style file.
func subIDCount(data string, u *user.User) int {
	for _, l := range strings.Split(data, "\n") {
		f := strings.Split(strings.TrimSpace(l), ":")
		if len(f) == 3 && (f[0] == u.Username || f[0] == u.Uid) {
			n, _ := strconv.Atoi(f[2])
			return n
		}
	}
	return 0
}

func checkSubIDs(label, path string, u *user.User) check {
	b, err := os.ReadFile(path)
	if err != nil {
		return check{label, "fail", err.Error()}
	}
	if n := subIDCount(string(b), u); n < 65536 {
		return check{label, "fail", fmt.Sprintf("%s has %d ids for %s, rootless Podman needs >= 65536", path, n, u.Username)}
	} else {
		return check{label, "ok", fmt.Sprintf("%d ids", n)}
	}
}

func checkDaemon() []check {
	p, err := config.DefaultPaths()
	if err != nil {
		return []check{{"daemon", "fail", err.Error()}}
	}
	fi, err := os.Stat(p.Socket())
	if err != nil {
		return []check{{"daemon", "fail", fmt.Sprintf("no socket at %s: systemctl --user status %s", p.Socket(), name.Name)}}
	}
	if fi.Mode().Perm() != 0o600 {
		return []check{{"daemon", "fail", fmt.Sprintf("%s has mode %v, want 0600", p.Socket(), fi.Mode().Perm())}}
	}
	var st api.Status
	if err := call("GET", "/api/status", nil, &st); err != nil {
		return []check{{"daemon", "fail", err.Error()}}
	}
	cs := []check{{"daemon", "ok", fmt.Sprintf("%s, pid %d, RSS %.1f MB, socket %s (0600)", st.Version, st.PID, float64(st.RSSKB)/1024, p.Socket())}}
	if st.AdminPasswordSet {
		cs = append(cs, check{"admin password", "ok", "set"})
	} else {
		cs = append(cs, check{"admin password", "warn", "not set: dermaga admin set-password"})
	}
	if st.TOTPEnabled {
		cs = append(cs, check{"totp", "ok", "enabled"})
	} else {
		cs = append(cs, check{"totp", "warn", "off: dermaga admin totp enable (strongly recommended)"})
	}
	return cs
}

func checkUnits(units ...string) check {
	out, _ := run("systemctl", append([]string{"--user", "is-active"}, units...)...) // non-zero exit if any is inactive
	states := strings.Fields(out)
	var bad []string
	for i, u := range units {
		if i >= len(states) || states[i] != "active" {
			bad = append(bad, u)
		}
	}
	if len(bad) > 0 {
		return check{"sockets", "fail", "not active: " + strings.Join(bad, ", ")}
	}
	return check{"sockets", "ok", strings.Join(units, ", ") + " active"}
}

// checkNetworkOnline: if nothing pulls in the system network-online.target,
// Quadlet units with default dependencies (Traefik) wait 90 s at every boot
// (ARCHITECTURE D29). Images without an active cloud-init are affected.
func checkNetworkOnline() check {
	out, err := run("systemctl", "show", "-p", "ActiveEnterTimestampMonotonic", "--value", "network-online.target")
	switch {
	case err != nil:
		return check{"network-online", "warn", "systemctl: " + err.Error()}
	case out == "0":
		return check{"network-online", "warn", "network-online.target was not reached this boot, so Traefik starts 90 s late: sudo systemctl add-wants multi-user.target network-online.target"}
	}
	return check{"network-online", "ok", "network-online.target reached"}
}

func checkClock() check {
	out, err := run("timedatectl", "show", "-p", "NTPSynchronized", "--value")
	if err != nil {
		return check{"clock", "fail", "timedatectl: " + err.Error()}
	}
	return clockCheck(out == "yes", uptime())
}

// clockCheck tolerates an unsynchronised clock right after boot: timesyncd's
// first sync takes about 30 s, and doctor may run before it (e.g. at boot).
func clockCheck(synced bool, up time.Duration) check {
	switch {
	case synced:
		return check{"clock", "ok", "NTP-synchronised"}
	case up < 5*time.Minute:
		return check{"clock", "warn", fmt.Sprintf("not NTP-synchronised yet (booted %s ago): re-run in a minute", up.Round(time.Second))}
	}
	return check{"clock", "fail", "not NTP-synchronised (TOTP and certificates need a correct clock): timedatectl status"}
}

// uptime is the time since boot, or a large value if /proc/uptime is unreadable.
func uptime() time.Duration {
	b, err := os.ReadFile("/proc/uptime")
	f := strings.Fields(string(b))
	if err != nil || len(f) == 0 {
		return 24 * time.Hour
	}
	s, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 24 * time.Hour
	}
	return time.Duration(s * float64(time.Second))
}

func checkDisk() check {
	dir, _ := os.UserHomeDir()
	if p, err := config.DefaultPaths(); err == nil {
		if _, err := os.Stat(p.DataDir); err == nil {
			dir = p.DataDir
		}
	}
	var s syscall.Statfs_t
	if err := syscall.Statfs(dir, &s); err != nil || s.Blocks == 0 {
		return check{"disk", "fail", fmt.Sprintf("statfs %s: %v", dir, err)}
	}
	used := 100 - s.Bavail*100/s.Blocks
	d := fmt.Sprintf("%d%% used, %.1f GiB free (%s)", used, float64(s.Bavail*uint64(s.Bsize))/(1<<30), dir)
	switch {
	case used >= 95:
		return check{"disk", "fail", d}
	case used >= 85:
		return check{"disk", "warn", d}
	}
	return check{"disk", "ok", d}
}
