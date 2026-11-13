package cli

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r3p-dev/dermaga/internal/api"
	"github.com/r3p-dev/dermaga/internal/backup"
	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/databases"
	"github.com/r3p-dev/dermaga/internal/secrets"
	"github.com/r3p-dev/dermaga/internal/store"
)

// Only --scheduled marks a timer run. INVOCATION_ID is inherited by every
// child of a systemd service (the daemon included) and must not count.
func TestBackupRunRequestScheduled(t *testing.T) {
	t.Setenv("INVOCATION_ID", "0123456789abcdef0123456789abcdef")
	req, err := backupRunRequest([]string{"system"})
	if err != nil || req.Scheduled || req.Target != "system" {
		t.Errorf("without the flag: %+v, %v; want an unscheduled run of system", req, err)
	}
	req, err = backupRunRequest([]string{"--scheduled", "db:shop"})
	if err != nil || !req.Scheduled || req.Target != "db:shop" {
		t.Errorf("with the flag: %+v, %v; want a scheduled run of db:shop", req, err)
	}
	if _, err := backupRunRequest(nil); err == nil {
		t.Error("a run without a target was accepted")
	}
}

// The S3 secret key comes from stdin (one line), never from a flag, and ends
// up stored: checked against a real daemon API on a Unix socket.
func TestBackupSettingsSetSecretFromStdin(t *testing.T) {
	td := t.TempDir()
	db, err := store.Open(filepath.Join(td, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box, _ := secrets.New(bytes.Repeat([]byte{7}, 32))
	mgr := backup.NewManager(db, box, databases.Store{DB: db, Box: box}, config.Paths{DataDir: td}, nil)
	ln, err := net.Listen("unix", filepath.Join(td, "d.sock"))
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: (&api.Server{DB: db, Backup: mgr, Box: box}).Socket()}
	go srv.Serve(ln)
	defer srv.Close()
	socketOverride = ln.Addr().String()
	defer func() { socketOverride = "" }()

	const secret = "s3-secret-from-stdin"
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString(secret + "\n")
	w.Close()
	oldFile, oldReader := os.Stdin, stdin
	os.Stdin, stdin = r, bufio.NewReader(r) // a pipe, not a terminal: the scripted path
	defer func() { os.Stdin, stdin = oldFile, oldReader }()

	args := []string{"--s3-endpoint", "s3.example.com", "--s3-bucket", "bkt", "--s3-access-key", "AK", "--s3-secret-key", "--json"}
	for _, a := range args {
		if strings.Contains(a, secret) {
			t.Fatal("the secret must not be an argument")
		}
	}
	if err := backupSettingsSet(args); err != nil {
		t.Fatal(err)
	}
	st, err := mgr.GetSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.S3SecretKey != secret || st.S3Bucket != "bkt" || st.S3AccessKey != "AK" || st.Repository != "s3:https://s3.example.com/bkt" {
		t.Errorf("stored settings: %+v", st)
	}
}

func TestMergeBackupSettings(t *testing.T) {
	cur := backup.Settings{
		Repository: "/home/server/.local/share/dermaga/restic",
		S3Region:   "auto", S3AccessKey: "AK", S3SecretKey: "••••••••", Configured: true,
	}
	in := backup.Settings{Repository: "s3:https://h/b2", S3Endpoint: "s3.example.com", S3Bucket: "bkt", S3Region: "eu", S3AccessKey: "AK2"}

	// Only the region changes; secrets are never sent back.
	got := mergeBackupSettings(cur, in, map[string]bool{"s3-region": true})
	if got.S3SecretKey != "" {
		t.Errorf("secret echoed back: %+v", got)
	}
	if got.S3Region != "eu" || got.Repository != cur.Repository || got.S3AccessKey != "AK" || got.S3Endpoint != "" {
		t.Errorf("region-only change: %+v", got)
	}

	// New endpoint/bucket without --repo: the server derives the repository.
	got = mergeBackupSettings(cur, in, map[string]bool{"s3-endpoint": true, "s3-bucket": true, "s3-access-key": true})
	if got.Repository != "" || got.S3Endpoint != "s3.example.com" || got.S3Bucket != "bkt" || got.S3AccessKey != "AK2" {
		t.Errorf("S3 change: %+v", got)
	}

	// An explicit --repo wins.
	got = mergeBackupSettings(cur, in, map[string]bool{"repo": true, "s3-bucket": true})
	if got.Repository != "s3:https://h/b2" {
		t.Errorf("explicit repo: %+v", got)
	}
}
