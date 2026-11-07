package traefik

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/r3p-dev/dermaga/internal/golden"
	"github.com/r3p-dev/dermaga/internal/spec"
)

const (
	backend = "dg-shop-1a2b3c4d"
	token   = "0123456789abcdef0123456789abcdef"
	// bcrypt of "secret", cost 10
	hash = "$2a$10$XHrAnsxjPHLuFmCNWUIDdeat./VrrO97Hsg2DjDInBZAYmb2bWA5y"
)

func app(mw spec.Middleware, domains ...spec.Domain) spec.App {
	if len(domains) == 0 {
		domains = []spec.Domain{{Host: "shop.example.com", Primary: true}}
	}
	return spec.App{Name: "shop", Domains: domains, Port: 8080, Middleware: mw, Source: spec.Source{Type: "image", Image: "docker.io/library/nginx:1"}}
}

var (
	auth      = []spec.BasicAuthUser{{User: "admin", Hash: hash}, {User: "ops", Hash: hash}}
	allowlist = []string{"203.0.113.7", "2001:db8::/32", "10.0.0.0/8"}
	everyOn   = spec.Middleware{
		Headers:      spec.Headers{Set: map[string]string{"Content-Security-Policy": "default-src 'self'"}},
		RateLimit:    &spec.RateLimit{Average: 20, Burst: 40},
		BasicAuth:    auth,
		IPAllowList:  allowlist,
		MaxBodyBytes: 10 << 20,
		PathPrefix:   "/shop",
		StripPrefix:  true,
	}
)

// appCases are the PLAN M2 golden cases. Each one is rendered twice: the
// steady-state file and the deploy-time file with the verify router.
var appCases = map[string]spec.App{
	"minimal":        app(spec.Middleware{}),
	"every-toggle":   app(everyOn),
	"everything-off": app(spec.Middleware{Headers: spec.Headers{Off: true}, NoCompress: true, NoRateLimit: true}),
	"aliases-www": app(spec.Middleware{},
		spec.Domain{Host: "example.com", Primary: true},
		spec.Domain{Host: "www.example.com", Redirect: true},
		spec.Domain{Host: "shop.example.net", Redirect: true},
		spec.Domain{Host: "example.org"}), // served directly, not redirected
	"apex-to-www": app(spec.Middleware{},
		spec.Domain{Host: "www.example.com", Primary: true},
		spec.Domain{Host: "example.com", Redirect: true}),
	"path-prefix-strip": app(spec.Middleware{PathPrefix: "/shop", StripPrefix: true},
		spec.Domain{Host: "example.com", Primary: true}, spec.Domain{Host: "example.org"}),
	"path-prefix-keep": app(spec.Middleware{PathPrefix: "/api/v1"}),
	"basic-auth":       app(spec.Middleware{BasicAuth: auth}),
	"allowlist":        app(spec.Middleware{IPAllowList: allowlist}),
	// D7: both access controls plus headers, compression and a custom rate limit
	"auth-allowlist-headers": app(spec.Middleware{
		BasicAuth: auth, IPAllowList: allowlist, RateLimit: &spec.RateLimit{Average: 5, Burst: 10},
		Headers: spec.Headers{Set: map[string]string{"X-Robots-Tag": "noindex"}},
	}),
	"body-limit": app(spec.Middleware{MaxBodyBytes: 1 << 20}),
	"headers-custom-removed": app(spec.Middleware{Headers: spec.Headers{
		Set:    map[string]string{"content-security-policy": "default-src 'self'", "x-frame-options": "DENY", "Server": "dermaga-test"},
		Remove: []string{"Permissions-Policy", "strict-transport-security"},
	}}),
}

func TestAppGolden(t *testing.T) {
	for name, a := range appCases {
		t.Run(name, func(t *testing.T) {
			steady, err := App(a, backend, "", Site{})
			if err != nil {
				t.Fatal(err)
			}
			golden.Check(t, "app-"+name+".json", steady)
			deploy, err := App(a, backend, token, Site{})
			if err != nil {
				t.Fatal(err)
			}
			golden.Check(t, "app-"+name+".verify.json", deploy)
		})
	}
}

func TestAppBehindTrustedProxy(t *testing.T) {
	b, err := App(app(spec.Middleware{IPAllowList: allowlist}), backend, "", Site{TrustedProxies: []string{"173.245.48.0/20"}})
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "app-trusted-proxy.json", b)
}

