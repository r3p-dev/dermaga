package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"dermaga/internal/store"
)

func domainsOf(t *testing.T, body []byte) DomainsResponse {
	t.Helper()
	var resp DomainsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	return resp
}

// TestDomainsAPI_APP2_TLS2_TLS3 covers authentication on the three routes, replacing the domain
// set (roles, path prefix, DNS gate for new domains, statuses) and "Check again".
func TestDomainsAPI_APP2_TLS2_TLS3(t *testing.T) {
	srv, s, fh, token := setupAppsTestServer(t)
	seedReleases(t, srv, s)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/apps/app_shop/domains"}, {"PUT", "/api/apps/app_shop/domains"}, {"POST", "/api/apps/app_shop/domains/check"},
	} {
		if rr := doAPIReq(srv, tc.method, tc.path, "", map[string]any{}); rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without session: %d", tc.method, tc.path, rr.Code)
		}
	}

	rr := doAPIReq(srv, "PUT", "/api/apps/app_shop/domains", token, UpdateDomainsRequest{
		Domains: []DomainInput{
			{Domain: "Shop.Example.com.", Role: "primary"},
			{Domain: "ok.shop.example.com", Role: "alias"},
			{Domain: "www.shop.example.com", Role: "redirect"},
		},
		PathPrefix: "/blog", StripPrefix: true,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rr.Code, rr.Body.String())
	}
	resp := domainsOf(t, rr.Body.Bytes())
	if !resp.Live || resp.PathPrefix != "/blog" || !resp.StripPrefix || len(resp.Domains) != 3 {
		t.Fatalf("response: %+v", resp)
	}
	byName := map[string]DomainView{}
	for _, d := range resp.Domains {
		byName[d.Domain] = d
	}
	if d := byName["shop.example.com"]; d.Role != "primary" || d.Certificate.Status != "waiting_dns" || d.DNS == nil || !strings.Contains(d.DNS.Warning, "203.0.113.10") {
		t.Fatalf("primary (DNS not pointing here, record shown): %+v", d)
	}
	if d := byName["ok.shop.example.com"]; d.Role != "alias" || !d.DNSVerified || d.Certificate.Status != "requesting" || d.DNS != nil {
		t.Fatalf("alias whose DNS points here must pass the gate: %+v", d)
	}
	if d := byName["www.shop.example.com"]; d.Role != "redirect" || d.DNSVerified {
		t.Fatalf("redirect: %+v", d)
	}
	route, _ := fh.ReadFile(filepath.Join(srv.cfg.Deploy.RouteDir(), "app-shop.yml"))
	if r := string(route); !strings.Contains(r, "Host(`ok.shop.example.com`) \\u0026\\u0026 PathPrefix(`/blog`)") || strings.Contains(r, "Host(`www.shop.example.com`)") {
		t.Fatalf("route: only domains past the DNS gate are routed:\n%s", r)
	}
	ds, _ := s.ListDomainsByApp(context.Background(), "app_shop")
	if len(ds) != 3 || ds[2].RedirectTo == nil || *ds[2].RedirectTo != "shop.example.com" {
		t.Fatalf("stored: %+v", ds)
	}

	if rr := doAPIReq(srv, "POST", "/api/apps/app_shop/domains/check", token, nil); rr.Code != http.StatusOK {
		t.Fatalf("check again: %d %s", rr.Code, rr.Body.String())
	}
	if rr := doAPIReq(srv, "GET", "/api/apps/app_shop/domains", token, nil); rr.Code != http.StatusOK || len(domainsOf(t, rr.Body.Bytes()).Domains) != 3 {
		t.Fatalf("GET: %d %s", rr.Code, rr.Body.String())
	}
}

