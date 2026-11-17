// Package install tests install.sh against a fake host: every system command is a stub in PATH
// that logs its arguments, and every system path lives under a temporary root.
package install

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	testKey     = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl laptop"
	testKeyFP   = "SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU"
	fakeBinary  = "fake dermaga binary\n"
	testVersion = "v0.0.0-test"
)

// stubs maps a command to the bash body of its stub. $ROOT is the fake root, $STUB the stub
// directory (state files live there). Commands not listed only log and succeed.
var stubs = map[string]string{
	// sshd -T reports the Port lines of dermaga's drop-in (22 without one); -t fails on demand.
	"sshd": `
case "$1" in
  -t) [[ ! -e $STUB/sshd_t_fails ]] ;;
  -T)
    f=$ROOT/etc/ssh/sshd_config.d/00-dermaga.conf
    if [[ -f $f ]]; then awk '$1 == "Port" {print "port " $2}' "$f"; else echo "port 22"; fi
    if [[ $* == *lport=* ]]; then echo "passwordauthentication no"; echo "permitrootlogin no"; fi ;;
esac`,
	// ss: sshd listens on its configured ports unless told not to; port 80 busy on demand.
	"ss": `
if [[ $* == *":80 or"* ]]; then [[ -e $STUB/port80_busy ]] && echo 'LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("nginx",pid=1,fd=6))'; exit 0; fi
p=$(sed -n 's/.*sport = :\([0-9]*\).*/\1/p' <<<"$*")
[[ -e $STUB/sshd_never_listens_$p ]] && exit 0
f=$ROOT/etc/ssh/sshd_config.d/00-dermaga.conf
if [[ -f $f ]]; then ports=$(awk '$1 == "Port" {print $2}' "$f"); else ports=22; fi
grep -qx "$p" <<<"$ports" && echo "LISTEN 0 128 0.0.0.0:$p 0.0.0.0:* users:((\"sshd\",pid=7,fd=3))"
exit 0`,
	"journalctl": `cat "$STUB/journal.txt" 2>/dev/null; exit 0`,
	"curl": `
out=""; url=""
while (($#)); do case $1 in -o) out=$2; shift 2 ;; -*) shift ;; *) url=$1; shift ;; esac; done
src=$STUB/downloads/${url##*/}
[[ -f $src ]] || exit 22
if [[ -n $out ]]; then cp "$src" "$out"; else cat "$src"; fi`,
	"systemctl": `
state=$STUB/active_units
case "$*" in
  *"is-active --quiet ssh.socket"*) exit 1 ;;
  *is-active*) u=${@: -1}; grep -qxF "$u" "$state" 2>/dev/null ;;
  "reload ssh.service") n=$(ls "$STUB"/reload-*.conf 2>/dev/null | wc -l); cp "$ROOT/etc/ssh/sshd_config.d/00-dermaga.conf" "$STUB/reload-$((n + 1)).conf" 2>/dev/null || touch "$STUB/reload-$((n + 1)).conf" ;;
  start*|restart*) echo "${@: -1}" >>"$state" ;;
  "add-wants multi-user.target network-online.target") mkdir -p "$ROOT/etc/systemd/system/multi-user.target.wants"; touch "$ROOT/etc/systemd/system/multi-user.target.wants/network-online.target" ;;
esac
exit 0`,
	"id": `
[[ $1 == -u ]] && { echo 1001; exit 0; }
[[ -e $STUB/user_exists ]]`,
	"useradd":     `touch "$STUB/user_exists"`,
	"loginctl":    `mkdir -p "$ROOT/var/lib/systemd/linger" && touch "$ROOT/var/lib/systemd/linger/server"`,
	"passwd":      `if [[ $1 == -S ]]; then [[ -e $STUB/passwd_set ]] && echo "server P" || echo "server L"; else read -r a; read -r b; [[ $a == "$b" ]] && touch "$STUB/passwd_set"; fi`,
	"dpkg":        `[[ $1 == -s && -e $STUB/pkgs_installed && $2 != chrony ]]`,
	"apt-get":     `[[ $* == *install* ]] && touch "$STUB/pkgs_installed"; exit 0`,
	"ssh-keygen":  `[[ $1 == -A ]] && exit 0; exec ` + "REAL_SSH_KEYGEN" + ` "$@"`,
	"timedatectl": `[[ $1 == show ]] && echo yes; exit 0`,
	"ip": `
case "$*" in
  "-o route show default") echo "default via 192.168.122.1 dev enp1s0 proto dhcp src 192.168.122.217 metric 100" ;;
  "-o -4 addr show dev enp1s0 scope global") echo "2: enp1s0    inet ${TEST_IPV4:-192.168.122.217}/24 brd 192.168.122.255 scope global dynamic enp1s0" ;;
esac`,
	"runuser": `
case "$*" in
  *"dermaga panel"*) echo "LAPTOP COMMAND FROM dermaga panel"; echo 'Lost this? Run ` + "`dermaga panel`" + ` on the server.' ;;
  *"dermaga doctor"*) echo "[OK  ] traefik        traefik.service is active" ;;
esac
exit 0`,
	"sleep": `exit 0`,
}

