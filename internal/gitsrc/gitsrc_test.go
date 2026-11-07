package gitsrc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestResolveAndExport(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(filepath.Join(src, "app"), 0o755)
	git(t, dir, "init", "-q", "-b", "main", src)
	os.WriteFile(filepath.Join(src, "app", "Containerfile"), []byte("FROM scratch\n"), 0o644)
	git(t, src, "add", ".")
	git(t, src, "commit", "-q", "-m", "first commit")
	first := git(t, src, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(src, "README"), []byte("x"), 0o644)
	git(t, src, "add", ".")
	git(t, src, "commit", "-q", "-m", "second")
	mirror := filepath.Join(dir, "mirror.git")
	git(t, dir, "clone", "-q", "--mirror", src, mirror)

	r := Repo{URL: "git@github.com:o/r.git", Mirror: mirror, Key: "/nonexistent/key", KnownHosts: "/nonexistent/kh"}
	ctx := context.Background()
	head, subject, err := r.Resolve(ctx, "main", "")
	if err != nil || head == first || subject != "second" || len(head) != 40 {
		t.Fatal(head, subject, err)
	}
	sha, subject, err := r.Resolve(ctx, "main", first[:9])
	if err != nil || sha != first || subject != "first commit" {
		t.Fatal(sha, subject, err)
	}
	for _, bad := range []string{"main", "--upload-pack=x", "HEAD~1", strings.Repeat("0", 40)} {
		if _, _, err := r.Resolve(ctx, "main", bad); err == nil {
			t.Errorf("ref %q accepted", bad)
		}
	}
	if _, _, err := r.Resolve(ctx, "nope", ""); err == nil {
		t.Error("missing branch resolved")
	}
	out := filepath.Join(dir, "build", "src")
	os.MkdirAll(filepath.Dir(out), 0o700)
	if err := r.Export(ctx, first, out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "app", "Containerfile")); err != nil {
		t.Error("exported tree incomplete")
	}
	for _, p := range []string{".git", "README"} { // .git never; README only exists in the later commit
		if _, err := os.Stat(filepath.Join(out, p)); err == nil {
			t.Errorf("%s in the export of the first commit", p)
		}
	}
	if (Repo{URL: r.URL, Mirror: mirror, Key: "/key with space", KnownHosts: "/kh"}).Fetch(ctx, nil) == nil {
		t.Error("unsafe key path accepted (GIT_SSH_COMMAND goes through a shell)")
	}
}

func TestKnownHosts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ssh_keys":["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"],
			"ssh_key_fingerprints":{"SHA256_ED25519":"+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU"}}`))
	}))
	defer srv.Close()
	file, fps, err := KnownHosts(context.Background(), srv.Client(), srv.URL)
	if err != nil || string(file) != "github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl\n" || fps["SHA256_ED25519"] == "" {
		t.Fatalf("%q %v %v", file, fps, err)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ssh_keys":["ssh-ed25519 AAAA\nevil.com ssh-rsa BBBB"]}`))
	}))
	defer bad.Close()
	if _, _, err := KnownHosts(context.Background(), bad.Client(), bad.URL); err == nil {
		t.Error("a key with a line break was accepted")
	}
}

func TestDeployKey(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not installed")
	}
	p := filepath.Join(t.TempDir(), "keys", "shop")
	pub, err := DeployKey(context.Background(), p, "dermaga-shop")
	if err != nil || !strings.HasPrefix(pub, "ssh-ed25519 ") || !strings.HasSuffix(pub, " dermaga-shop") {
		t.Fatal(pub, err)
	}
	again, _ := DeployKey(context.Background(), p, "dermaga-shop")
	fi, _ := os.Stat(p)
	if again != pub || fi.Mode().Perm() != 0o600 {
		t.Errorf("key regenerated (%v) or mode %v", again != pub, fi.Mode().Perm())
	}
	if fp, err := Fingerprint(context.Background(), p+".pub"); err != nil || !strings.HasPrefix(fp, "SHA256:") {
		t.Error(fp, err)
	}
}
