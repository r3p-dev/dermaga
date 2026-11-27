package doctor

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dermaga/internal/auth"
	"dermaga/internal/host"
	"dermaga/internal/store"
)

// healthyHost answers every command the way a healthy Debian 13 server does.
func healthyHost(overrides map[string]string) *host.FakeHost {
	fh := host.NewFakeHost()
	_ = fh.WriteFileAtomic("/var/lib/systemd/linger/server", nil, 0644)
	_ = fh.WriteFileAtomic("/etc/subuid", []byte("debian:100000:65536\nserver:165536:65536\n"), 0644)
	_ = fh.WriteFileAtomic("/etc/subgid", []byte("server:165536:65536\n"), 0644)
	answers := map[string]string{
		"systemctl --user is-active traefik-http.socket traefik-https.socket":   "active\nactive\n",
		"podman version --format {{.Client.Version}}":                           "5.4.2\n",
		"systemctl --user is-active traefik.service":                            "active\n",
		"systemctl show -P ActiveState network-online.target":                   "active\n",
		"systemctl show -P ActiveEnterTimestampMonotonic network-online.target": "3045123\n",
		"timedatectl show -P NTPSynchronized":                                   "yes\n",
		"df --output=pcent /home/server":                                        "Use%\n 12%\n",
	}
	for k, v := range overrides {
		answers[k] = v
	}
	fh.CommandMock = func(name string, args ...string) ([]byte, error) {
		return []byte(answers[name+" "+strings.Join(args, " ")]), nil
	}
	return fh
}

func statusByName(results []Result) map[string]Status {
	m := map[string]Status{}
	for _, r := range results {
		m[r.Name] = r.Status
	}
	return m
}

func TestDoctorHealthyHost(t *testing.T) {
	results := Checker{Host: healthyHost(nil), User: "server", Home: "/home/server"}.Run(context.Background())
	if Failed(results) {
		t.Fatalf("healthy host reported failures:\n%s", Print(results))
	}
	for _, r := range results {
		if r.Status != OK {
			t.Errorf("%s: %s %s", r.Name, r.Status, r.Detail)
		}
	}
}

// DOC-1: each failure is detected and comes with a fix hint.
func TestDoctorDetectsEachProblem(t *testing.T) {
	cases := map[string]struct {
		overrides map[string]string
		prepare   func(*host.FakeHost)
		check     string
		want      Status
	}{
		"no linger":        {prepare: func(f *host.FakeHost) { _ = f.Remove("/var/lib/systemd/linger/server") }, check: "lingering", want: Fail},
		"small subuid":     {prepare: func(f *host.FakeHost) { _ = f.WriteFileAtomic("/etc/subuid", []byte("server:100000:1000\n"), 0644) }, check: "subuids", want: Fail},
		"missing subgid":   {prepare: func(f *host.FakeHost) { _ = f.Remove("/etc/subgid") }, check: "subgids", want: Fail},
		"socket down":      {overrides: map[string]string{"systemctl --user is-active traefik-http.socket traefik-https.socket": "active\nfailed\n"}, check: "sockets", want: Fail},
		"old podman":       {overrides: map[string]string{"podman version --format {{.Client.Version}}": "4.9.3\n"}, check: "podman", want: Fail},
		"traefik down":     {overrides: map[string]string{"systemctl --user is-active traefik.service": "activating\n"}, check: "traefik", want: Fail},
		"slow network":     {overrides: map[string]string{"systemctl show -P ActiveEnterTimestampMonotonic network-online.target": "91000000\n"}, check: "network-online", want: Warn},
		"network inactive": {overrides: map[string]string{"systemctl show -P ActiveState network-online.target": "inactive\n"}, check: "network-online", want: Warn},
		"clock":            {overrides: map[string]string{"timedatectl show -P NTPSynchronized": "no\n"}, check: "clock", want: Fail},
		"disk 90":          {overrides: map[string]string{"df --output=pcent /home/server": "Use%\n 90%\n"}, check: "disk", want: Warn},
		"disk 96":          {overrides: map[string]string{"df --output=pcent /home/server": "Use%\n 96%\n"}, check: "disk", want: Fail},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fh := healthyHost(tc.overrides)
			if tc.prepare != nil {
				tc.prepare(fh)
			}
			results := Checker{Host: fh, User: "server", Home: "/home/server"}.Run(context.Background())
			if got := statusByName(results)[tc.check]; got != tc.want {
				t.Errorf("%s = %s, want %s\n%s", tc.check, got, tc.want, Print(results))
			}
			for _, r := range results {
				if r.Status != OK && r.Fix == "" {
					t.Errorf("%s has no fix hint", r.Name)
				}
			}
		})
	}
}

