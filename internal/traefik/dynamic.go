// Package traefik renders Traefik's static config and the dynamic files
// dermaga owns (00-global.yml, app-<name>.yml) from typed structs as JSON
// (ARCHITECTURE D6: JSON is valid YAML, so no YAML library). User input only
// ever becomes a JSON string value, and every value is validated first.
package traefik

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/r3p-dev/dermaga/internal/name"
	"github.com/r3p-dev/dermaga/internal/spec"
)

// Dynamic is a file-provider document.
type Dynamic struct {
	HTTP *HTTP `json:"http,omitempty"`
	TLS  *TLS  `json:"tls,omitempty"`
}

type HTTP struct {
	Routers     map[string]Router     `json:"routers,omitempty"`
	Services    map[string]Service    `json:"services,omitempty"`
	Middlewares map[string]Middleware `json:"middlewares,omitempty"`
}

type Router struct {
	EntryPoints []string   `json:"entryPoints"`
	Rule        string     `json:"rule"`
	Service     string     `json:"service"`
	Middlewares []string   `json:"middlewares,omitempty"`
	TLS         *RouterTLS `json:"tls,omitempty"`
}

type RouterTLS struct {
	CertResolver string `json:"certResolver"`
}

type Service struct {
	LoadBalancer LoadBalancer `json:"loadBalancer"`
}

type LoadBalancer struct {
	Servers []Server `json:"servers"`
}

type Server struct {
	URL string `json:"url"`
}

// Middleware holds exactly one of its fields.
type Middleware struct {
	Headers       *Headers       `json:"headers,omitempty"`
	RateLimit     *RateLimit     `json:"rateLimit,omitempty"`
	Compress      *Compress      `json:"compress,omitempty"`
	BasicAuth     *BasicAuth     `json:"basicAuth,omitempty"`
	IPAllowList   *IPAllowList   `json:"ipAllowList,omitempty"`
	Buffering     *Buffering     `json:"buffering,omitempty"`
	StripPrefix   *StripPrefix   `json:"stripPrefix,omitempty"`
	RedirectRegex *RedirectRegex `json:"redirectRegex,omitempty"`
	Chain         *Chain         `json:"chain,omitempty"`
}

type Headers struct {
	STSSeconds              int64             `json:"stsSeconds,omitempty"`
	STSIncludeSubdomains    bool              `json:"stsIncludeSubdomains,omitempty"`
	ContentTypeNosniff      bool              `json:"contentTypeNosniff,omitempty"`
	CustomFrameOptionsValue string            `json:"customFrameOptionsValue,omitempty"`
	ReferrerPolicy          string            `json:"referrerPolicy,omitempty"`
	CustomResponseHeaders   map[string]string `json:"customResponseHeaders,omitempty"` // "" removes the header
}

type RateLimit struct {
	Average         int              `json:"average"`
	Burst           int              `json:"burst"`
	Period          string           `json:"period"`
	SourceCriterion *SourceCriterion `json:"sourceCriterion,omitempty"`
}

type SourceCriterion struct {
	IPStrategy IPStrategy `json:"ipStrategy"`
}

type IPStrategy struct {
	Depth int `json:"depth"`
}

type Compress struct {
	MinResponseBodyBytes int `json:"minResponseBodyBytes"`
}

type BasicAuth struct {
	Users []string `json:"users"`
}

type IPAllowList struct {
	SourceRange []string    `json:"sourceRange"`
	IPStrategy  *IPStrategy `json:"ipStrategy,omitempty"`
}

type Buffering struct {
	MaxRequestBodyBytes int64 `json:"maxRequestBodyBytes"`
}

type StripPrefix struct {
	Prefixes []string `json:"prefixes"`
}

type RedirectRegex struct {
	Regex       string `json:"regex"`
	Replacement string `json:"replacement"`
	Permanent   bool   `json:"permanent"`
}

type Chain struct {
	Middlewares []string `json:"middlewares"`
}

type TLS struct {
	Options map[string]TLSOptions `json:"options"`
}

type TLSOptions struct {
	MinVersion   string   `json:"minVersion"`
	SNIStrict    bool     `json:"sniStrict"`
	CipherSuites []string `json:"cipherSuites"`
}

// Site holds the host-wide settings the generated files depend on.
type Site struct {
	TrustedProxies []string // CIDRs of a proxy/CDN in front (vps-setup's TRUSTED_PROXY_IPS)
	HookHost       string   // public host of the webhook endpoint; "" = no hook route
	ACMEEmail      string
	ACMEStaging    bool
}

const (
	certResolver = "le"
	// VerifyHeader carries the per-deploy token on the verify entrypoint (D7).
	VerifyHeader = "X-Dermaga-Verify"
	// VerifyEntryPoint is the internal plain-HTTP entrypoint (D7), never published.
	VerifyEntryPoint = "verify"
	VerifyPort       = 8081
)

var tokenRE = regexp.MustCompile(`^[0-9a-f]{32,64}$`)

