package traefik

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testCertPEM(t *testing.T, host string, notBefore, notAfter time.Time) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: notBefore, NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// acmeJSON builds a file in the layout the M8 spike found: {"le": {"Account": …, "Certificates": [{"domain": {"main"}, "certificate", "key", "Store"}]}}.
func acmeJSON(t *testing.T, certs map[string][2]time.Time) []byte {
	t.Helper()
	var list []map[string]any
	for host, d := range certs {
		list = append(list, map[string]any{"domain": map[string]any{"main": host}, "certificate": testCertPEM(t, host, d[0], d[1]), "key": "c2VjcmV0", "Store": "default"})
	}
	b, err := json.Marshal(map[string]any{"le": map[string]any{"Account": map[string]any{"Email": ""}, "Certificates": list}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadCertificates_TLS3(t *testing.T) {
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	data := acmeJSON(t, map[string][2]time.Time{"Shop.Example.com": {now, now.Add(90 * 24 * time.Hour)}})
	certs, err := ReadCertificates(data)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := certs["shop.example.com"]
	if !ok || !c.NotAfter.Equal(now.Add(90*24*time.Hour)) {
		t.Fatalf("certificate not read: %+v", certs)
	}
	if _, ok := certs["other.example.com"]; ok {
		t.Fatal("negative control: a host without a certificate must be absent")
	}
	if got, err := ReadCertificates(nil); err != nil || len(got) != 0 {
		t.Fatalf("empty acme.json = no certificates, got %v %v", got, err)
	}
	if _, err := ReadCertificates([]byte("{not json")); err == nil {
		t.Fatal("a broken acme.json must be an error")
	}
}

// TestExpiringSoon_TLS4: warn below 1/6 of the lifetime left, for 90- and 45-day certificates.
func TestExpiringSoon_TLS4(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, days := range []int{90, 45} {
		life := time.Duration(days) * 24 * time.Hour
		c := Certificate{NotBefore: start, NotAfter: start.Add(life)}
		if c.ExpiringSoon(start.Add(life * 3 / 4)) {
			t.Errorf("%d days: 1/4 left (after the renewal point, renewal may still be running) is not a warning yet", days)
		}
		if !c.ExpiringSoon(start.Add(life*5/6 + time.Hour)) {
			t.Errorf("%d days: under 1/6 left must warn", days)
		}
	}
	now := start.Add(85 * 24 * time.Hour)
	acme := acmeJSON(t, map[string][2]time.Time{"old.example.com": {start, start.Add(90 * 24 * time.Hour)}, "new.example.com": {now, now.Add(90 * 24 * time.Hour)}})
	certs, _ := ReadCertificates(acme)
	got := ExpiringCertificates(certs, []string{"old.example.com", "new.example.com", "none.example.com"}, now)
	if len(got) != 1 || !strings.Contains(got[0], "old.example.com expires in 5 days") {
		t.Fatalf("expiring list: %v", got)
	}
}

// TestACMEErrors_TLS3 reads Traefik's real error lines captured in the M8 spike (the time field was
// re-added; the spike's observe script had dropped it): the newest error per domain wins.
func TestACMEErrors_TLS3(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "acme-errors.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	errs := ACMEErrors(data)
	if len(errs) != 3 {
		t.Fatalf("want bad, blocked, nodns; got %v", errs)
	}
	cases := map[string]string{
		"blocked.spike.test": "does not issue certificates",
		"nodns.spike.test":   "could not find a DNS record",
		"bad.spike.test":     "could not reach this server on port 80 in time",
	}
	for host, want := range cases {
		reason, fix := PlainReason(errs[host].Detail)
		if !strings.Contains(reason, want) || fix == "" {
			t.Errorf("%s: reason %q, want %q", host, reason, want)
		}
	}
	if b := errs["blocked.spike.test"]; b.Time.Format(time.RFC3339) != "2026-11-27T04:31:45Z" {
		t.Errorf("newest blocked error must win, got %v", b.Time)
	}
	reason, _ := PlainReason("acme: error: 400 :: urn:ietf:params:acme:error:connection :: dial tcp 1.2.3.4:80: connect: connection refused")
	if !strings.Contains(reason, "could not connect") {
		t.Errorf("connection refused: %q", reason)
	}
	if len(ACMEErrors([]byte(`{"message":"something else","domains":["x.example.com"]}`))) != 0 {
		t.Error("negative control: other log lines are ignored")
	}
}
