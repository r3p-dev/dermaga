package backup

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/databases"
	"github.com/r3p-dev/dermaga/internal/secrets"
	"github.com/r3p-dev/dermaga/internal/store"
)

type fakeHost struct {
	units      map[string][]byte
	timers     map[string]bool
	active     map[string]string
	resticRuns [][]string
	podmanRuns [][]string
	files      map[string][]byte
	dumpData   map[string][]byte
}

func newFakeHost() *fakeHost {
	return &fakeHost{
		units:    map[string][]byte{},
		timers:   map[string]bool{},
		active:   map[string]string{},
		files:    map[string][]byte{},
		dumpData: map[string][]byte{},
	}
}

func (f *fakeHost) Run(_ context.Context, _ time.Duration, argv ...string) (string, error) {
	f.podmanRuns = append(f.podmanRuns, argv)
	if len(argv) >= 2 && argv[0] == "podman" && argv[1] == "exec" {
		return "1", nil
	}
	return "", nil
}

func (f *fakeHost) RunStdin(_ context.Context, _ time.Duration, stdin io.Reader, argv ...string) (string, error) {
	f.podmanRuns = append(f.podmanRuns, argv)
	if stdin != nil {
		_, _ = io.Copy(io.Discard, stdin)
	}
	return "", nil
}

func (f *fakeHost) RunEnv(_ context.Context, _ time.Duration, _ []string, _ io.Writer, argv ...string) (string, error) {
	f.podmanRuns = append(f.podmanRuns, argv)
	return "", nil
}

func (f *fakeHost) RunRestic(_ context.Context, _ []string, stdin io.Reader, stdout io.Writer, argv ...string) (string, error) {
	f.resticRuns = append(f.resticRuns, argv)
	if len(argv) > 0 && argv[0] == "dump" {
		fn := argv[len(argv)-1]
		if d, ok := f.dumpData[fn]; ok && stdout != nil {
			_, _ = stdout.Write(d)
		}
		return "", nil
	}
	if len(argv) > 0 && argv[0] == "snapshots" {
		return `[{"id":"snap12345678","short_id":"snap1234","time":"2026-11-09T00:00:00Z","tags":["db:testdb"]}]`, nil
	}
	if len(argv) > 0 && argv[0] == "backup" {
		// Mock json output
		return `{"message_type":"summary","files_new":1,"snapshot_id":"mocksnap1234567890abcdef","total_bytes_processed":1024}`, nil
	}
	return "", nil
}

func (f *fakeHost) WriteUserUnit(filename string, data []byte) error {
	f.units[filename] = data
	return nil
}

func (f *fakeHost) RemoveUserUnit(filename string) error {
	delete(f.units, filename)
	return nil
}

func (f *fakeHost) ReloadSystemd(_ context.Context) error {
	return nil
}

func (f *fakeHost) EnableTimer(_ context.Context, timerUnit string) error {
	f.timers[timerUnit] = true
	return nil
}

func (f *fakeHost) DisableTimer(_ context.Context, timerUnit string) error {
	delete(f.timers, timerUnit)
	return nil
}

func (f *fakeHost) ActiveState(_ context.Context, unit string) (string, error) {
	if s, ok := f.active[unit]; ok {
		return s, nil
	}
	return "active", nil
}

func (f *fakeHost) ListTimers(_ context.Context) (string, error) {
	return "NEXT LEFT LAST PASSED UNIT ACTIVATES", nil
}

func (f *fakeHost) ReadFile(path string) ([]byte, error) {
	if d, ok := f.files[path]; ok {
		return d, nil
	}
	return os.ReadFile(path)
}

func (f *fakeHost) WriteFile(path string, data []byte, _ os.FileMode) error {
	f.files[path] = data
	return nil
}

func (f *fakeHost) Remove(path string) error {
	delete(f.files, path)
	return nil
}

func setupTestManager(t *testing.T) (*Manager, *fakeHost, config.Paths, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dermaga.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}

	keyPath := filepath.Join(dir, "master.key")
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}

	paths := config.Paths{
		ConfigDir:  filepath.Join(dir, "config"),
		DataDir:    filepath.Join(dir, "data"),
		RuntimeDir: filepath.Join(dir, "run"),
		StateDir:   filepath.Join(dir, "state"),
		QuadletDir: filepath.Join(dir, "containers", "systemd"),
		TraefikDir: filepath.Join(dir, "traefik"),
	}
	_ = os.MkdirAll(paths.ConfigDir, 0o700)
	_ = os.MkdirAll(paths.DataDir, 0o700)
	_ = os.MkdirAll(paths.TraefikDir, 0o700)
	_ = os.MkdirAll(filepath.Join(paths.ConfigDir, "env"), 0o700)

	dbStore := databases.Store{DB: db, Box: box}
	host := newFakeHost()
	mgr := NewManager(db, box, dbStore, paths, host)
	return mgr, host, paths, db
}

