// Package spec is the typed configuration of an app (BRIEF §3.4, §3.6) and its
// validation. Every user-controlled value that reaches a generated unit, a
// Traefik file or a command passes through Validate first.
package spec

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

// App is what the generators need to know about one app. Name and Domains are
// stored in their own columns/table; the rest is the apps.spec JSON.
type App struct {
	Name    string   `json:"-"`
	Domains []Domain `json:"-"`
	Source  Source   `json:"source"`
	// Strategy: "" picks recreate for apps with volumes, blue-green otherwise (BRIEF §3.5.10).
	Strategy   string     `json:"strategy,omitempty"`
	Port       int        `json:"port"`
	Health     Health     `json:"health,omitzero"`
	Limits     Limits     `json:"limits,omitzero"` // zero fields: DefaultLimits
	AddCaps    []string   `json:"addCaps,omitempty"`
	ReadOnly   bool       `json:"readOnly,omitempty"`
	Volumes    []Volume   `json:"volumes,omitempty"`
	Middleware Middleware `json:"middleware,omitzero"`
}

// Source says where releases come from (BRIEF §2): a GitHub repo built on the
// server, or a public image.
type Source struct {
	Type          string   `json:"type"`                    // "git" or "image"
	Repo          string   `json:"repo,omitempty"`          // git@github.com:owner/repo.git
	Branch        string   `json:"branch,omitempty"`        // deployed branch
	Context       string   `json:"context,omitempty"`       // build context in the repo; "" = root
	Containerfile string   `json:"containerfile,omitempty"` // relative to Context; "" = Containerfile, else Dockerfile
	Image         string   `json:"image,omitempty"`         // image apps: fully qualified, pinned reference
	AutoDeploy    bool     `json:"autoDeploy,omitempty"`    // deploy on webhook deliveries
	Trigger       string   `json:"trigger,omitempty"`       // "push" (default) or "workflow_run" (CI gate, BRIEF §5)
	Workflow      string   `json:"workflow,omitempty"`      // workflow name or path for the CI gate
	WatchPaths    []string `json:"watchPaths,omitempty"`    // deploy only if a push touched a match ("**" spans directories)
}

// RepoFullName is "owner/repo" of a git source.
func (s Source) RepoFullName() string {
	return strings.TrimSuffix(strings.TrimPrefix(s.Repo, "git@github.com:"), ".git")
}

// EffectiveStrategy resolves "" (BRIEF §3.5.10: recreate is the default with volumes).
func (a App) EffectiveStrategy() string {
	switch {
	case a.Strategy != "":
		return a.Strategy
	case len(a.Volumes) > 0:
		return "recreate"
	}
	return "blue-green"
}

type Domain struct {
	Host     string `json:"host"`
	Primary  bool   `json:"primary,omitempty"`
	Redirect bool   `json:"redirect,omitempty"` // 301 to the primary domain (aliases, www ↔ apex)
}

