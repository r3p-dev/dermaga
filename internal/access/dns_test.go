package access_test

import (
	"context"
	"net"
	"strings"
	"testing"

	"dermaga/internal/access"
)

func TestCheckDomainDNS_SETUP2(t *testing.T) {
	ctx := context.Background()

	// Matching A record, no AAAA -> no warning
	res := access.CheckDomainDNS(ctx, "hooks.example.com", "203.0.113.10", func(_ context.Context, network, _ string) ([]net.IP, error) {
		if network == "ip4" {
			return []net.IP{net.ParseIP("203.0.113.10")}, nil
		}
		return nil, nil
	})
	if !res.AMatches || res.AAAAMismatch || res.Warning != "" {
		t.Fatalf("expected clean match, got %+v", res)
	}

	// Mismatched AAAA flagged (breaks ACME per SETUP-2)
	res = access.CheckDomainDNS(ctx, "hooks.example.com", "203.0.113.10", func(_ context.Context, network, _ string) ([]net.IP, error) {
		if network == "ip4" {
			return []net.IP{net.ParseIP("203.0.113.10")}, nil
		}
		return []net.IP{net.ParseIP("2001:db8::99")}, nil
	})
	if !res.AAAAMismatch || !strings.Contains(res.Warning, "AAAA record") {
		t.Fatalf("expected AAAA warning, got %+v", res)
	}

	// Missing A record -> warning with exact record to create
	res = access.CheckDomainDNS(ctx, "hooks.example.com", "203.0.113.10", func(_ context.Context, _, _ string) ([]net.IP, error) {
		return nil, nil
	})
	if res.AMatches || !strings.Contains(res.Warning, "203.0.113.10") {
		t.Fatalf("expected missing A warning, got %+v", res)
	}
}
