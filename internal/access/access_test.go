package access

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func validTarget() Target {
	return Target{
		Address:        "203.0.113.10",
		Port:           4222,
		ServerHostname: "vps1",
		KeyFingerprint: "SHA256:V+KZKBpZYT+xiu/DXAsTjf2LTerWPN+DYUlih/Z1U9A",
	}
}

func TestValidateRejectsUnsafeValues(t *testing.T) {
	for name, mutate := range map[string]func(*Target){
		"quote in address":     func(x *Target) { x.Address = "1.2.3.4'; rm -rf ~; '" },
		"dollar in address":    func(x *Target) { x.Address = "$(id)" },
		"backtick in address":  func(x *Target) { x.Address = "`id`" },
		"newline in address":   func(x *Target) { x.Address = "host\nHost *" },
		"backslash in address": func(x *Target) { x.Address = `a\b` },
		"port zero":            func(x *Target) { x.Port = 0 },
		"port too high":        func(x *Target) { x.Port = 65536 },
		"hostname with space":  func(x *Target) { x.ServerHostname = "my host" },
		"hostname uppercase":   func(x *Target) { x.ServerHostname = "VPS" },
		"fingerprint md5":      func(x *Target) { x.KeyFingerprint = "MD5:aa:bb" },
		"fingerprint quote":    func(x *Target) { x.KeyFingerprint = "SHA256:'" },
	} {
		tg := validTarget()
		mutate(&tg)
		if _, err := LaptopCommand(tg); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	for _, addr := range []string{"203.0.113.10", "2001:db8::1", "vps.example.com"} {
		tg := validTarget()
		tg.Address = addr
		if _, err := LaptopCommand(tg); err != nil {
			t.Errorf("%s rejected: %v", addr, err)
		}
	}
}

// LAP-1: the command must paste the same way in bash, zsh and fish. Inside single quotes fish
// only interprets \\ and \', and no shell allows a single quote.
func TestLaptopCommandIsPasteSafe(t *testing.T) {
	cmd, err := LaptopCommand(validTarget())
	if err != nil {
		t.Fatal(err)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(cmd, "sh -c '"), "'")
	if strings.ContainsAny(inner, "'\n") || strings.Contains(inner, `\\`) {
		t.Errorf("unsafe characters in command: %s", cmd)
	}
}

type laptop struct {
	home string
	fp   string // fingerprint of ~/.ssh/id_ed25519.pub
}

func newLaptop(t *testing.T) laptop {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not installed")
	}
	home := t.TempDir()
	key := filepath.Join(home, ".ssh", "id_ed25519")
	if err := os.MkdirAll(filepath.Dir(key), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "laptop", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	out, err := exec.Command("ssh-keygen", "-lf", key+".pub").Output()
	if err != nil {
		t.Fatal(err)
	}
	return laptop{home: home, fp: strings.Fields(string(out))[1]}
}

func (l laptop) paste(t *testing.T, shell, cmd string) string {
	t.Helper()
	c := exec.Command(shell, "-c", cmd)
	// fish refuses to start without a writable config home; keep it inside the fake home too.
	c.Env = append(os.Environ(), "HOME="+l.home, "XDG_CONFIG_HOME="+filepath.Join(l.home, ".config"), "XDG_DATA_HOME="+filepath.Join(l.home, ".local/share"))
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: command failed: %v\n%s", shell, err, out)
	}
	return string(out)
}