type Health struct {
	Path       string `json:"path,omitempty"` // HTTP path probed by dermaga probe (M4)
	Cmd        string `json:"cmd,omitempty"`  // HealthCmd= in the unit, run inside the container
	Successes  int    `json:"successes,omitempty"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

type Limits struct {
	Memory string `json:"memory,omitempty"` // podman --memory, e.g. 512m
	CPUs   string `json:"cpus,omitempty"`   // podman --cpus, e.g. 1 or 0.5
	Pids   int    `json:"pids,omitempty"`
}

// DefaultLimits are vps-setup.sh's DEFAULT_MEMORY, DEFAULT_CPUS and DEFAULT_PIDS.
var DefaultLimits = Limits{Memory: "512m", CPUs: "1", Pids: 512}

// Effective fills unset limits from DefaultLimits.
func (l Limits) Effective() Limits {
	if l.Memory == "" {
		l.Memory = DefaultLimits.Memory
	}
	if l.CPUs == "" {
		l.CPUs = DefaultLimits.CPUs
	}
	if l.Pids == 0 {
		l.Pids = DefaultLimits.Pids
	}
	return l
}

// Volume is a named volume dg-<app>_<name> mounted at Path. "_" can't occur in
// app or volume names, so volume names of different apps never collide.
type Volume struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Middleware is an app's Traefik middleware configuration (BRIEF §3.6). Zero
// values mean the global defaults.
type Middleware struct {
	Headers      Headers         `json:"headers,omitzero"`
	NoCompress   bool            `json:"noCompress,omitempty"`
	RateLimit    *RateLimit      `json:"rateLimit,omitempty"` // nil: the global default
	NoRateLimit  bool            `json:"noRateLimit,omitempty"`
	BasicAuth    []BasicAuthUser `json:"basicAuth,omitempty"`
	IPAllowList  []string        `json:"ipAllowList,omitempty"` // IPs or CIDRs
	MaxBodyBytes int64           `json:"maxBodyBytes,omitempty"`
	PathPrefix   string          `json:"pathPrefix,omitempty"`
	StripPrefix  bool            `json:"stripPrefix,omitempty"`
}

// Headers changes the default response headers (DefaultHeaders). Off drops all
// defaults; Remove drops some; Set adds or replaces headers, and an empty
// value strips the header from responses (Traefik semantics).
type Headers struct {
	Off    bool              `json:"off,omitempty"`
	Set    map[string]string `json:"set,omitempty"`
	Remove []string          `json:"remove,omitempty"`
}

type RateLimit struct {
	Average int `json:"average"` // requests per second per client IP
	Burst   int `json:"burst"`
}

// BasicAuthUser holds a bcrypt hash generated by dermaga, never a password.
type BasicAuthUser struct {
	User string `json:"user"`
	Hash string `json:"hash"`
}

// DefaultHeaders reproduce vps-setup.sh's security-headers middleware and its
// per-app Permissions-Policy as plain response headers.
var DefaultHeaders = map[string]string{
	"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
	"X-Content-Type-Options":    "nosniff",
	"X-Frame-Options":           "SAMEORIGIN",
	"Referrer-Policy":           "strict-origin-when-cross-origin",
	"Permissions-Policy":        "camera=(), microphone=(), geolocation=(), payment=()",
	"Server":                    "",
	"X-Powered-By":              "",
}

// DefaultRateLimit is vps-setup.sh's RATE_LIMIT_AVERAGE / RATE_LIMIT_BURST.
var DefaultRateLimit = RateLimit{Average: 100, Burst: 200}

// EffectiveHeaders applies h to DefaultHeaders. Names are canonicalised, so
// "x-frame-options" replaces the default X-Frame-Options.
func (h Headers) EffectiveHeaders() map[string]string {
	out := map[string]string{}
	if !h.Off {
		for k, v := range DefaultHeaders {
			out[k] = v
		}
		for _, k := range h.Remove {
			delete(out, http.CanonicalHeaderKey(k))
		}
	}
	for k, v := range h.Set {
		out[http.CanonicalHeaderKey(k)] = v
	}
	return out
}

// SafeCaps is Docker's default capability set: an app may add these back
// after DropCapability=ALL, nothing else (same rule as vps-setup.sh).
var SafeCaps = []string{"AUDIT_WRITE", "CHOWN", "DAC_OVERRIDE", "FOWNER", "FSETID", "KILL", "MKNOD",
	"NET_BIND_SERVICE", "NET_RAW", "SETFCAP", "SETGID", "SETPCAP", "SETUID", "SYS_CHROOT"}

// Reserved names: vps-setup.sh's list plus names dermaga's own units use
// (dg-hooks, dg-db-*, dg-backup-*).
var reserved = []string{"traefik", "web", "podman", "vps", "hooks", "db", "backup", "dermaga"}

var (
	nameRE      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)
	domainRE    = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)
	headerRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`)
	pathRE      = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)
	healthRE    = regexp.MustCompile(`^/[A-Za-z0-9._~/?&=%+-]*$`)
	memoryRE    = regexp.MustCompile(`^[1-9][0-9]{0,5}[kmg]$`)
	cpusRE      = regexp.MustCompile(`^([1-9][0-9]?|0\.[0-9]?[1-9]|[1-9][0-9]?\.[0-9]{1,2})$`)
	authUserRE  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	bcryptRE    = regexp.MustCompile(`^\$2[aby]\$(0[4-9]|[12][0-9]|3[01])\$[./A-Za-z0-9]{53}$`)
	errNoDomain = errors.New("an app needs exactly one primary domain")
	repoRE      = regexp.MustCompile(`^git@github\.com:[A-Za-z0-9](-?[A-Za-z0-9]){0,38}/[A-Za-z0-9._-]{1,100}\.git$`)
	branchRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$`)
	relPathRE   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*(/[A-Za-z0-9_][A-Za-z0-9._-]*)*$`)
	imageRE     = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?/[a-z0-9]+([._/-][a-z0-9]+)*(:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?(@sha256:[a-f0-9]{64})?$`)
)

// ValidName reports whether s is a valid app, volume or database name
// (vps-setup.sh's valid_app_name, plus dermaga's reserved names). Apps can't
// start with "db-", so dg-<app>-<rel8> never looks like dg-db-<name>.
func ValidName(s string) bool {
	return nameRE.MatchString(s) && !slices.Contains(reserved, s) && !strings.HasPrefix(s, "db-")
}

// ValidIP reports whether s is an IP address or a CIDR.
func ValidIP(s string) bool {
	_, err := netip.ParsePrefix(s)
	if err != nil {
		_, err = netip.ParseAddr(s)
	}
	return err == nil
}

// ValidImage reports whether ref is a fully qualified image reference
// (vps-setup.sh's valid_image_ref): registry/path[:tag][@sha256:digest].
func ValidImage(ref string) bool {
	reg, _, _ := strings.Cut(ref, "/")
	return imageRE.MatchString(ref) && (strings.ContainsAny(reg, ".:") || reg == "localhost")
}

// NormalizeRepo turns https://github.com/owner/repo[.git] or owner/repo into
// the SSH form the deploy key needs. Other input is returned unchanged.
func NormalizeRepo(s string) string {
	t := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "github.com/"), ".git")
	if !strings.HasPrefix(s, "git@") && strings.Count(t, "/") == 1 {
		return "git@github.com:" + t + ".git"
	}
	return s
}

func ValidDomain(s string) bool { return len(s) <= 253 && domainRE.MatchString(s) }

// Text checks a free-text value (header value, health command): printable,
// no control characters, no surrounding whitespace, bounded length.
func Text(what, s string, max int) error {
	if len(s) > max {
		return fmt.Errorf("%s: longer than %d bytes", what, max)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s: control characters (line breaks, tabs, …) are not allowed", what)
		}
	}
	if strings.TrimSpace(s) != s {
		return fmt.Errorf("%s: leading or trailing spaces are not allowed", what)
	}
	return nil
}

// Validate checks every field. Generators call it before rendering.
func (a App) Validate() error {
	if !ValidName(a.Name) {
		return fmt.Errorf("app name %q: use 2-32 lowercase letters, digits and '-', starting with a letter (reserved: %s, db-*)", a.Name, strings.Join(reserved, ", "))
	}
	if err := validateDomains(a.Domains); err != nil {
		return err
	}
	if err := a.Source.validate(); err != nil {
		return err
	}
	if a.Strategy != "" && a.Strategy != "recreate" && a.Strategy != "blue-green" {
		return fmt.Errorf("strategy %q: recreate or blue-green", a.Strategy)
	}
	if a.Port < 1 || a.Port > 65535 {
		return fmt.Errorf("port %d: must be 1-65535", a.Port)
	}
	if err := a.Health.validate(); err != nil {
		return err
	}
	if err := a.Limits.validate(); err != nil {
		return err
	}
	for _, c := range a.AddCaps {
		if !slices.Contains(SafeCaps, c) {
			return fmt.Errorf("capability %q: allowed are %s", c, strings.Join(SafeCaps, " "))
		}
	}
	names, paths := map[string]bool{}, map[string]bool{}
	for _, v := range a.Volumes {
		if !ValidName(v.Name) || !pathRE.MatchString(v.Path) || strings.Contains(v.Path, "/..") || strings.Contains(v.Path, "/./") {
			return fmt.Errorf("volume %q at %q: invalid name or mount path", v.Name, v.Path)
		}
		if names[v.Name] || paths[v.Path] {
			return fmt.Errorf("volume %q at %q: duplicate name or mount path", v.Name, v.Path)
		}
		names[v.Name], paths[v.Path] = true, true
	}
	return a.Middleware.validate()
}

func validateDomains(ds []Domain) error {
	primaries, seen := 0, map[string]bool{}
	for _, d := range ds {
		if !ValidDomain(d.Host) {
			return fmt.Errorf("domain %q: not a valid lowercase hostname", d.Host)
		}
		if seen[d.Host] {
			return fmt.Errorf("domain %q: listed twice", d.Host)
		}
		seen[d.Host] = true
		if d.Primary {
			primaries++
			if d.Redirect {
				return fmt.Errorf("domain %q: the primary domain can't redirect", d.Host)
			}
		}
	}
	if primaries != 1 {
		return errNoDomain
	}
	return nil
}

// Primary returns the primary domain (Validate guarantees there is one).
func (a App) Primary() string {
	for _, d := range a.Domains {
		if d.Primary {
			return d.Host
		}
	}
	return ""
}

func (s Source) validate() error {
	switch s.Type {
	case "git":
		if !repoRE.MatchString(s.Repo) {
			return fmt.Errorf("repo %q: use git@github.com:owner/repo.git or https://github.com/owner/repo", s.Repo)
		}
		if !branchRE.MatchString(s.Branch) || strings.Contains(s.Branch, "..") || strings.HasSuffix(s.Branch, "/") || strings.HasSuffix(s.Branch, ".lock") {
			return fmt.Errorf("branch %q: invalid", s.Branch)
		}
		for _, p := range []string{s.Context, s.Containerfile} {
			if p != "" && (!relPathRE.MatchString(p) || strings.Contains(p, "..")) {
				return fmt.Errorf("path %q: must be relative to the repo and must not contain \"..\"", p)
			}
		}
		if s.Image != "" {
			return errors.New("git apps have no image reference")
		}
	case "image":
		if !ValidImage(s.Image) {
			return fmt.Errorf("image %q: use a fully qualified, pinned reference, e.g. docker.io/louislam/uptime-kuma:2.5.9", s.Image)
		}
		if s.Repo != "" || s.Branch != "" || s.Context != "" || s.Containerfile != "" || s.Trigger != "" || s.Workflow != "" || len(s.WatchPaths) > 0 {
			return errors.New("image apps have no repo, branch, build or webhook settings")
		}
	default:
		return fmt.Errorf("source type %q: git or image", s.Type)
	}
	switch s.Trigger {
	case "", "push":
		if s.Workflow != "" {
			return errors.New("workflow: only with trigger workflow_run")
		}
	case "workflow_run":
		if err := Text("workflow", s.Workflow, 200); err != nil || s.Workflow == "" {
			return errors.New("workflow: name or path of the workflow that gates deploys is required")
		}
	default:
		return fmt.Errorf("trigger %q: push or workflow_run", s.Trigger)
	}
	for _, w := range s.WatchPaths {
		if err := Text("watch path", w, 256); err != nil || w == "" {
			return fmt.Errorf("watch path %q: invalid", w)
		}
	}
	return nil
}

func (h Health) validate() error {
	if h.Path != "" && (len(h.Path) > 1024 || !healthRE.MatchString(h.Path)) {
		return fmt.Errorf("health path %q: must start with / and use URL path characters only", h.Path)
	}
	if err := Text("health command", h.Cmd, 1024); err != nil {
		return err
	}
	if strings.HasSuffix(h.Cmd, `\`) {
		return errors.New("health command: must not end with a backslash")
	}
	if h.Successes < 0 || h.Successes > 100 || h.TimeoutSec < 0 || h.TimeoutSec > 3600 {
		return errors.New("health: successes must be 0-100 and timeout 0-3600 s (0 = default)")
	}
	return nil
}

func (l Limits) validate() error {
	if l.Memory != "" && !memoryRE.MatchString(l.Memory) {
		return fmt.Errorf("memory %q: use a number with k, m or g, e.g. 512m", l.Memory)
	}
	if l.CPUs != "" && !cpusRE.MatchString(l.CPUs) {
		return fmt.Errorf("cpus %q: use e.g. 1, 2 or 0.5", l.CPUs)
	}
	if l.Pids < 0 || l.Pids > 32768 {
		return fmt.Errorf("pids %d: must be 1-32768 (0 = default)", l.Pids)
	}
	return nil
}

func (m Middleware) validate() error {
	for k, v := range m.Headers.Set {
		if !headerRE.MatchString(k) {
			return fmt.Errorf("header name %q: letters, digits and '-' only", k)
		}
		if err := Text("header "+k, v, 4096); err != nil {
			return err
		}
	}
	for _, k := range m.Headers.Remove {
		if !headerRE.MatchString(k) {
			return fmt.Errorf("header name %q: letters, digits and '-' only", k)
		}
	}
	if r := m.RateLimit; r != nil && (r.Average < 1 || r.Average > 1_000_000 || r.Burst < 1 || r.Burst > 1_000_000) {
		return errors.New("rate limit: average and burst must be 1-1000000")
	}
	users := map[string]bool{}
	for _, u := range m.BasicAuth {
		if !authUserRE.MatchString(u.User) || users[u.User] {
			return fmt.Errorf("basic auth user %q: invalid or duplicate (letters, digits, . _ -)", u.User)
		}
		if !bcryptRE.MatchString(u.Hash) {
			return fmt.Errorf("basic auth user %q: hash is not a bcrypt hash", u.User)
		}
		users[u.User] = true
	}
	for _, s := range m.IPAllowList {
		if !ValidIP(s) {
			return fmt.Errorf("IP allowlist entry %q: not an IP address or CIDR", s)
		}
	}
	if m.MaxBodyBytes < 0 {
		return errors.New("max body size: must not be negative")
	}
	if m.PathPrefix != "" && (len(m.PathPrefix) > 256 || !pathRE.MatchString(m.PathPrefix) || strings.Contains(m.PathPrefix, "/..")) {
		return fmt.Errorf("path prefix %q: like /app, no trailing slash", m.PathPrefix)
	}
	if m.StripPrefix && m.PathPrefix == "" {
		return errors.New("strip prefix: needs a path prefix")
	}
	return nil
}