// TestDomainsAPI_Validation_APP2: every refused set names the field and leaves the domains as they were.
func TestDomainsAPI_Validation_APP2(t *testing.T) {
	srv, s, _, token := setupAppsTestServer(t)
	seedReleases(t, srv, s)
	ctx := context.Background()
	_ = s.CreateApp(ctx, &store.App{ID: "app_other", Name: "other", RepoOwner: "o", RepoName: "r", Port: 80, HealthType: "none"})
	_ = s.CreateDomain(ctx, &store.Domain{ID: "dom_o", AppID: "app_other", Domain: "other.example.com", IsPrimary: true})

	p := func(d string) DomainInput { return DomainInput{Domain: d, Role: "primary"} }
	a := func(d string) DomainInput { return DomainInput{Domain: d, Role: "alias"} }
	cases := map[string]struct {
		req   UpdateDomainsRequest
		field string
		want  string
	}{
		"hook domain":     {UpdateDomainsRequest{Domains: []DomainInput{p("hooks.example.com")}}, "domains.0", "hook domain"},
		"other app":       {UpdateDomainsRequest{Domains: []DomainInput{p("shop.example.com"), a("other.example.com")}}, "domains.1", "another app"},
		"wildcard":        {UpdateDomainsRequest{Domains: []DomainInput{p("*.example.com")}}, "domains.0", "Wildcard"},
		"non-ascii":       {UpdateDomainsRequest{Domains: []DomainInput{p("bücher.example.com")}}, "domains.0", "xn--"},
		"injection":       {UpdateDomainsRequest{Domains: []DomainInput{p("a.example.com`) || Host(`x")}}, "domains.0", "not a valid domain"},
		"no dot":          {UpdateDomainsRequest{Domains: []DomainInput{p("intranet")}}, "domains.0", "not a valid domain"},
		"duplicate":       {UpdateDomainsRequest{Domains: []DomainInput{p("shop.example.com"), a("SHOP.example.com")}}, "domains.1", "twice"},
		"two primaries":   {UpdateDomainsRequest{Domains: []DomainInput{p("shop.example.com"), p("b.example.com")}}, "domains.1", "Only one"},
		"no primary":      {UpdateDomainsRequest{Domains: []DomainInput{a("shop.example.com")}}, "domains", "primary"},
		"none":            {UpdateDomainsRequest{}, "domains", "between 1 and"},
		"bad role":        {UpdateDomainsRequest{Domains: []DomainInput{p("shop.example.com"), {Domain: "b.example.com", Role: "mirror"}}}, "domains.1", "role"},
		"prefix slash":    {UpdateDomainsRequest{Domains: []DomainInput{p("shop.example.com")}, PathPrefix: "/blog/"}, "path_prefix", "/blog"},
		"prefix injected": {UpdateDomainsRequest{Domains: []DomainInput{p("shop.example.com")}, PathPrefix: "/a`) || Path(`/b"}, "path_prefix", "/blog"},
	}
	for name, tc := range cases {
		rr := doAPIReq(srv, "PUT", "/api/apps/app_shop/domains", token, tc.req)
		var body struct {
			Fields map[string]string `json:"fields"`
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if rr.Code != http.StatusBadRequest || !strings.Contains(body.Fields[tc.field], tc.want) {
			t.Errorf("%s: %d fields=%v", name, rr.Code, body.Fields)
		}
	}
	if ds, _ := s.ListDomainsByApp(ctx, "app_shop"); len(ds) != 1 || ds[0].Domain != "shop.example.com" {
		t.Fatalf("a refused set must not change the domains: %+v", ds)
	}
}

// TestDomainsAPI_TraefikRejects_APP10: when Traefik does not load the new route, the API answers
// with the error and the domains stay as they were.
func TestDomainsAPI_TraefikRejects_APP10(t *testing.T) {
	srv, s, _, token := setupAppsTestServer(t)
	seedReleases(t, srv, s)
	failVerify.Store(true)
	defer failVerify.Store(false)
	rr := doAPIReq(srv, "PUT", "/api/apps/app_shop/domains", token, UpdateDomainsRequest{Domains: []DomainInput{
		{Domain: "shop.example.com", Role: "primary"}, {Domain: "ok.new.example.com", Role: "alias"},
	}})
	if rr.Code != http.StatusBadGateway || !strings.Contains(rr.Body.String(), "previous route and settings stay") {
		t.Fatalf("want 502 with the reason, got %d %s", rr.Code, rr.Body.String())
	}
	if ds, _ := s.ListDomainsByApp(context.Background(), "app_shop"); len(ds) != 1 {
		t.Fatalf("domains changed after a failed route: %+v", ds)
	}
}