// GlobalFile and CustomFile are the other files in the dynamic directory.
const (
	GlobalFile = "00-global.yml"
	CustomFile = "99-custom.yml"
)

// customPlaceholder is written once if 99-custom.yml is missing; dermaga
// never touches the file again (BRIEF §3.3).
const customPlaceholder = `# 99-custom.yml: your own Traefik dynamic configuration (file provider).
# dermaga created this file once and never changes or deletes it.
# Anything here is hot-reloaded. An invalid file stops Traefik from loading
# ANY dynamic change (dermaga's deploys then fail verification and roll back):
# check "journalctl --user -u traefik" after editing.
#
# The websecure entrypoint has no middlewares of its own (ARCHITECTURE D27).
# Routes defined here must name secure@file to get the default security
# headers, rate limit and compression, for example:
#
# http:
#   routers:
#     my-route:
#       rule: Host(` + "`example.com`" + `)
#       entryPoints: [websecure]
#       tls: {certResolver: le}
#       middlewares: [secure@file]
#       service: my-service
#   services:
#     my-service:
#       loadBalancer:
#         servers:
#           - url: http://my-container:8080
`

// WriteCustomPlaceholder creates 99-custom.yml in dir unless it exists.
func WriteCustomPlaceholder(dir string) error {
	f, err := os.OpenFile(filepath.Join(dir, CustomFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := f.WriteString(customPlaceholder); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// AppFileName is the dynamic file of an app.
func AppFileName(app string) string { return name.AppFile + app + ".yml" }

// VerifyRouter is the name of an app's deploy-time token router.
func VerifyRouter(app string) string { return name.AppFile + app + "-verify" }

// App renders app-<name>.yml with the service pointing at backend (the
// container dg-<app>-<rel8>). verifyToken != "" adds the token router
// app-<name>-verify on the verify entrypoint only (ARCHITECTURE D7): same
// service, the public router's middlewares minus basicAuth and ipAllowList.
// The steady-state file (verifyToken == "") has no verify router.
func App(a spec.App, backend, verifyToken string, site Site) ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if !backendRE.MatchString(backend) {
		return nil, fmt.Errorf("backend %q: not a container name", backend)
	}
	if verifyToken != "" && !tokenRE.MatchString(verifyToken) {
		return nil, fmt.Errorf("verify token: want 32-64 lowercase hex characters")
	}
	if err := site.validate(); err != nil {
		return nil, err
	}
	base := name.AppFile + a.Name
	mw := a.Middleware
	h := HTTP{
		Routers:     map[string]Router{},
		Middlewares: map[string]Middleware{},
		Services: map[string]Service{base: {LoadBalancer: LoadBalancer{
			Servers: []Server{{URL: fmt.Sprintf("http://%s:%d", backend, a.Port)}},
		}}},
	}
	// Order: headers first, so 401/403/429 answers carry them too; then the
	// cheap rejections; compression and prefix stripping last.
	var chain, verifyChain []string
	add := func(suffix string, m Middleware, onVerify bool) {
		n := base + "-" + suffix
		h.Middlewares[n] = m
		chain = append(chain, n)
		if onVerify {
			verifyChain = append(verifyChain, n)
		}
	}
	if hs := mw.Headers.EffectiveHeaders(); len(hs) > 0 {
		add("headers", Middleware{Headers: &Headers{CustomResponseHeaders: hs}}, true)
	}
	if len(mw.IPAllowList) > 0 {
		add("allowlist", Middleware{IPAllowList: &IPAllowList{SourceRange: mw.IPAllowList, IPStrategy: site.ipStrategy()}}, false)
	}
	if !mw.NoRateLimit {
		rl := spec.DefaultRateLimit
		if mw.RateLimit != nil {
			rl = *mw.RateLimit
		}
		add("ratelimit", Middleware{RateLimit: site.rateLimit(rl)}, true)
	}
	if len(mw.BasicAuth) > 0 {
		var users []string
		for _, u := range mw.BasicAuth {
			users = append(users, u.User+":"+u.Hash)
		}
		add("auth", Middleware{BasicAuth: &BasicAuth{Users: users}}, false)
	}
	if mw.MaxBodyBytes > 0 {
		add("body", Middleware{Buffering: &Buffering{MaxRequestBodyBytes: mw.MaxBodyBytes}}, true)
	}
	if !mw.NoCompress {
		add("compress", Middleware{Compress: &Compress{MinResponseBodyBytes: 1024}}, true)
	}
	if mw.StripPrefix {
		add("strip", Middleware{StripPrefix: &StripPrefix{Prefixes: []string{mw.PathPrefix}}}, true)
	}

	primary := a.Primary()
	var served, redirected []string
	for _, d := range a.Domains {
		if d.Redirect {
			redirected = append(redirected, d.Host)
		} else {
			served = append(served, d.Host)
		}
	}
	rule := hostRule(served)
	if mw.PathPrefix != "" {
		if len(served) > 1 {
			rule = "(" + rule + ")"
		}
		rule += " && PathPrefix(`" + mw.PathPrefix + "`)"
	}
	tls := &RouterTLS{CertResolver: certResolver}
	h.Routers[base] = Router{EntryPoints: []string{"websecure"}, Rule: rule, Service: base, Middlewares: chain, TLS: tls}
	if len(redirected) > 0 {
		h.Middlewares[base+"-redirect"] = Middleware{RedirectRegex: &RedirectRegex{
			Regex:       `^https?://[^/]+(.*)$`,
			Replacement: "https://" + primary + "${1}",
			Permanent:   true,
		}}
		h.Routers[base+"-redirect"] = Router{
			EntryPoints: []string{"websecure"}, Rule: hostRule(redirected), Service: base,
			Middlewares: []string{base + "-redirect"}, TLS: tls,
		}
	}
	if verifyToken != "" {
		h.Routers[VerifyRouter(a.Name)] = Router{
			EntryPoints: []string{VerifyEntryPoint},
			Rule:        "Host(`" + primary + "`) && Header(`" + VerifyHeader + "`, `" + verifyToken + "`)",
			Service:     base,
			Middlewares: verifyChain,
		}
	}
	return marshal(Dynamic{HTTP: &h})
}

var backendRE = regexp.MustCompile(`^` + name.Unit + `[a-z][a-z0-9-]{0,40}[a-z0-9]$`)

func hostRule(hosts []string) string {
	var b bytes.Buffer
	for i, h := range hosts {
		if i > 0 {
			b.WriteString(" || ")
		}
		b.WriteString("Host(`" + h + "`)") // hosts passed spec.ValidDomain: no backticks
	}
	return b.String()
}

func (s Site) validate() error {
	if s.HookHost != "" && !spec.ValidDomain(s.HookHost) {
		return fmt.Errorf("hook host %q: not a valid hostname", s.HookHost)
	}
	for _, c := range s.TrustedProxies {
		if !spec.ValidIP(c) {
			return fmt.Errorf("trusted proxy %q: not an IP address or CIDR", c)
		}
	}
	if s.ACMEEmail != "" {
		if err := spec.Text("ACME email", s.ACMEEmail, 254); err != nil || !strings.Contains(s.ACMEEmail, "@") {
			return fmt.Errorf("ACME email %q: invalid", s.ACMEEmail)
		}
	}
	return nil
}

// Behind a trusted proxy the client is the last X-Forwarded-For hop, as in vps-setup.sh.
func (s Site) ipStrategy() *IPStrategy {
	if len(s.TrustedProxies) == 0 {
		return nil
	}
	return &IPStrategy{Depth: 1}
}

func (s Site) rateLimit(rl spec.RateLimit) *RateLimit {
	r := &RateLimit{Average: rl.Average, Burst: rl.Burst, Period: "1s"}
	if st := s.ipStrategy(); st != nil {
		r.SourceCriterion = &SourceCriterion{IPStrategy: *st}
	}
	return r
}

// Global renders 00-global.yml: vps-setup.sh's 00-vps.yml middlewares and TLS
// options, for the hook route and for 99-custom.yml routes (as secure@file).
// App routers carry their own copies, so each app can change them.
func Global(site Site) ([]byte, error) {
	if err := site.validate(); err != nil {
		return nil, err
	}
	h := HTTP{Middlewares: map[string]Middleware{
		"secure": {Chain: &Chain{Middlewares: []string{"security-headers", "rate-limit", "compress"}}},
		"security-headers": {Headers: &Headers{
			STSSeconds:              31536000,
			STSIncludeSubdomains:    true,
			ContentTypeNosniff:      true,
			CustomFrameOptionsValue: "SAMEORIGIN",
			ReferrerPolicy:          "strict-origin-when-cross-origin",
			CustomResponseHeaders:   map[string]string{"Server": "", "X-Powered-By": ""},
		}},
		"rate-limit": {RateLimit: site.rateLimit(spec.DefaultRateLimit)},
		"compress":   {Compress: &Compress{MinResponseBodyBytes: 1024}},
	}}
	if site.HookHost != "" {
		n := name.Name + "-hooks"
		h.Routers = map[string]Router{n: {
			EntryPoints: []string{"websecure"},
			Rule:        "Host(`" + site.HookHost + "`) && PathPrefix(`/hooks/`)",
			Service:     n,
			Middlewares: []string{"secure"},
			TLS:         &RouterTLS{CertResolver: certResolver},
		}}
		h.Services = map[string]Service{n: {LoadBalancer: LoadBalancer{Servers: []Server{{URL: "http://" + name.Unit + "hooks:8080"}}}}}
	}
	return marshal(Dynamic{
		HTTP: &h,
		TLS: &TLS{Options: map[string]TLSOptions{"default": {
			MinVersion: "VersionTLS12",
			SNIStrict:  true,
			CipherSuites: []string{
				"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
				"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
				"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305",
				"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305",
			},
		}}},
	})
}

// marshal writes indented JSON with two spaces (YAML forbids tab indentation)
// and without HTML escaping, so values read as written.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