// stubbed are the commands that only log; real coreutils, awk, sed, grep, jq, ssh stay real.
var stubbed = []string{"chown", "usermod", "modprobe", "nft", "fail2ban-client", "visudo"}

type fakeServer struct {
	t     *testing.T
	root  string
	stub  string
	tty   string
	env   []string
	files map[string]string // relative to root, written before the run
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	realKeygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not installed")
	}
	dir := t.TempDir()
	s := &fakeServer{t: t, root: filepath.Join(dir, "root"), stub: filepath.Join(dir, "stub"), tty: filepath.Join(dir, "tty")}
	bin := filepath.Join(s.stub, "bin")
	for _, d := range []string{bin, filepath.Join(s.stub, "downloads"), filepath.Join(s.root, "etc/ssh/sshd_config.d"),
		filepath.Join(s.root, "run/systemd/system"), filepath.Join(s.root, "home/server"), filepath.Join(s.root, "home/debian/.ssh"),
		filepath.Join(s.root, "root/.ssh"), filepath.Join(s.root, "run/user/1001"), filepath.Join(s.root, "var/log")} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	// The user manager's bus must be a socket for the installer to continue.
	l, err := net.Listen("unix", filepath.Join(s.root, "run/user/1001/bus"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })

	write := func(path, body string, mode os.FileMode) {
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range stubs {
		body = strings.ReplaceAll(body, "REAL_SSH_KEYGEN", realKeygen)
		write(filepath.Join(bin, name), "#!/usr/bin/env bash\necho \""+name+" $*\" >>\"$STUB/calls.log\"\n"+body+"\n", 0755)
	}
	for _, name := range stubbed {
		write(filepath.Join(bin, name), "#!/usr/bin/env bash\necho \""+name+" $*\" >>\"$STUB/calls.log\"\n", 0755)
	}

	sum := sha256.Sum256([]byte(fakeBinary))
	write(filepath.Join(s.stub, "downloads/dermaga-linux-amd64"), fakeBinary, 0644)
	write(filepath.Join(s.stub, "downloads/checksums.txt"), hex.EncodeToString(sum[:])+"  dermaga-linux-amd64\n0000000000000000000000000000000000000000000000000000000000000000  dermaga-linux-arm64\n", 0644)
	meta, err := os.ReadFile("testdata/github_meta.json")
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(s.stub, "downloads/meta"), string(meta), 0644)

	s.files = map[string]string{
		"etc/os-release":                   "ID=debian\nVERSION_ID=\"13\"\n",
		"home/debian/.ssh/authorized_keys": testKey + "\n",
	}
	s.env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"DERMAGA_TEST_ROOT="+s.root,
		"DERMAGA_TEST_TTY="+s.tty,
		"STUB="+s.stub,
		"ROOT="+s.root,
		"SUDO_USER=debian",
		"SSH_CONNECTION=192.168.122.1 51000 192.168.122.217 22",
	)
	return s
}

// journal makes sshd's log contain these lines.
func (s *fakeServer) journal(lines ...string) {
	s.writeStub("journal.txt", strings.Join(lines, "\n")+"\n")
}

func (s *fakeServer) writeStub(name, body string) {
	if err := os.WriteFile(filepath.Join(s.stub, name), []byte(body), 0644); err != nil {
		s.t.Fatal(err)
	}
}