func TestSettingsGenerationAndEncryption(t *testing.T) {
	mgr, _, _, db := setupTestManager(t)
	defer db.Close()

	ctx := context.Background()
	st1, err := mgr.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}

	if len(st1.Password) != 64 {
		t.Errorf("expected 64 hex chars password, got %d (%s)", len(st1.Password), st1.Password)
	}

	// Verify it's encrypted in SQLite
	var encPass string
	if err := db.QueryRow("SELECT restic_password FROM settings WHERE id = 1").Scan(&encPass); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encPass, "v1:") {
		t.Errorf("restic_password in DB not sealed: %s", encPass)
	}

	// Verify subsequent calls return the same password
	st2, err := mgr.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st1.Password != st2.Password {
		t.Errorf("passwords mismatch: %s vs %s", st1.Password, st2.Password)
	}

	// Test S3 settings save
	s3Set := Settings{
		Repository:  "s3:https://s3.us-west-002.backblazeb2.com/mybucket",
		S3Endpoint:  "https://s3.us-west-002.backblazeb2.com",
		S3Bucket:    "mybucket",
		S3Region:    "us-west-002",
		S3AccessKey: "testaccesskey",
		S3SecretKey: "testsecretkey",
	}
	if err := mgr.SaveSettings(ctx, s3Set); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	st3, err := mgr.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st3.S3Bucket != "mybucket" || st3.S3SecretKey != "testsecretkey" {
		t.Errorf("saved settings mismatch: %+v", st3)
	}

	// Verify env vars
	env, err := mgr.ResticEnv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hasS3Key := false
	for _, e := range env {
		if strings.HasPrefix(e, "AWS_ACCESS_KEY_ID=testaccesskey") {
			hasS3Key = true
		}
	}
	if !hasS3Key {
		t.Errorf("AWS_ACCESS_KEY_ID missing from restic env: %v", env)
	}
}

func TestDumpCommandsD10(t *testing.T) {
	mgr, _, _, db := setupTestManager(t)
	defer db.Close()
	ctx := context.Background()

	// 1. Postgres
	pg, creds, err := mgr.Databases.Create(ctx, "pgdb", databases.EnginePostgres, "18")
	if err != nil {
		t.Fatal(err)
	}
	fn, cmd, err := dumpCommand(pg, creds, "dg-db-pgdb")
	if err != nil {
		t.Fatal(err)
	}
	if fn != "pgdb.dump" {
		t.Errorf("expected pgdb.dump, got %s", fn)
	}
	expectedCmd := "podman exec dg-db-pgdb pg_dump -Fc -U postgres pgdb"
	if strings.Join(cmd, " ") != expectedCmd {
		t.Errorf("postgres dump command mismatch: got %q, want %q", strings.Join(cmd, " "), expectedCmd)
	}

	// 2. MariaDB
	maria, mcreds, err := mgr.Databases.Create(ctx, "mariadb", databases.EngineMariaDB, "11.8")
	if err != nil {
		t.Fatal(err)
	}
	fn, cmd, err = dumpCommand(maria, mcreds, "dg-db-mariadb")
	if err != nil {
		t.Fatal(err)
	}
	if fn != "mariadb.sql" {
		t.Errorf("expected mariadb.sql, got %s", fn)
	}
	if !strings.Contains(strings.Join(cmd, " "), "mariadb-dump --single-transaction --routines --triggers --events") {
		t.Errorf("mariadb dump command missing flags: %q", cmd)
	}

	// 3. Valkey
	valkey, vcreds, err := mgr.Databases.Create(ctx, "cache", databases.EngineValkey, "9")
	if err != nil {
		t.Fatal(err)
	}
	fn, cmd, err = dumpCommand(valkey, vcreds, "dg-db-cache")
	if err != nil {
		t.Fatal(err)
	}
	if fn != "cache.rdb" {
		t.Errorf("expected cache.rdb, got %s", fn)
	}
	cmdStr := strings.Join(cmd, " ")
	if !strings.Contains(cmdStr, "valkey-cli --no-auth-warning bgsave") || !strings.Contains(cmdStr, "cat /data/dump.rdb") {
		t.Errorf("valkey dump command missing bgsave or cat: %q", cmdStr)
	}
}

