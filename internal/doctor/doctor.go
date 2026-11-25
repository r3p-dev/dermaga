// Package doctor runs the host health checks (DOC-1) and explains how to fix each failure.
// The installer runs them at the end (INST-11).
package doctor

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dermaga/internal/auth"
	"dermaga/internal/host"
	"dermaga/internal/store"
)

// MinPodmanVersion is the oldest Podman dermaga supports: Debian 13's, the only one tested.
const MinPodmanVersion = "5.4.2"

// Status of one check.
type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
)

// Result is the outcome of one check.
type Result struct {
	Name   string
	Status Status
	Detail string
	Fix    string // what to do when Status is not OK
}

// Checker runs the checks for one user through the host boundary.
type Checker struct {
	Host  host.Host
	Store *store.Store // optional; enables database-backed checks (TOTP, domains)
	User  string       // the Linux user dermaga runs as
	Home  string       // that user's home directory
}

// Run executes every check in a fixed order. Checks for domains, GitHub, TOTP and certificates
// join when those features exist.
func (c Checker) Run(ctx context.Context) []Result {
	results := []Result{
		c.linger(),
		c.subIDs("/etc/subuid", "subuids"),
		c.subIDs("/etc/subgid", "subgids"),
		c.traefikSockets(ctx),
		c.podmanVersion(ctx),
		c.traefikRunning(ctx),
		c.networkOnline(ctx),
		c.clockSynced(ctx),
		c.diskUsage(ctx),
	}
	if c.Store != nil {
		results = append(results, c.totpStatus(ctx), c.githubStatus(ctx))
	}
	return results
}

// Failed reports whether any result is a failure.
func Failed(results []Result) bool {
	for _, r := range results {
		if r.Status == Fail {
			return true
		}
	}
	return false
}

func (c Checker) run(ctx context.Context, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, _ := c.Host.RunCommand(ctx, name, args...)
	return strings.TrimSpace(string(out))
}

func (c Checker) linger() Result {
	r := Result{Name: "lingering", Status: OK, Detail: "enabled for " + c.User}
	if _, err := c.Host.Stat("/var/lib/systemd/linger/" + c.User); err != nil {
		r.Status, r.Detail = Fail, "not enabled for "+c.User
		r.Fix = "Without lingering your apps stop when you log out. Run: sudo loginctl enable-linger " + c.User
	}
	return r
}

func (c Checker) subIDs(path, name string) Result {
	r := Result{Name: name, Status: Fail, Detail: "no range of at least 65536 for " + c.User + " in " + path,
		Fix: "Rootless Podman needs them. Re-run the installer, or add a line like " + c.User + ":100000:65536 to " + path}
	data, err := c.Host.ReadFile(path)
	if err != nil {
		return r
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Split(line, ":")
		if len(f) == 3 && f[0] == c.User {
			if n, err := strconv.Atoi(f[2]); err == nil && n >= 65536 {
				return Result{Name: name, Status: OK, Detail: line}
			}
		}
	}
	return r
}

func (c Checker) traefikSockets(ctx context.Context) Result {
	out := c.run(ctx, "systemctl", "--user", "is-active", "traefik-http.socket", "traefik-https.socket")
	if out == "active\nactive" {
		return Result{Name: "sockets", Status: OK, Detail: "traefik-http.socket and traefik-https.socket are listening on 80 and 443"}
	}
	return Result{Name: "sockets", Status: Fail, Detail: "traefik sockets: " + strings.ReplaceAll(out, "\n", ", "),
		Fix: "Run: systemctl --user status traefik-http.socket traefik-https.socket. Another program may hold port 80 or 443."}
}

func (c Checker) podmanVersion(ctx context.Context) Result {
	v := c.run(ctx, "podman", "version", "--format", "{{.Client.Version}}")
	if v != "" && !olderThan(v, MinPodmanVersion) {
		return Result{Name: "podman", Status: OK, Detail: "version " + v}
	}
	return Result{Name: "podman", Status: Fail, Detail: fmt.Sprintf("version %q, need %s or newer", v, MinPodmanVersion),
		Fix: "Install Debian 13's podman package: sudo apt-get install podman"}
}

func (c Checker) traefikRunning(ctx context.Context) Result {
	if out := c.run(ctx, "systemctl", "--user", "is-active", "traefik.service"); out != "active" {
		return Result{Name: "traefik", Status: Fail, Detail: "traefik.service is " + out,
			Fix: "Run: journalctl --user -u traefik.service -n 50"}
	}
	return Result{Name: "traefik", Status: OK, Detail: "traefik.service is active"}
}