func (l laptop) config(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(l.home, ".ssh", "config"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// sshResolve asks ssh how it would connect to alias with this config (ssh -G parses only).
func (l laptop) sshResolve(t *testing.T, alias string) map[string]string {
	t.Helper()
	out, err := exec.Command("ssh", "-G", "-F", filepath.Join(l.home, ".ssh", "config"), alias).Output()
	if err != nil {
		t.Fatalf("ssh -G %s: %v", alias, err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(line, " "); ok {
			if _, seen := got[k]; !seen { // first value wins, as in ssh
				got[k] = v
			}
		}
	}
	return got
}

func shells(t *testing.T) []string {
	var found []string
	for _, s := range []string{"sh", "bash", "zsh", "fish"} {
		if _, err := exec.LookPath(s); err == nil {
			found = append(found, s)
		} else {
			t.Logf("LAP-1: %s not installed, not tested here", s)
		}
	}
	return found
}

// LAP-1 / scenario f: pasted twice into a fresh laptop gives one block, a matching IdentityFile,
// and ssh resolves `dermaga` to the server with the panel forward.
func TestLaptopCommandFreshLaptopPastedTwice(t *testing.T) {
	for _, shell := range shells(t) {
		t.Run(shell, func(t *testing.T) {
			l := newLaptop(t)
			tg := validTarget()
			tg.KeyFingerprint = l.fp
			cmd, err := LaptopCommand(tg)
			if err != nil {
				t.Fatal(err)
			}
			first := l.paste(t, shell, cmd)
			second := l.paste(t, shell, cmd)
			if !strings.Contains(first, "Saved. From now on: ssh dermaga, then open http://localhost:7070") {
				t.Errorf("first paste output: %q", first)
			}
			if !strings.Contains(second, "Already saved. From now on: ssh dermaga") {
				t.Errorf("second paste output: %q", second)
			}
			cfg := l.config(t)
			if n := strings.Count(cfg, "Host dermaga\n"); n != 1 {
				t.Errorf("expected one block, got %d:\n%s", n, cfg)
			}
			if strings.Contains(cfg, "Host *") {
				t.Errorf("empty config must not get a Host * line:\n%s", cfg)
			}
			if _, err := os.Stat(filepath.Join(l.home, ".ssh", "config.dermaga-backup")); err == nil {
				t.Error("an empty config needs no backup")
			}
			if fi, _ := os.Stat(filepath.Join(l.home, ".ssh", "config")); fi.Mode().Perm() != 0600 {
				t.Errorf("config mode %v", fi.Mode().Perm())
			}
			got := l.sshResolve(t, "dermaga")
			for k, want := range map[string]string{
				"hostname":            "203.0.113.10",
				"port":                "4222",
				"user":                "server",
				"localforward":        "7070 [127.0.0.1]:7070",
				"identityfile":        filepath.Join(l.home, ".ssh", "id_ed25519"),
				"identitiesonly":      "yes",
				"serveraliveinterval": "30",
			} {
				if got[k] != want {
					t.Errorf("ssh -G %s = %q, want %q", k, got[k], want)
				}
			}
		})
	}
}

// LAP-1: an existing unmarked "Host dermaga" gets the alias dermaga-<hostname>; the old config is
// backed up once and keeps working; options at its top still apply to every host.
func TestLaptopCommandExistingConfig(t *testing.T) {
	for _, shell := range shells(t) {
		t.Run(shell, func(t *testing.T) {
			l := newLaptop(t)
			original := "User me\nCompression yes\n\nHost dermaga\n    HostName 198.51.100.7\n"
			path := filepath.Join(l.home, ".ssh", "config")
			if err := os.WriteFile(path, []byte(original), 0644); err != nil {
				t.Fatal(err)
			}
			tg := validTarget() // fingerprint matches no key on this laptop
			cmd, _ := LaptopCommand(tg)
			out := l.paste(t, shell, cmd)
			l.paste(t, shell, cmd)

			if !strings.Contains(out, "Saved. From now on: ssh dermaga-vps1,") || !strings.Contains(out, "No key in") {
				t.Errorf("output: %q", out)
			}
			backup, err := os.ReadFile(path + ".dermaga-backup")
			if err != nil || string(backup) != original {
				t.Errorf("backup = %q, %v", backup, err)
			}
			cfg := l.config(t)
			if strings.Count(cfg, "Host dermaga-vps1\n") != 1 || !strings.HasSuffix(cfg, original) {
				t.Errorf("config:\n%s", cfg)
			}
			if strings.Contains(cfg, "IdentityFile") {
				t.Error("IdentityFile written without a matching key")
			}
			mine := l.sshResolve(t, "dermaga-vps1")
			if mine["user"] != "server" || mine["port"] != "4222" || mine["compression"] != "yes" {
				t.Errorf("dermaga-vps1 resolved to %v", mine)
			}
			theirs := l.sshResolve(t, "dermaga")
			if theirs["hostname"] != "198.51.100.7" || theirs["user"] != "me" {
				t.Errorf("existing dermaga host changed: hostname=%q user=%q", theirs["hostname"], theirs["user"])
			}
		})
	}
}