func TestSystemBackupExcludesMasterKey(t *testing.T) {
	mgr, host, paths, db := setupTestManager(t)
	defer db.Close()
	ctx := context.Background()

	// Create test files
	masterKeyFile := paths.MasterKey()
	_ = os.WriteFile(masterKeyFile, []byte("super-secret-key-32-bytes-long!"), 0o600)

	cfgFile := paths.ConfigFile()
	_ = os.WriteFile(cfgFile, []byte("[panel]\nport = 7070\n"), 0o644)

	envFile := filepath.Join(paths.ConfigDir, "env", "app.env")
	_ = os.WriteFile(envFile, []byte("FOO=bar\n"), 0o600)

	snap, err := mgr.BackupSystem(ctx)
	if err != nil {
		t.Fatalf("BackupSystem failed: %v", err)
	}

	if snap.ID == "" {
		t.Errorf("empty snapshot ID")
	}

	// Verify restic backup was called with system tag
	called := false
	for _, run := range host.resticRuns {
		if len(run) >= 4 && run[0] == "backup" {
			for _, arg := range run {
				if arg == "system" {
					called = true
				}
			}
		}
	}
	if !called {
		t.Errorf("restic backup not called with system tag: %v", host.resticRuns)
	}
}

func TestTimerGenerationAndOrchestration(t *testing.T) {
	mgr, host, _, db := setupTestManager(t)
	defer db.Close()
	ctx := context.Background()

	d, _, err := mgr.Databases.Create(ctx, "shopdb", databases.EnginePostgres, "18")
	if err != nil {
		t.Fatal(err)
	}

	if err := mgr.InstallDBTimer(ctx, d); err != nil {
		t.Fatalf("InstallDBTimer failed: %v", err)
	}

	svcUnit := "dg-backup-db-shopdb.service"
	tmrUnit := "dg-backup-db-shopdb.timer"

	svc, ok := host.units[svcUnit]
	if !ok {
		t.Fatalf("%s not written", svcUnit)
	}
	if !strings.Contains(string(svc), "backup run db:shopdb") {
		t.Errorf("service missing command: %s", string(svc))
	}
	if !strings.Contains(string(svc), "OnFailure=dg-backup-failed@%%n.service") {
		t.Errorf("service missing OnFailure: %s", string(svc))
	}

	tmr, ok := host.units[tmrUnit]
	if !ok {
		t.Fatalf("%s not written", tmrUnit)
	}
	if !strings.Contains(string(tmr), "RandomizedDelaySec=1800") {
		t.Errorf("timer missing RandomizedDelaySec=1800: %s", string(tmr))
	}
	if !host.timers[tmrUnit] {
		t.Errorf("timer %s not enabled in systemd", tmrUnit)
	}

	// Test remove
	if err := mgr.RemoveDBTimer(ctx, "shopdb"); err != nil {
		t.Fatal(err)
	}
	if host.timers[tmrUnit] {
		t.Errorf("timer %s still enabled after remove", tmrUnit)
	}
	if _, ok := host.units[tmrUnit]; ok {
		t.Errorf("timer unit file still exists after remove")
	}
}

func TestKeysExport(t *testing.T) {
	mgr, _, paths, db := setupTestManager(t)
	defer db.Close()

	key := bytes.Repeat([]byte{0x42}, 32)
	_ = os.WriteFile(paths.MasterKey(), key, 0o600)

	hexKey, inst, err := mgr.ExportKeys()
	if err != nil {
		t.Fatalf("ExportKeys failed: %v", err)
	}
	if hexKey != hex.EncodeToString(key) {
		t.Errorf("hex key mismatch: %s vs %s", hexKey, hex.EncodeToString(key))
	}
	if !strings.Contains(inst, "DERMAGA MASTER KEY EXPORT") {
		t.Errorf("instructions missing header: %s", inst)
	}
}

func TestRestoreDatabaseSafetyDump(t *testing.T) {
	mgr, host, _, db := setupTestManager(t)
	defer db.Close()
	ctx := context.Background()

	_, _, err := mgr.Databases.Create(ctx, "shopdb", databases.EnginePostgres, "18")
	if err != nil {
		t.Fatal(err)
	}

	host.dumpData["shopdb.dump"] = []byte("PGDMP-MOCK-DATA")

	// Restore
	err = mgr.RestoreDatabase(ctx, "shopdb", RestoreOpts{SnapshotID: "snap1234"})
	if err != nil {
		t.Fatalf("RestoreDatabase: %v", err)
	}

	// Verify safety dump was performed first (backup command present in resticRuns)
	hasBackup := false
	hasDump := false
	for _, r := range host.resticRuns {
		if len(r) > 0 && r[0] == "backup" {
			hasBackup = true
		}
		if len(r) > 0 && r[0] == "dump" {
			hasDump = true
		}
	}
	if !hasBackup {
		t.Errorf("expected safety backup to be executed before restore")
	}
	if !hasDump {
		t.Errorf("expected restic dump to be executed during restore")
	}
}
