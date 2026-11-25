package traefik

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// HookDomainFileName is the dynamic Traefik file for the reserved GitHub webhook host (APP-2, TLS-5).
const HookDomainFileName = "01-hook-domain.yml"

// HookDomainMarker is the ownership comment on 01-hook-domain.yml (DEC-17).
const HookDomainMarker = "# managed by dermaga; hook-domain"

var dnsLabelRegex = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidateHookDomain validates a reserved hook domain hostname (SETUP-2, APP-2, SEC-16).
func ValidateHookDomain(domain string) error {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" || len(d) > 253 {
		return fmt.Errorf("hook domain must be 1..253 characters")
	}
	if strings.ContainsAny(d, " \t\r\n\x00%$`\"'\\/") {
		return fmt.Errorf("hook domain contains invalid characters")
	}
	if net.ParseIP(d) != nil {
		return fmt.Errorf("hook domain must be a DNS hostname, not an IP address")
	}
	if d == "localhost" || strings.HasSuffix(d, ".localhost") {
		return fmt.Errorf("hook domain cannot be localhost")
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return fmt.Errorf("hook domain must be a fully-qualified domain name (e.g. hooks.example.com)")
	}
	for _, lbl := range labels {
		if !dnsLabelRegex.MatchString(lbl) {
			return fmt.Errorf("invalid DNS label %q in hook domain", lbl)
		}
	}
	return nil
}

// HasHookDomainMarker checks whether content carries dermaga's hook-domain ownership marker.
func HasHookDomainMarker(content string) bool {
	firstLine, _, _ := strings.Cut(content, "\n")
	return strings.TrimSpace(firstLine) == HookDomainMarker
}

// RenderHookDomain renders 01-hook-domain.yml routing only POST /hooks/github-app
// with automatic ACME TLS (APP-2, TLS-5).
func RenderHookDomain(domain, backendURL string) ([]byte, error) {
	d := strings.ToLower(strings.TrimSpace(domain))
	if err := ValidateHookDomain(d); err != nil {
		return nil, err
	}
	servers := []Server{}
	if backendURL != "" {
		servers = []Server{{URL: backendURL}}
	}

	doc := Dynamic{
		HTTP: &HTTPConfig{
			Routers: map[string]Router{
				"hook-domain": {
					EntryPoints: []string{"websecure"},
					Rule:        fmt.Sprintf("Host(`%s`) && Path(`/hooks/github-app`) && Method(`POST`)", d),
					Service:     "hook-domain",
					TLS:         &RouterTLS{CertResolver: CertResolverName},
				},
			},
			Services: map[string]Service{
				"hook-domain": {
					LoadBalancer: LoadBalancer{
						Servers: servers,
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	buf.WriteString(HookDomainMarker + "\n")
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("marshaling hook domain config: %w", err)
	}
	return buf.Bytes(), nil
}
