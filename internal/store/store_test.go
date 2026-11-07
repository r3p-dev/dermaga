package store

import (
	"fmt"
	_ "github.com/r3p-dev/dermaga/internal/golden" // accepts go test ./... -update
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestOpenMigratesAndRerunIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO audit_log (ts, actor, action) VALUES (1, 'cli', 'x')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		if fi, err := os.Stat(f); err == nil && fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", f, fi.Mode().Perm())
		}
	}

	db, err = Open(path) // second run: nothing re-applied, data kept
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v, rows, settings, fk int
	var mode string
	db.QueryRow("PRAGMA user_version").Scan(&v)
	db.QueryRow("SELECT count(*) FROM audit_log").Scan(&rows)
	db.QueryRow("SELECT count(*) FROM settings").Scan(&settings)
	db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	entries, _ := migrations.ReadDir("migrations")
	if v != len(entries) || rows != 1 || settings != 1 || mode != "wal" || fk != 1 {
		t.Fatalf("user_version=%d (want %d) audit=%d settings=%d journal=%s fk=%d", v, len(entries), rows, settings, mode, fk)
	}
}

func TestMigrateRollsBackAndRejectsBadNumbering(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var before int
	db.QueryRow("PRAGMA user_version").Scan(&before)

	fsys := fstest.MapFS{}
	for i := 1; i <= before; i++ { // already applied, skipped
		fsys[fmt.Sprintf("migrations/%03d_x.sql", i)] = &fstest.MapFile{Data: []byte("SELECT 1;")}
	}
	fsys[fmt.Sprintf("migrations/%03d_bad.sql", before+1)] = &fstest.MapFile{
		Data: []byte("CREATE TABLE t_new (x INTEGER); THIS IS NOT SQL;")}
	if err := migrate(db, fsys); err == nil {
		t.Fatal("broken migration accepted")
	}
	var after, tables int
	db.QueryRow("PRAGMA user_version").Scan(&after)
	db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 't_new'").Scan(&tables)
	if after != before || tables != 0 {
		t.Fatalf("not rolled back: user_version %d→%d, t_new=%d", before, after, tables)
	}

	if err := migrate(db, fstest.MapFS{"migrations/002_gap.sql": {Data: []byte("SELECT 1;")}}); err == nil {
		t.Fatal("gap in migration numbers accepted")
	}
}