// TestVerifyRouter checks ARCHITECTURE D7 on every case, not only in goldens.
func TestVerifyRouter(t *testing.T) {
	for name, a := range appCases {
		steady := decode(t, a, "")
		if _, ok := steady.HTTP.Routers["app-shop-verify"]; ok {
			t.Errorf("%s: steady-state file has a verify router", name)
		}
		for n, r := range steady.HTTP.Routers {
			if !slices.Equal(r.EntryPoints, []string{"websecure"}) {
				t.Errorf("%s: router %s on %v", name, n, r.EntryPoints)
			}
		}

		d := decode(t, a, token)
		pub, v := d.HTTP.Routers["app-shop"], d.HTTP.Routers["app-shop-verify"]
		if !slices.Equal(v.EntryPoints, []string{"verify"}) || !slices.Equal(pub.EntryPoints, []string{"websecure"}) {
			t.Errorf("%s: verify on %v, public on %v", name, v.EntryPoints, pub.EntryPoints)
		}
		for n, r := range d.HTTP.Routers {
			if n != "app-shop-verify" && slices.Contains(r.EntryPoints, "verify") {
				t.Errorf("%s: %s is attached to verify", name, n)
			}
		}
		if v.Service != pub.Service || v.TLS != nil {
			t.Errorf("%s: verify service %q tls %v", name, v.Service, v.TLS)
		}
		want := slices.DeleteFunc(slices.Clone(pub.Middlewares), func(m string) bool {
			return d.HTTP.Middlewares[m].BasicAuth != nil || d.HTTP.Middlewares[m].IPAllowList != nil
		})
		if !slices.Equal(v.Middlewares, want) {
			t.Errorf("%s: verify middlewares %v, want public %v minus basicAuth/ipAllowList", name, v.Middlewares, pub.Middlewares)
		}
		wantRule := "Host(`" + a.Primary() + "`) && Header(`X-Dermaga-Verify`, `" + token + "`)"
		if v.Rule != wantRule {
			t.Errorf("%s: verify rule %s", name, v.Rule)
		}
	}
	// the D7 case: everything except basicAuth and ipAllowList stays
	v := decode(t, appCases["auth-allowlist-headers"], token).HTTP.Routers["app-shop-verify"]
	if !slices.Equal(v.Middlewares, []string{"app-shop-headers", "app-shop-ratelimit", "app-shop-compress"}) {
		t.Errorf("verify middlewares %v", v.Middlewares)
	}
}

func decode(t *testing.T, a spec.App, tok string) Dynamic {
	t.Helper()
	b, err := App(a, backend, tok, Site{})
	if err != nil {
		t.Fatal(err)
	}
	var d Dynamic
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

// Injection: newline, %, $, backtick and quotes in every user-controlled field.
func TestInjectionRejectedOrEscaped(t *testing.T) {
	bad := []string{"a\nb", "a\rb", "100%", "$x", "a`b", `a"b`, "a'b", "a b", "x`) || Host(`evil.com"}
	for _, s := range bad {
		for field, a := range map[string]spec.App{
			"domain":      app(spec.Middleware{}, spec.Domain{Host: "shop.example.com" + s, Primary: true}),
			"alias":       app(spec.Middleware{}, spec.Domain{Host: "shop.example.com", Primary: true}, spec.Domain{Host: s + ".example.com"}),
			"path prefix": app(spec.Middleware{PathPrefix: "/x" + s}),
			"header name": app(spec.Middleware{Headers: spec.Headers{Set: map[string]string{"X-" + s: "v"}}}),
			"remove name": app(spec.Middleware{Headers: spec.Headers{Remove: []string{"X-" + s}}}),
			"auth user":   app(spec.Middleware{BasicAuth: []spec.BasicAuthUser{{User: "u" + s, Hash: hash}}}),
			"auth hash":   app(spec.Middleware{BasicAuth: []spec.BasicAuthUser{{User: "u", Hash: hash + s}}}),
			"allowlist":   app(spec.Middleware{IPAllowList: []string{"10.0.0.1" + s}}),
		} {
			if _, err := App(a, backend, "", Site{}); err == nil {
				t.Errorf("%s %q: accepted", field, s)
			}
		}
		if _, err := App(app(spec.Middleware{}), backend+s, "", Site{}); err == nil {
			t.Errorf("backend %q: accepted", s)
		}
		if _, err := App(app(spec.Middleware{}), backend, token+s, Site{}); err == nil {
			t.Errorf("token %q: accepted", s)
		}
		if _, err := Global(Site{HookHost: "hooks.example.com" + s}); err == nil {
			t.Errorf("hook host %q: accepted", s)
		}
	}
	// header values are free text: line breaks are rejected, the rest is a JSON string value
	if _, err := App(app(spec.Middleware{Headers: spec.Headers{Set: map[string]string{"X-A": "a\nX-Evil: 1"}}}), backend, "", Site{}); err == nil {
		t.Error("header value with a line break accepted")
	}
	v := "default-src 'self' \"x\" `y` 100% $HOME \\"
	d := decode(t, app(spec.Middleware{Headers: spec.Headers{Set: map[string]string{"Content-Security-Policy": v}}}), token)
	if got := d.HTTP.Middlewares["app-shop-headers"].Headers.CustomResponseHeaders["Content-Security-Policy"]; got != v {
		t.Errorf("header value round trip: %q", got)
	}
	if r := d.HTTP.Routers["app-shop"].Rule; r != "Host(`shop.example.com`)" {
		t.Errorf("rule changed: %s", r)
	}
}

func TestGlobalAndStaticGolden(t *testing.T) {
	cases := map[string]Site{
		"default": {},
		"hooks-trusted-staging": {
			HookHost: "hooks.example.com", TrustedProxies: []string{"173.245.48.0/20", "2400:cb00::/32"},
			ACMEEmail: "admin@example.com", ACMEStaging: true,
		},
	}
	for name, site := range cases {
		g, err := Global(site)
		if err != nil {
			t.Fatal(err)
		}
		golden.Check(t, "00-global-"+name+".json", g)
		s, err := StaticConfig(site)
		if err != nil {
			t.Fatal(err)
		}
		golden.Check(t, "traefik-"+name+".json", s)
		if strings.Contains(string(s), "docker") || strings.Contains(string(s), "secure@file") {
			t.Errorf("%s: static config has the docker provider or entrypoint middlewares", name)
		}
	}
}

// Every rendered file must be valid JSON (and therefore YAML) with unique keys.
func TestOutputIsJSON(t *testing.T) {
	for name, a := range appCases {
		b, _ := App(a, backend, token, Site{})
		if !json.Valid(b) || strings.Contains(string(b), "\t") {
			t.Errorf("%s: not valid JSON or has tabs (YAML forbids tab indentation)", name)
		}
	}
}
