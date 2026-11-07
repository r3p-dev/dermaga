package spec

import (
	"strings"
	"testing"

	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
)

func valid() App {
	return App{Name: "shop", Port: 8080, Domains: []Domain{{Host: "shop.example.com", Primary: true}}}
}

func TestValidate(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*App){
		"reserved name":       func(a *App) { a.Name = "hooks" },
		"db- prefix":          func(a *App) { a.Name = "db-shop" },
		"uppercase name":      func(a *App) { a.Name = "Shop" },
		"no primary":          func(a *App) { a.Domains[0].Primary = false },
		"two primaries":       func(a *App) { a.Domains = append(a.Domains, Domain{Host: "b.example.com", Primary: true}) },
		"duplicate host":      func(a *App) { a.Domains = append(a.Domains, Domain{Host: "shop.example.com"}) },
		"primary redirects":   func(a *App) { a.Domains[0].Redirect = true },
		"uppercase host":      func(a *App) { a.Domains[0].Host = "Shop.example.com" },
		"port 0":              func(a *App) { a.Port = 0 },
		"unsafe capability":   func(a *App) { a.AddCaps = []string{"SYS_ADMIN"} },
		"bad memory":          func(a *App) { a.Limits.Memory = "1.5g" },
		"cpus 0":              func(a *App) { a.Limits.CPUs = "0" },
		"volume path ..":      func(a *App) { a.Volumes = []Volume{{Name: "data", Path: "/a/../etc"}} },
		"volume root":         func(a *App) { a.Volumes = []Volume{{Name: "data", Path: "/"}} },
		"duplicate volume":    func(a *App) { a.Volumes = []Volume{{Name: "data", Path: "/a"}, {Name: "data", Path: "/b"}} },
		"health cmd newline":  func(a *App) { a.Health.Cmd = "true\nfalse" },
		"health cmd trailing": func(a *App) { a.Health.Cmd = `echo \` },
		"health path":         func(a *App) { a.Health.Path = "health" },
		"rate limit 0":        func(a *App) { a.Middleware.RateLimit = &RateLimit{} },
		"bad hash":            func(a *App) { a.Middleware.BasicAuth = []BasicAuthUser{{User: "a", Hash: "plain"}} },
		"bad cidr":            func(a *App) { a.Middleware.IPAllowList = []string{"10.0.0.0/33"} },
		"negative body":       func(a *App) { a.Middleware.MaxBodyBytes = -1 },
		"prefix slash":        func(a *App) { a.Middleware.PathPrefix = "/app/" },
		"strip w/o prefix":    func(a *App) { a.Middleware.StripPrefix = true },
	}
	for n, f := range bad {
		a := valid()
		f(&a)
		if err := a.Validate(); err == nil {
			t.Errorf("%s: accepted", n)
		}
	}
	for _, c := range []string{"1", "2", "0.5", "0.25", "1.5", "16"} {
		a := valid()
		a.Limits.CPUs = c
		if err := a.Validate(); err != nil {
			t.Errorf("cpus %s: %v", c, err)
		}
	}
}

func TestEffectiveHeaders(t *testing.T) {
	h := Headers{
		Set:    map[string]string{"x-frame-options": "DENY", "content-security-policy": "default-src 'self'"},
		Remove: []string{"permissions-policy"},
	}.EffectiveHeaders()
	if h["X-Frame-Options"] != "DENY" || h["Content-Security-Policy"] == "" || len(h) != len(DefaultHeaders)+1-1 {
		t.Errorf("%v", h)
	}
	if _, ok := h["Permissions-Policy"]; ok {
		t.Error("removed default still present")
	}
	if v, ok := h["Server"]; !ok || v != "" {
		t.Error("Server must be stripped by default")
	}
	off := Headers{Off: true, Set: map[string]string{"X-A": "b"}}.EffectiveHeaders()
	if len(off) != 1 || !strings.EqualFold("b", off["X-A"]) {
		t.Errorf("off: %v", off)
	}
}
