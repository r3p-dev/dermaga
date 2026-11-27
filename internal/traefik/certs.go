package traefik

import (
	"bufio"
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
	"time"
)

// Certificate is what dermaga shows about a certificate Traefik stored in acme.json (TLS-3).
type Certificate struct {
	NotBefore time.Time
	NotAfter  time.Time
}

// ExpiringSoon reports a certificate with less than 1/6 of its lifetime left. Traefik renews at
// 1/3, so by then a renewal has failed; relative, so it fits shorter lifetimes too (DEC-41, TLS-4).
func (c Certificate) ExpiringSoon(now time.Time) bool {
	return c.NotAfter.Sub(now) < c.NotAfter.Sub(c.NotBefore)/6
}

// acmeFile is the part of Traefik's acme.json dermaga reads: resolver → certificates. Private keys
// are in the same file and are never decoded.
type acmeFile map[string]struct {
	Certificates []struct {
		Domain struct {
			Main string   `json:"main"`
			SANs []string `json:"sans"`
		} `json:"domain"`
		Certificate string `json:"certificate"` // base64 of the PEM chain
	} `json:"Certificates"`
}

// ReadCertificates returns the certificate of every host in acme.json (all resolvers). An empty
// or missing file means no certificates yet.
func ReadCertificates(data []byte) (map[string]Certificate, error) {
	out := map[string]Certificate{}
	if len(bytes.TrimSpace(data)) == 0 {
		return out, nil
	}
	var f acmeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("reading acme.json: %w", err)
	}
	for _, resolver := range f {
		for _, c := range resolver.Certificates {
			raw, err := base64.StdEncoding.DecodeString(c.Certificate)
			if err != nil {
				continue
			}
			block, _ := pem.Decode(raw)
			if block == nil {
				continue
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				continue
			}
			for _, host := range append([]string{c.Domain.Main}, c.Domain.SANs...) {
				out[strings.ToLower(host)] = Certificate{NotBefore: cert.NotBefore, NotAfter: cert.NotAfter}
			}
		}
	}
	return out, nil
}

// ExpiringCertificates lists the hosts whose certificate is close to expiry (TLS-4), one plain
// sentence each, for the dashboard and doctor.
func ExpiringCertificates(certs map[string]Certificate, hosts []string, now time.Time) []string {
	var out []string
	for _, h := range hosts {
		if c, ok := certs[strings.ToLower(h)]; ok && c.ExpiringSoon(now) {
			days := int(c.NotAfter.Sub(now).Hours() / 24)
			out = append(out, fmt.Sprintf("The certificate for %s expires in %d days (%s) and has not been renewed.", h, days, c.NotAfter.UTC().Format("2 Jan 2006")))
		}
	}
	return out
}

// ACMEError is Traefik's newest failed certificate order for one domain.
type ACMEError struct {
	Time   time.Time
	Detail string
}

// ACMEErrors reads Traefik's JSON log lines (journal, one object per line) and returns the newest
// "Unable to obtain ACME certificate for domains" error per domain. The line format was captured in
// the M8 spike (tmp/evidence/M8/logs/spike-acme-observe-2.log).
func ACMEErrors(log []byte) map[string]ACMEError {
	out := map[string]ACMEError{}
	sc := bufio.NewScanner(bytes.NewReader(log))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var l struct {
			Message string    `json:"message"`
			Error   string    `json:"error"`
			Domains []string  `json:"domains"`
			Time    time.Time `json:"time"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Message != "Unable to obtain ACME certificate for domains" {
			continue
		}
		for _, d := range l.Domains {
			d = strings.ToLower(d)
			if prev, ok := out[d]; !ok || !l.Time.Before(prev.Time) {
				out[d] = ACMEError{Time: l.Time, Detail: l.Error}
			}
		}
	}
	return out
}

// PlainReason turns an ACME error into what happened and what to do, in plain words (TLS-3). The
// raw detail is shown next to it.
func PlainReason(detail string) (reason, fix string) {
	d := strings.ToLower(detail)
	switch {
	case strings.Contains(d, "error:ratelimited"):
		return "Let's Encrypt refused because too many certificates or failed attempts were requested for this domain recently.",
			"Wait for the time Let's Encrypt names in the details, then press Check again."
	case strings.Contains(d, "error:rejectedidentifier"):
		return "The certificate authority does not issue certificates for this domain.",
			"Use another domain, or check the spelling."
	case strings.Contains(d, "error:caa"):
		return "A CAA record on this domain does not allow Let's Encrypt to issue certificates.",
			"Add a CAA record that allows letsencrypt.org, or remove the CAA records, then press Check again."
	case strings.Contains(d, "could not resolve"), strings.Contains(d, "nxdomain"), strings.Contains(d, "error:dns"):
		return "The certificate authority could not find a DNS record for this domain.",
			"Check the A record at your DNS provider, wait until it has spread, then press Check again."
	case strings.Contains(d, "timeout"), strings.Contains(d, "deadline exceeded"):
		return "The certificate authority could not reach this server on port 80 in time.",
			"Check that the A record points to this server and that no firewall or proxy blocks port 80, then press Check again."
	case strings.Contains(d, "error:connection"):
		return "The certificate authority could not connect to this server on port 80.",
			"Check that the A record points to this server and that port 80 is open, then press Check again."
	case strings.Contains(d, "error:unauthorized"):
		return "The certificate authority reached a server that did not answer its check correctly.",
			"Check that the A and AAAA records point only to this server and that no proxy answers in its place, then press Check again."
	default:
		return "The certificate authority refused the certificate.",
			"Read the details below, fix the cause, then press Check again."
	}
}

// LastConfigError returns the newest error Traefik logged that is not about a certificate order
// (a dynamic file it could not load, a missing middleware), as "message: error" (APP-10).
func LastConfigError(log []byte) string {
	last := ""
	sc := bufio.NewScanner(bytes.NewReader(log))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var l struct {
			Level   string `json:"level"`
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Level != "error" || l.Message == "Unable to obtain ACME certificate for domains" {
			continue
		}
		last = l.Message
		if l.Error != "" {
			last += ": " + l.Error
		}
	}
	return last
}