// newLoginOn4222 is what sshd logs (LogLevel VERBOSE) when server logs in on the new port; the
// format is copied from dermaga-vm (testdata/journal_vm_verbose.txt).
var newLoginOn4222 = []string{
	`Connection from 192.168.122.1 port 37160 on 192.168.122.217 port 4222 rdomain ""`,
	`Accepted publickey for server from 192.168.122.1 port 37160 ssh2: ED25519 ` + testKeyFP,
}

// run executes a stamped copy of install.sh, the way the release workflow stamps it.
func (s *fakeServer) run(ttyInput string, args ...string) (string, error) {
	s.t.Helper()
	for rel, body := range s.files {
		p := filepath.Join(s.root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			s.t.Fatal(err)
		}
	}
	s.files = map[string]string{}
	if err := os.WriteFile(s.tty, []byte(ttyInput), 0600); err != nil {
		s.t.Fatal(err)
	}
	src, err := os.ReadFile("install.sh")
	if err != nil {
		s.t.Fatal(err)
	}
	script := filepath.Join(s.stub, "install.sh")
	if err := os.WriteFile(script, []byte(strings.Replace(string(src), "__DERMAGA_VERSION__", testVersion, 1)), 0755); err != nil {
		s.t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(s.stub, "calls.log"))
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = s.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (s *fakeServer) read(rel string) string {
	b, err := os.ReadFile(filepath.Join(s.root, rel))
	if err != nil {
		return ""
	}
	return string(b)
}

func (s *fakeServer) calls() string {
	b, _ := os.ReadFile(filepath.Join(s.stub, "calls.log"))
	return string(b)
}

func (s *fakeServer) reloads() []string {
	var out []string
	for i := 1; ; i++ {
		b, err := os.ReadFile(filepath.Join(s.stub, fmt.Sprintf("reload-%d.conf", i)))
		if err != nil {
			return out
		}
		out = append(out, string(b))
	}
}

func (s *fakeServer) config(lines ...string) string {
	p := filepath.Join(s.stub, "install.conf")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		s.t.Fatal(err)
	}
	return p
}

func (s *fakeServer) automated() []string {
	return []string{"--config", s.config(
		`SSH_PUBLIC_KEY="`+testKey+`"`, "SSH_PORT=4222", "PUBLIC_ADDRESS=192.168.122.217", "SUDO_PASSWORDLESS=yes"), "--yes"}
}

const dropin = "etc/ssh/sshd_config.d/00-dermaga.conf"