func TestOlderThan(t *testing.T) {
	for _, c := range []struct {
		v    string
		want bool
	}{{"5.4.2", false}, {"5.4.1", true}, {"5.10.0", false}, {"6.0.0-rc1", false}, {"4.9.3", true}, {"5.4", true}} {
		if got := olderThan(c.v, "5.4.2"); got != c.want {
			t.Errorf("olderThan(%q) = %v", c.v, got)
		}
	}
}

func TestDoctorTOTPWarning_DOC2(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "doc.db"))
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	defer st.Close()

	c := Checker{Host: healthyHost(nil), Store: st, User: "server", Home: "/home/server"}

	if got := statusByName(c.Run(ctx))["totp"]; got != Warn {
		t.Fatalf("uninitialized totp status = %s, want warn", got)
	}

	if err := auth.SetAdminPassword(ctx, st, "panel", "correct-horse-battery"); err != nil {
		t.Fatalf("SetAdminPassword failed: %v", err)
	}
	if got := statusByName(c.Run(ctx))["totp"]; got != Warn {
		t.Fatalf("disabled totp status = %s, want warn", got)
	}

	if err := st.SetSetting(ctx, auth.SettingTOTPEnabled, "true"); err != nil {
		t.Fatalf("SetSetting failed: %v", err)
	}
	if got := statusByName(c.Run(ctx))["totp"]; got != OK {
		t.Fatalf("enabled totp status = %s, want ok", got)
	}
}

// DEC-44: the test-only DNS resolver is reported while dermaga.service sets it.
func TestDoctorDNSResolverOverride_DEC44(t *testing.T) {
	results := Checker{Host: healthyHost(map[string]string{
		"systemctl --user show dermaga.service -p Environment --value": "HOME=/home/server DERMAGA_DNS_RESOLVER=127.0.0.1:8053\n",
	}), User: "server", Home: "/home/server"}.Run(context.Background())
	if got := statusByName(results)["dns resolver"]; got != Warn {
		t.Fatalf("dns resolver = %s, want warn", got)
	}
	if got := statusByName(Checker{Host: healthyHost(nil), User: "server", Home: "/home/server"}.Run(context.Background()))["dns resolver"]; got != OK {
		t.Fatalf("negative control: without the variable it is ok, got %s", got)
	}
}

// TLS-4: a certificate with less than 1/6 of its lifetime left is a warning; a fresh one is not.
func TestDoctorCertificates_TLS4(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "doc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.CreateApp(ctx, &store.App{ID: "app_1", Name: "web", RepoOwner: "o", RepoName: "r", Port: 80, HealthType: "none"})
	_ = st.CreateDomain(ctx, &store.Domain{ID: "dom_1", AppID: "app_1", Domain: "web.example.com", IsPrimary: true})
	now := time.Now()
	for _, tc := range []struct {
		notBefore time.Time
		want      Status
	}{
		{now.Add(-85 * 24 * time.Hour), Warn}, // 5 of 90 days left
		{now.Add(-10 * 24 * time.Hour), OK},
	} {
		fh := healthyHost(nil)
		_ = fh.WriteFileAtomic("/home/server/.local/share/dermaga/traefik/acme.json", acmeWithCert(t, "web.example.com", tc.notBefore, tc.notBefore.Add(90*24*time.Hour)), 0600)
		if got := statusByName(Checker{Host: fh, Store: st, User: "server", Home: "/home/server"}.Run(ctx))["certificates"]; got != tc.want {
			t.Errorf("certificate issued %s: status %s, want %s", tc.notBefore.Format(time.DateOnly), got, tc.want)
		}
	}
}

func acmeWithCert(t *testing.T, host string, notBefore, notAfter time.Time) []byte {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{host}, NotBefore: notBefore, NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return []byte(fmt.Sprintf(`{"le":{"Certificates":[{"domain":{"main":%q},"certificate":%q}]}}`, host, cert))
}
