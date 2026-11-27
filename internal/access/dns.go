package access

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// DNSLookup abstracts net.Resolver.LookupIP for deterministic testing.
type DNSLookup func(ctx context.Context, network, host string) ([]net.IP, error)

// DNSCheckResult reports the A/AAAA comparison against the server's configured IP (SETUP-2).
type DNSCheckResult struct {
	Domain       string   `json:"domain"`
	ServerIP     string   `json:"server_ip"`
	ARecords     []string `json:"a_records"`
	AAAARecords  []string `json:"aaaa_records"`
	AMatches     bool     `json:"a_matches"`
	AAAAMismatch bool     `json:"aaaa_mismatch"`
	Warning      string   `json:"warning,omitempty"`
}

// CheckDomainDNS resolves A and AAAA records for domain and compares them against serverIP.
// DNS mismatches are reported as warnings and never block saving (SETUP-2).
func CheckDomainDNS(ctx context.Context, domain, serverIP string, lookup DNSLookup) DNSCheckResult {
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIP
	}

	res := DNSCheckResult{
		Domain:   strings.ToLower(strings.TrimSpace(domain)),
		ServerIP: strings.TrimSpace(serverIP),
	}

	dCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	if ips, err := lookup(dCtx, "ip4", res.Domain); err == nil {
		for _, ip := range ips {
			s := ip.String()
			res.ARecords = append(res.ARecords, s)
			if s == res.ServerIP {
				res.AMatches = true
			}
		}
	}

	if ips, err := lookup(dCtx, "ip6", res.Domain); err == nil {
		for _, ip := range ips {
			s := ip.String()
			res.AAAARecords = append(res.AAAARecords, s)
			if s != res.ServerIP {
				res.AAAAMismatch = true
			}
		}
	}

	switch {
	case res.AAAAMismatch:
		res.Warning = fmt.Sprintf(
			"AAAA record (%s) points elsewhere than this server (%s); remove or update it so ACME challenges do not fail.",
			strings.Join(res.AAAARecords, ", "), res.ServerIP,
		)
	case !res.AMatches && res.ServerIP != "":
		if len(res.ARecords) == 0 {
			res.Warning = fmt.Sprintf("No A record found yet. Create an A record for %s pointing to %s.", res.Domain, res.ServerIP)
		} else {
			res.Warning = fmt.Sprintf("A record (%s) does not match this server (%s). Update it or wait for DNS propagation.", strings.Join(res.ARecords, ", "), res.ServerIP)
		}
	}

	return res
}

// PointsHere reports whether the domain can get a certificate on this server: its A record is the
// server's address and no AAAA record points elsewhere (TLS-2, DEC-38).
func (r DNSCheckResult) PointsHere() bool {
	return r.AMatches && !r.AAAAMismatch
}

// ResolverEnv names the test-only DNS server override (DEC-44). Only the VM test drop-in of the
// user unit sets it; `dermaga doctor` warns while it is set.
const ResolverEnv = "DERMAGA_DNS_RESOLVER"

// LookupFor returns a DNSLookup that asks only the DNS server at addr ("host:port"), or nil (the
// system resolver) when addr is empty.
func LookupFor(addr string) DNSLookup {
	if addr == "" {
		return nil
	}
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}}
	return r.LookupIP
}
