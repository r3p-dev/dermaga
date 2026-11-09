// Package doctor provides host and environment health checks.
package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/r3p-dev/dermaga/internal/config"
)

// Check is the result of a single health inspection.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok", "warn", "fail"
	Detail string `json:"detail"`
}

// RunHostChecks executes the system-level checks (linger, subuid/subgid, sockets, network, clock, disk).
func RunHostChecks() []Check {
	u, err := user.Current()
	if err != nil {
		return []Check{{Name: "user", Status: "fail", Detail: err.Error()}}
	}
	cs := []Check{
		Linger(u.Username),
		SubIDs("subuid", "/etc/subuid", u),
		SubIDs("subgid", "/etc/subgid", u),
	}
	cs = append(cs, Units("traefik-http.socket", "traefik-https.socket"), NetworkOnline(), Clock(), Disk())
	return cs
}

func run(prog string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, prog, args...).Output()
	return strings.TrimSpace(string(out)), err
}

func Linger(username string) Check {
	if _, err := os.Stat("/var/lib/systemd/linger/" + username); err != nil {
		return Check{Name: "lingering", Status: "fail", Detail: "off: run sudo loginctl enable-linger " + username + " (services stop at logout otherwise)"}
	}
	return Check{Name: "lingering", Status: "ok", Detail: "enabled for " + username}
}

func SubIDCount(data string, u *user.User) int {
	for _, l := range strings.Split(data, "\n") {
		f := strings.Split(strings.TrimSpace(l), ":")
		if len(f) == 3 && (f[0] == u.Username || f[0] == u.Uid) {
			n, _ := strconv.Atoi(f[2])
			return n
		}
	}
	return 0
}

func SubIDs(label, path string, u *user.User) Check {
	b, err := os.ReadFile(path)
	if err != nil {
		return Check{Name: label, Status: "fail", Detail: err.Error()}
	}
	n := SubIDCount(string(b), u)
	if n < 65536 {
		return Check{Name: label, Status: "fail", Detail: fmt.Sprintf("%s has %d ids for %s, rootless Podman needs >= 65536", path, n, u.Username)}
	}
	return Check{Name: label, Status: "ok", Detail: fmt.Sprintf("%d ids", n)}
}

func Units(units ...string) Check {
	out, _ := run("systemctl", append([]string{"--user", "is-active"}, units...)...)
	states := strings.Fields(out)
	var bad []string
	for i, u := range units {
		if i >= len(states) || states[i] != "active" {
			bad = append(bad, u)
		}
	}
	if len(bad) > 0 {
		return Check{Name: "sockets", Status: "fail", Detail: "not active: " + strings.Join(bad, ", ")}
	}
	return Check{Name: "sockets", Status: "ok", Detail: strings.Join(units, ", ") + " active"}
}

func NetworkOnline() Check {
	out, err := run("systemctl", "show", "-p", "ActiveEnterTimestampMonotonic", "--value", "network-online.target")
	switch {
	case err != nil:
		return Check{Name: "network-online", Status: "warn", Detail: "systemctl: " + err.Error()}
	case out == "0":
		return Check{Name: "network-online", Status: "warn", Detail: "network-online.target was not reached this boot, so Traefik starts 90 s late: sudo systemctl add-wants multi-user.target network-online.target"}
	}
	return Check{Name: "network-online", Status: "ok", Detail: "network-online.target reached"}
}

func Clock() Check {
	out, err := run("timedatectl", "show", "-p", "NTPSynchronized", "--value")
	if err != nil {
		return Check{Name: "clock", Status: "fail", Detail: "timedatectl: " + err.Error()}
	}
	return ClockCheck(out == "yes", Uptime())
}

func ClockCheck(synced bool, up time.Duration) Check {
	switch {
	case synced:
		return Check{Name: "clock", Status: "ok", Detail: "NTP-synchronised"}
	case up < 5*time.Minute:
		return Check{Name: "clock", Status: "warn", Detail: fmt.Sprintf("not NTP-synchronised yet (booted %s ago): re-run in a minute", up.Round(time.Second))}
	}
	return Check{Name: "clock", Status: "fail", Detail: "not NTP-synchronised (TOTP and certificates need a correct clock): timedatectl status"}
}

func Uptime() time.Duration {
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

func Disk() Check {
	dir, _ := os.UserHomeDir()
	if p, err := config.DefaultPaths(); err == nil {
		if _, err := os.Stat(p.DataDir); err == nil {
			dir = p.DataDir
		}
	}
	var s syscall.Statfs_t
	if err := syscall.Statfs(dir, &s); err != nil || s.Blocks == 0 {
		return Check{Name: "disk", Status: "fail", Detail: fmt.Sprintf("statfs %s: %v", dir, err)}
	}
	used := 100 - s.Bavail*100/s.Blocks
	d := fmt.Sprintf("%d%% used, %.1f GiB free (%s)", used, float64(s.Bavail*uint64(s.Bsize))/(1<<30), dir)
	switch {
	case used >= 95:
		return Check{Name: "disk", Status: "fail", Detail: d}
	case used >= 85:
		return Check{Name: "disk", Status: "warn", Detail: d}
	}
	return Check{Name: "disk", Status: "ok", Detail: d}
}
