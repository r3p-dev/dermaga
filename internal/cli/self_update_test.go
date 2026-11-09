package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseChecksum(t *testing.T) {
	manifest := `# SHA256 manifest
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  dermaga-linux-amd64
11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff *dermaga-linux-arm64
`

	hash, err := ParseChecksum(manifest, "dermaga-linux-amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("wrong hash: %s", hash)
	}

	hash, err = ParseChecksum(manifest, "dermaga-linux-arm64")
	if err != nil {
		t.Fatalf("unexpected error with asterisk: %v", err)
	}
	if hash != "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff" {
		t.Errorf("wrong hash: %s", hash)
	}

	_, err = ParseChecksum(manifest, "dermaga-linux-riscv64")
	if err == nil {
		t.Fatal("expected error for missing file")
	}

	badManifest := "short_hash  dermaga-linux-amd64\n"
	_, err = ParseChecksum(badManifest, "dermaga-linux-amd64")
	if err == nil {
		t.Fatal("expected error for invalid hash length")
	}
}

func TestSelfUpdateCheck(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/r3p-dev/dermaga/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"tag_name": "v1.2.3"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	client := srv.Client()

	opts := SelfUpdateOptions{
		Check:      true,
		Repo:       "r3p-dev/dermaga",
		Stdout:     &stdout,
		Stderr:     &stderr,
		HTTPClient: client,
	}

	// Override API request URL by routing through test server client transport
	client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(req)
	})

	err := RunSelfUpdate(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Latest version:  v1.2.3") {
		t.Errorf("expected latest version in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Update available") {
		t.Errorf("expected update available note, got:\n%s", out)
	}
}

func TestSelfUpdateSuccess(t *testing.T) {
	dir := t.TempDir()
	binaryName := fmt.Sprintf("dermaga-linux-%s", runtime.GOARCH)

	// Create a dummy executable script that satisfies "version" check
	scriptContent := "#!/bin/sh\nif [ \"$1\" = \"version\" ]; then echo \"dermaga v1.2.3\"; exit 0; fi\nexit 0\n"
	newBinData := []byte(scriptContent)
	h := sha256.Sum256(newBinData)
	expectedHash := hex.EncodeToString(h[:])

	checksumsContent := fmt.Sprintf("%s  %s\n", expectedHash, binaryName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checksums.txt":
			w.Write([]byte(checksumsContent))
		case "/" + binaryName:
			w.Write(newBinData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Initial dummy binary at target location
	targetBin := filepath.Join(dir, "dermaga")
	if err := os.WriteFile(targetBin, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	opts := SelfUpdateOptions{
		Version:     "v1.2.3",
		Force:       true,
		BaseURL:     srv.URL,
		BinaryPath:  targetBin,
		SkipRestart: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
		HTTPClient:  srv.Client(),
	}

	err := RunSelfUpdate(context.Background(), opts)
	if err != nil {
		t.Fatalf("self-update failed: %v", err)
	}

	// Verify content was updated
	updatedData, err := os.ReadFile(targetBin)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(updatedData, newBinData) {
		t.Errorf("binary content does not match new binary")
	}

	// Verify backup was cleaned up
	if _, err := os.Stat(targetBin + ".old"); !os.IsNotExist(err) {
		t.Errorf("backup file still exists")
	}

	if !strings.Contains(stdout.String(), "SHA-256 verified") {
		t.Errorf("missing verification message: %s", stdout.String())
	}
}

func TestSelfUpdateChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	binaryName := fmt.Sprintf("dermaga-linux-%s", runtime.GOARCH)

	tamperedData := []byte("#!/bin/sh\necho evil\n")
	bogusChecksums := fmt.Sprintf("0000000000000000000000000000000000000000000000000000000000000000  %s\n", binaryName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checksums.txt":
			w.Write([]byte(bogusChecksums))
		case "/" + binaryName:
			w.Write(tamperedData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	initialContent := []byte("#!/bin/sh\necho original\n")
	targetBin := filepath.Join(dir, "dermaga")
	if err := os.WriteFile(targetBin, initialContent, 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	opts := SelfUpdateOptions{
		Version:     "v1.2.3",
		Force:       true,
		BaseURL:     srv.URL,
		BinaryPath:  targetBin,
		SkipRestart: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
		HTTPClient:  srv.Client(),
	}

	err := RunSelfUpdate(context.Background(), opts)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("wrong error message: %v", err)
	}

	// Verify target was not overwritten
	currentData, err := os.ReadFile(targetBin)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(currentData, initialContent) {
		t.Errorf("target binary was modified despite checksum mismatch!")
	}
}

func TestResolveLatestReleaseTagRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/myorg/myrepo/releases/latest" {
			http.Redirect(w, r, "/myorg/myrepo/releases/tag/v2.4.6", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := srv.Client()
	client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(req)
	})

	tag, err := resolveLatestReleaseTag(context.Background(), client, "myorg/myrepo")
	if err != nil {
		t.Fatalf("unexpected error resolving tag via redirect: %v", err)
	}
	if tag != "v2.4.6" {
		t.Errorf("expected tag v2.4.6, got %q", tag)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
