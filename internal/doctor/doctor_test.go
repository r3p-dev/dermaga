package doctor

import (
	"context"
	"strings"
	"testing"

	"dermaga/internal/host"
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
