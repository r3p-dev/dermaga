package access_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

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

// TestLookupFor_DEC44: without an address the system resolver is used; with one, the query goes
// to that DNS server only (a UDP listener here receives it).
func TestLookupFor_DEC44(t *testing.T) {
	if access.LookupFor("") != nil {
		t.Fatal("empty address must mean the system resolver")
	}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	got := make(chan bool, 1)
	go func() {
		buf := make([]byte, 512)
		n, _, err := pc.ReadFrom(buf)
		got <- err == nil && strings.Contains(string(buf[:n]), "probe") // the queried name travels in the packet
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = access.LookupFor(pc.LocalAddr().String())(ctx, "ip4", "probe.dermaga.test")
	select {
	case ok := <-got:
		if !ok {
			t.Fatal("the query did not reach the configured DNS server")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no query reached the configured DNS server")
	}
	if (access.DNSCheckResult{AMatches: true, AAAAMismatch: true}).PointsHere() || !(access.DNSCheckResult{AMatches: true}).PointsHere() {
		t.Fatal("PointsHere: A must match and no AAAA may point elsewhere")
	}
}
