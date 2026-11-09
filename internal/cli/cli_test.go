package cli

import (
	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSubIDCount(t *testing.T) {
	u := &user.User{Username: "server", Uid: "1001"}
	data := "debian:100000:65536\nserver:165536:65536\n"
	if n := subIDCount(data, u); n != 65536 {
		t.Fatal(n)
	}
	if n := subIDCount("1001:200000:1000\n", u); n != 1000 {
		t.Fatal("numeric uid entry:", n)
	}
	if n := subIDCount("serverx:1:65536\nbroken\n", u); n != 0 {
		t.Fatal("prefix matched:", n)
	}
}

func TestSSHPortAndSnippet(t *testing.T) {
	dir := t.TempDir()
	drop := filepath.Join(dir, "00-vps.conf")
	main := filepath.Join(dir, "sshd_config")
	os.WriteFile(drop, []byte("# Port 1\nPasswordAuthentication no\nPort 4222\n"), 0o644)
	os.WriteFile(main, []byte("Include /etc/ssh/sshd_config.d/*.conf\nPort 2222\n"), 0o644)
	if p := sshPort([]string{filepath.Join(dir, "missing"), drop, main}); p != "4222" {
		t.Fatal(p)
	}
	if p := sshPort(nil); p != "22" {
		t.Fatal(p)
	}
	s := panelSnippet("vps.example", "4222", "server", 7070)
	for _, want := range []string{"HostName vps.example", "Port 4222", "User server", "LocalForward 7070 127.0.0.1:7070",
		"SessionType none", "ExitOnForwardFailure yes", "http://localhost:7070"} {
		if !strings.Contains(s, want) {
			t.Errorf("snippet lacks %q", want)
		}
	}
}

func TestClockCheckToleratesEarlyBoot(t *testing.T) {
	for _, c := range []struct {
		synced bool
		up     time.Duration
		want   string
	}{
		{true, time.Second, "ok"},
		{false, 6 * time.Second, "warn"},
		{false, 10 * time.Minute, "fail"},
	} {
		if got := clockCheck(c.synced, c.up).Status; got != c.want {
			t.Errorf("synced=%v up=%v: %s, want %s", c.synced, c.up, got, c.want)
		}
	}
}

func TestRollbackCommandHelp(t *testing.T) {
	if code := Main([]string{"rollback", "shop", "-h"}); code != 0 {
		t.Fatalf("rollback shop -h exited with %d", code)
	}
}

func TestDBCommandHelp(t *testing.T) {
	for _, args := range [][]string{
		{"db", "-h"},
		{"db", "list", "-h"},
		{"db", "create", "test", "-h"},
		{"db", "show", "test", "-h"},
		{"db", "link", "test", "app", "-h"},
		{"db", "remove", "test", "-h"},
		{"db", "proxy", "test", "-h"},
	} {
		if code := Main(args); code != 0 {
			t.Errorf("command %v exited with %d, want 0", args, code)
		}
	}
}