// networkOnline warns when network-online.target is inactive or took 5 s or more after boot.
func (c Checker) networkOnline(ctx context.Context) Result {
	state := c.run(ctx, "systemctl", "show", "-P", "ActiveState", "network-online.target")
	usec, _ := strconv.ParseInt(c.run(ctx, "systemctl", "show", "-P", "ActiveEnterTimestampMonotonic", "network-online.target"), 10, 64)
	took := time.Duration(usec) * time.Microsecond
	if state == "active" && took < 5*time.Second {
		return Result{Name: "network-online", Status: OK, Detail: fmt.Sprintf("active %.1fs after boot", took.Seconds())}
	}
	return Result{Name: "network-online", Status: Warn, Detail: fmt.Sprintf("state %s, %.1fs after boot", state, took.Seconds()),
		Fix: "Apps start without it, but other services may wait at boot. Re-run the installer to wire it into multi-user.target."}
}

func (c Checker) clockSynced(ctx context.Context) Result {
	if c.run(ctx, "timedatectl", "show", "-P", "NTPSynchronized") == "yes" {
		return Result{Name: "clock", Status: OK, Detail: "synchronised"}
	}
	return Result{Name: "clock", Status: Fail, Detail: "not synchronised",
		Fix: "TOTP codes and certificates need a correct clock. Run: timedatectl timesync-status"}
}

func (c Checker) diskUsage(ctx context.Context) Result {
	out := c.run(ctx, "df", "--output=pcent", c.Home)
	fields := strings.Fields(out) // "Use%" "12%"
	pct := -1
	if len(fields) == 2 {
		pct, _ = strconv.Atoi(strings.TrimSuffix(fields[1], "%"))
	}
	r := Result{Name: "disk", Status: OK, Detail: fmt.Sprintf("%d%% used", pct)}
	switch {
	case pct < 0:
		r.Status, r.Detail, r.Fix = Fail, "could not read disk usage of "+c.Home, "Run: df -h "+c.Home
	case pct >= 95:
		r.Status, r.Fix = Fail, "New builds are refused above 95%. Free space: podman system df"
	case pct >= 85:
		r.Status, r.Fix = Warn, "Free space soon: podman system df"
	}
	return r
}

func (c Checker) totpStatus(ctx context.Context) Result {
	state, err := auth.GetSetupState(ctx, c.Store)
	if err != nil {
		return Result{Name: "totp", Status: Warn, Detail: "could not read setup state", Fix: "Check dermaga database permissions."}
	}
	if state == auth.SetupStateUninitialized {
		return Result{
			Name:   "totp",
			Status: Warn,
			Detail: "first-run setup not completed yet",
			Fix:    "Open the first-run setup link from `dermaga panel` to set an admin password and enable TOTP.",
		}
	}
	if !auth.IsTOTPEnabled(ctx, c.Store) {
		return Result{
			Name:   "totp",
			Status: Warn,
			Detail: "two-factor authentication is disabled",
			Fix:    "Enable TOTP in Settings -> Security to protect admin login.",
		}
	}
	return Result{Name: "totp", Status: OK, Detail: "two-factor authentication is enabled"}
}

func (c Checker) githubStatus(ctx context.Context) Result {
	appID, err := c.Store.GetSetting(ctx, auth.SettingGitHubAppID)
	if err != nil || appID == "" {
		return Result{
			Name:   "github",
			Status: OK,
			Detail: "not connected (configure in Settings -> GitHub)",
		}
	}
	pemEnc, err := c.Store.GetSetting(ctx, "github_app_pem_encrypted")
	if err != nil || pemEnc == "" {
		return Result{
			Name:   "github",
			Status: Fail,
			Detail: "GitHub App key is missing",
			Fix:    "Disconnect and reconnect GitHub in Settings -> GitHub.",
		}
	}
	slug, _ := c.Store.GetSetting(ctx, "github_app_slug")
	return Result{
		Name:   "github",
		Status: OK,
		Detail: fmt.Sprintf("connected (%s, app_id=%s)", slug, appID),
	}
}

// olderThan compares dotted numeric versions such as 5.4.2.
func olderThan(v, min string) bool {
	a, b := strings.Split(v, "."), strings.Split(min, ".")
	for i := 0; i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x, _ = strconv.Atoi(strings.SplitN(a[i], "-", 2)[0])
		}
		y, _ = strconv.Atoi(b[i])
		if x != y {
			return x < y
		}
	}
	return false
}

// Print writes results as plain lines; the installer shows this output as is.
func Print(results []Result) string {
	var b strings.Builder
	for _, r := range results {
		fmt.Fprintf(&b, "[%-4s] %-14s %s\n", strings.ToUpper(string(r.Status)), r.Name, r.Detail)
		if r.Status != OK && r.Fix != "" {
			fmt.Fprintf(&b, "       %-14s %s\n", "", r.Fix)
		}
	}
	return b.String()
}