// INST-4, LOCK-1…LOCK-4, INST-9, INST-10, INST-12: the automated path from a fresh server.
func TestInstallFreshServer(t *testing.T) {
	s := newFakeServer(t)
	s.journal(newLoginOn4222...)
	out, err := s.run("", s.automated()...)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}

	reloads := s.reloads()
	if len(reloads) != 2 {
		t.Fatalf("expected 2 sshd reloads (open, then close), got %d\n%s", len(reloads), out)
	}
	if !strings.Contains(reloads[0], "Port 22\nPort 4222\n") || !strings.Contains(reloads[0], "Match LocalPort 4222\n    AllowUsers server") {
		t.Errorf("LOCK-1: first reload must keep port 22 and add 4222 for server only:\n%s", reloads[0])
	}
	final := s.read(dropin)
	if reloads[1] != final || strings.Contains(final, "Port 22") || !strings.Contains(final, "Port 4222\nPermitRootLogin no\nPasswordAuthentication no") {
		t.Errorf("LOCK-4: final sshd config:\n%s", final)
	}
	calls := s.calls()
	if strings.Index(calls, "sshd -t") > strings.Index(calls, "systemctl reload ssh.service") {
		t.Error("sshd -t must run before the reload")
	}
	if nft := s.read("etc/nftables.conf"); !strings.Contains(nft, "tcp dport { 4222 } accept") || strings.Contains(nft, "dport { 22") {
		t.Errorf("HOST-3: final firewall:\n%s", nft)
	}
	if f2b := s.read("etc/fail2ban/jail.d/90-dermaga.local"); !strings.Contains(f2b, "port = 4222\n") {
		t.Errorf("HOST-4: fail2ban jail:\n%s", f2b)
	}
	if got := s.read("home/server/.local/bin/dermaga"); got != fakeBinary {
		t.Errorf("INST-9: binary = %q", got)
	}
	if kh := s.read("home/server/.config/dermaga/github_known_hosts"); strings.Count(kh, "github.com ") != 3 {
		t.Errorf("INST-10: known hosts:\n%s", kh)
	}
	if !strings.Contains(out, "github.com SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU") {
		t.Error("INST-10: fingerprints must be shown")
	}
	if !strings.Contains(s.read("home/server/.ssh/authorized_keys"), testKey) {
		t.Error("INST-5: key not authorised for server")
	}
	if !strings.Contains(calls, "install-settings --ssh-address 192.168.122.217 --ssh-port 4222 --login-key-fingerprint "+testKeyFP) {
		t.Errorf("INST-6: settings not stored:\n%s", calls)
	}
	for _, want := range []string{"ssh -p 4222 server@192.168.122.217", "Keep this window open", "LAPTOP COMMAND FROM dermaga panel", "Lost this? Run `dermaga panel` on the server.", "[OK  ] traefik"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if !strings.Contains(s.read("etc/sudoers.d/90-dermaga-server"), "NOPASSWD") {
		t.Error("HOST-7: SUDO_PASSWORDLESS=yes must write the sudoers rule")
	}
	if !strings.Contains(s.read("etc/sysctl.d/90-dermaga.conf"), "net.ipv4.ip_unprivileged_port_start = 80") {
		t.Error("HOST-6: unprivileged ports for the Traefik sockets")
	}

	// INST-2 / scenario g: running it again changes nothing and reloads nothing.
	out, err = s.run("", s.automated()...)
	if err != nil {
		t.Fatalf("re-run failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "nothing changed: this server was already set up") {
		t.Errorf("re-run output:\n%s", out)
	}
	for _, c := range []string{"systemctl reload", "systemctl restart", "--user restart", "apt-get", "useradd", "daemon-reload", "add-wants", "enable-linger", "passwd server"} {
		if strings.Contains(s.calls(), c) {
			t.Errorf("re-run called %q:\n%s", c, s.calls())
		}
	}
	if len(s.reloads()) != 2 {
		t.Error("re-run reloaded sshd")
	}
}

// LOCK-5 / scenario c and d: without a login on the new port nothing is closed.
func TestInstallTimeoutKeepsOldAccess(t *testing.T) {
	s := newFakeServer(t)
	s.journal(`Accepted publickey for debian from 192.168.122.1 port 51000 ssh2: ED25519 ` + testKeyFP)
	out, err := s.run("", s.automated()...)
	if err == nil {
		t.Fatalf("install must fail without a new login:\n%s", out)
	}
	for _, want := range []string{"Nothing was closed", "Your current way of logging in still works", "Run the same command again"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if d := s.read(dropin); !strings.Contains(d, "Port 22\n") {
		t.Errorf("port 22 must stay open:\n%s", d)
	}
	if nft := s.read("etc/nftables.conf"); !strings.Contains(nft, "tcp dport { 22, 4222 } accept") {
		t.Errorf("firewall must keep port 22:\n%s", nft)
	}
	if len(s.reloads()) != 1 {
		t.Errorf("only the opening reload may happen, got %d", len(s.reloads()))
	}

	// Re-running after the login works resumes and closes the old way in.
	s.journal(newLoginOn4222...)
	if out, err := s.run("", s.automated()...); err != nil {
		t.Fatalf("resume failed: %v\n%s", err, out)
	}
	if strings.Contains(s.read(dropin), "Port 22") {
		t.Error("resumed run must close port 22")
	}
}

// INST-3, INST-5, INST-6, LOCK-3: the interactive path with Enter only, the current login's key
// marked, the private address explained, and "yes" as the manual fallback.
func TestInstallInteractiveEnterOnly(t *testing.T) {
	s := newFakeServer(t)
	s.files["root/.ssh/authorized_keys"] = `no-port-forwarding,command="echo 'Please login as the user \"debian\" rather than the user \"root\".';echo;sleep 10;exit 142" ` + testKey + "\n" +
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIK0wmN/Cr3JXqmLW7u+g9pTh+wyqDHpSQEIQczXkVx9q other@laptop\n"
	s.journal(`Accepted publickey for debian from 192.168.122.1 port 51000 ssh2: ED25519 ` + testKeyFP)
	// key (default = the current one), port, address, sudo password: no, plan, then "yes" at the wait
	out, err := s.run("\n\n\nn\n\nyes\n")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if !regexp.MustCompile(`1\. ED25519 ` + regexp.QuoteMeta(testKeyFP) + ` laptop \(you logged in with this key\)`).MatchString(out) {
		t.Errorf("INST-5: current key not marked (cloud-init's root key must be deduplicated):\n%s", out)
	}
	for _, want := range []string{"SSH port [4222]", "is private", "Public IP address of this server [192.168.122.217]", "Which key logs in as server? (number, or p to paste another) [1]", "continuing on your word"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(s.read("home/server/.ssh/authorized_keys"), testKey) {
		t.Error("chosen key not authorised")
	}
}

// INST-6: a public IPv4 needs no question.
func TestInstallPublicAddressNotAsked(t *testing.T) {
	s := newFakeServer(t)
	s.env = append(s.env, "TEST_IPV4=203.0.113.10")
	s.journal(newLoginOn4222...)
	out, err := s.run("\n\n\n\nyes\n") // key, port, sudo: no, plan, yes
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "Public IP address of this server") || !strings.Contains(out, "address 203.0.113.10") {
		t.Errorf("output:\n%s", out)
	}
}

// INST-5 / scenario b: no key on the server; a wrong paste is refused, a valid one accepted.
func TestInstallNoKeyGuidedPaste(t *testing.T) {
	s := newFakeServer(t)
	s.files["home/debian/.ssh/authorized_keys"] = ""
	s.journal(newLoginOn4222...)
	out, err := s.run("not a key\n" + testKey + "\n\n\nn\n\n")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"No SSH key is set up on this server yet", "ssh-keygen -t ed25519", "never leaves the laptop", "that is not a public key"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// LOCK-1 negative control: a config sshd rejects is rolled back and sshd is never reloaded.
func TestInstallRejectedSSHDConfigRolledBack(t *testing.T) {
	s := newFakeServer(t)
	s.writeStub("sshd_t_fails", "")
	out, err := s.run("", s.automated()...)
	if err == nil || !strings.Contains(out, "sshd rejected the new configuration. The previous one is back") {
		t.Fatalf("expected a refusal:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(s.root, dropin)); statErr == nil {
		t.Error("the rejected drop-in must be removed")
	}
	if strings.Contains(s.calls(), "systemctl reload ssh.service") {
		t.Error("sshd must not be reloaded with a rejected config")
	}
}

// LOCK-1 negative control: when sshd does not listen on the new port after the reload, the
// previous config is restored and sshd reloaded again.
func TestInstallSSHDNotListeningRolledBack(t *testing.T) {
	s := newFakeServer(t)
	s.writeStub("sshd_never_listens_4222", "")
	out, err := s.run("", s.automated()...)
	if err == nil || !strings.Contains(out, "sshd did not listen on port(s) 22 4222") {
		t.Fatalf("expected a rollback:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(s.root, dropin)); statErr == nil {
		t.Error("the drop-in must be removed again")
	}
	if n := strings.Count(s.calls(), "systemctl reload ssh.service"); n != 2 {
		t.Errorf("expected reload, then reload of the restored config; got %d", n)
	}
}

// SEC-22: a binary whose checksum does not match, or is missing, is never installed.
func TestInstallRefusesUnverifiedBinary(t *testing.T) {
	for name, checksums := range map[string]string{
		"mismatch": strings.Repeat("a", 64) + "  dermaga-linux-amd64\n",
		"missing":  strings.Repeat("a", 64) + "  dermaga-linux-arm64\n",
	} {
		t.Run(name, func(t *testing.T) {
			s := newFakeServer(t)
			s.writeStub("downloads/checksums.txt", checksums)
			out, err := s.run("", s.automated()...)
			if err == nil || !strings.Contains(out, "refusing to install") {
				t.Fatalf("expected a refusal:\n%s", out)
			}
			if _, statErr := os.Stat(filepath.Join(s.root, "home/server/.local/bin/dermaga")); statErr == nil {
				t.Error("unverified binary installed")
			}
		})
	}
}

// INST-10: a host key that does not match GitHub's published fingerprints aborts.
func TestInstallGitHubKeyMismatch(t *testing.T) {
	s := newFakeServer(t)
	s.writeStub("downloads/meta", `{"ssh_key_fingerprints":{"SHA256_ED25519":"AAAA"},"ssh_keys":["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"]}`)
	out, err := s.run("", s.automated()...)
	if err == nil || !strings.Contains(out, "does not match the published fingerprints") {
		t.Fatalf("expected a refusal:\n%s", out)
	}
}

// INST-2, INST-3, INST-7, INST-8: refusals before anything changes.
func TestInstallRefusesBeforeChanging(t *testing.T) {
	cases := map[string]struct {
		prepare func(*fakeServer)
		tty     string
		args    func(*fakeServer) []string
		want    string
	}{
		"not debian 13":    {prepare: func(s *fakeServer) { s.files["etc/os-release"] = "ID=debian\nVERSION_ID=\"12\"\n" }, want: "dermaga needs Debian 13 (trixie); this server runs debian 12"},
		"port 80 busy":     {prepare: func(s *fakeServer) { s.writeStub("port80_busy", "") }, want: "another program already uses port 80 or 443"},
		"no terminal":      {prepare: func(s *fakeServer) { s.env = append(s.env, "DERMAGA_TEST_TTY=/nonexistent/tty") }, want: "no terminal to ask questions on"},
		"plan declined":    {tty: "\n\n\nn\nn\n", want: "stopped; nothing was changed"},
		"yes needs config": {args: func(*fakeServer) []string { return []string{"--yes"} }, want: "--yes needs --config FILE"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := newFakeServer(t)
			if tc.prepare != nil {
				tc.prepare(s)
			}
			var args []string
			if tc.args != nil {
				args = tc.args(s)
			}
			out, err := s.run(tc.tty, args...)
			if err == nil || !strings.Contains(out, tc.want) {
				t.Fatalf("expected %q:\n%s", tc.want, out)
			}
			for _, rel := range []string{dropin, "etc/nftables.conf", "etc/sysctl.d/90-dermaga.conf"} {
				if _, statErr := os.Stat(filepath.Join(s.root, rel)); statErr == nil {
					t.Errorf("%s written before the refusal", rel)
				}
			}
			if strings.Contains(s.calls(), "apt-get") {
				t.Error("packages installed before the refusal")
			}
		})
	}
}

// bashFn sources install.sh and runs one function.
func bashFn(t *testing.T, env []string, script string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", "-c", "source ./install.sh; "+script)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// LOCK-3: the login detection on real sshd log lines from dermaga-vm.
func TestLoginSeenOnRealJournal(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	_ = os.MkdirAll(bin, 0755)
	if err := os.WriteFile(filepath.Join(bin, "journalctl"), []byte("#!/bin/sh\ncat testdata/journal_vm_verbose.txt\n"), 0755); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + bin + ":" + os.Getenv("PATH")}
	if out, err := bashFn(t, env, "login_seen 0 4222"); err != nil {
		t.Errorf("login on 4222 not seen: %v %s", err, out)
	}
	// negative controls: another port, or a login with no matching Connection line
	if _, err := bashFn(t, env, "login_seen 0 2222"); err == nil {
		t.Error("a login on 4222 must not count for port 2222")
	}
	if _, err := bashFn(t, env, "SERVER_USER=root; login_seen 0 4222"); err == nil {
		t.Error("a login as server must not count for another user")
	}
}

func TestIsPublicIPv4(t *testing.T) {
	for addr, public := range map[string]bool{
		"203.0.113.10": true, "8.8.8.8": true, "10.0.0.1": false, "172.16.5.4": false, "172.32.0.1": true,
		"192.168.122.217": false, "100.64.0.1": false, "100.128.0.1": true, "127.0.0.1": false, "169.254.1.1": false, "256.1.1.1": false, "-": false,
	} {
		_, err := bashFn(t, nil, "is_public_ipv4 "+addr)
		if (err == nil) != public {
			t.Errorf("is_public_ipv4 %s = %v, want %v", addr, err == nil, public)
		}
	}
}

// sshdEffective runs the real sshd -T on install/testdata/debian13_sshd_config (copied from
// dermaga-vm) with dermaga's drop-in, for one connection context.
func sshdEffective(t *testing.T, dropinContent, context string) map[string]string {
	t.Helper()
	sshd, err := exec.LookPath("sshd")
	if err != nil {
		if _, statErr := os.Stat("/usr/sbin/sshd"); statErr != nil {
			t.Skip("sshd not installed")
		}
		sshd = "/usr/sbin/sshd"
	}
	dir := t.TempDir()
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(dir, "hostkey")).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	main, err := os.ReadFile("testdata/debian13_sshd_config")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(dir, "d"), 0755)
	cfg := strings.Replace(string(main), "/etc/ssh/sshd_config.d", filepath.Join(dir, "d"), 1)
	// The final drop-in names Debian's host key files (the test key replaces them) and uses
	// DebianBanner, a Debian patch other builds reject; both are checked by sshd -t on dermaga-vm.
	dropinContent = regexp.MustCompile(`(?m)^(HostKey|DebianBanner) .*\n`).ReplaceAllString(dropinContent, "")
	_ = os.WriteFile(filepath.Join(dir, "sshd_config"), []byte(cfg), 0644)
	_ = os.WriteFile(filepath.Join(dir, "d/00-dermaga.conf"), []byte(dropinContent), 0644)
	out, err := exec.Command(sshd, "-T", "-f", filepath.Join(dir, "sshd_config"), "-h", filepath.Join(dir, "hostkey"), "-C", context).CombinedOutput()
	if err != nil {
		t.Fatalf("sshd -T rejected the config: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(line, " "); ok {
			if k == "port" {
				got[k] += v + " "
			} else {
				got[k] = v
			}
		}
	}
	return got
}

// LOCK-1 with the real sshd: during the confirmation window port 4222 takes only server with a
// key, and port 22 is unchanged for the provider's user.
func TestSSHDOpenPhaseWithRealSSHD(t *testing.T) {
	open, err := bashFn(t, nil, "render_sshd_open 4222 22")
	if err != nil {
		t.Fatal(err)
	}
	newPort := sshdEffective(t, open, "user=debian,host=x,addr=203.0.113.9,lport=4222")
	if newPort["port"] != "22 4222 " || newPort["allowusers"] != "server" || newPort["authenticationmethods"] != "publickey" || newPort["permitrootlogin"] != "no" {
		t.Errorf("port 4222: %v", newPort)
	}
	oldPort := sshdEffective(t, open, "user=debian,host=x,addr=203.0.113.9,lport=22")
	if oldPort["allowusers"] != "" || oldPort["authenticationmethods"] != "any" {
		t.Errorf("port 22 must be unchanged: allowusers=%q authenticationmethods=%q", oldPort["allowusers"], oldPort["authenticationmethods"])
	}
	if oldPort["x11forwarding"] != "yes" {
		t.Error("Match all must keep sshd_config's own lines global")
	}
	same, _ := bashFn(t, nil, "render_sshd_open 22 22")
	if strings.Contains(same, "Match") {
		t.Errorf("same port: no Match block, or the current login would be restricted:\n%s", same)
	}
}

// HOST-2 with the real sshd: key-only on the chosen port, modern algorithms, AllowUsers server,
// LogLevel VERBOSE, root login off, TCP forwarding local only, no stream-local forwarding.
func TestSSHDFinalWithRealSSHD(t *testing.T) {
	final, err := bashFn(t, nil, "render_sshd_final 4222")
	if err != nil {
		t.Fatal(err)
	}
	got := sshdEffective(t, final, "user=server,host=x,addr=203.0.113.9,lport=4222")
	for k, want := range map[string]string{
		"port": "4222 ", "passwordauthentication": "no", "kbdinteractiveauthentication": "no", "permitrootlogin": "no",
		"authenticationmethods": "publickey", "allowusers": "server", "loglevel": "VERBOSE", "allowtcpforwarding": "local",
		"allowstreamlocalforwarding": "no", "x11forwarding": "no", "permittunnel": "no", "gatewayports": "no",
	} {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
	for _, k := range []string{"kexalgorithms", "ciphers", "macs"} {
		if got[k] == "" || strings.Contains(got[k], "sha1") || strings.Contains(got[k], "cbc") {
			t.Errorf("%s = %q", k, got[k])
		}
	}
}

func TestInstallScriptSyntax(t *testing.T) {
	if out, err := exec.Command("bash", "-n", "install.sh").CombinedOutput(); err != nil {
		t.Fatalf("bash -n: %v\n%s", err, out)
	}
}
