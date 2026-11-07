// Package store opens dermaga's SQLite database and applies its migrations.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens (creating if needed) the database at path in WAL mode with
// foreign keys on, and migrates it to the latest schema.
func Open(path string) (*sql.DB, error) {
	// SQLite gives -wal and -shm the database file's mode, so 0600 here covers all three.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+
		"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	// One connection: the daemon's write rate is tiny, and it rules out SQLITE_BUSY between our own connections.
	db.SetMaxOpenConns(1)
	if err := migrate(db, migrations); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// migrate applies migrations/NNN_*.sql in order. PRAGMA user_version holds the
// number of the last applied migration and changes in the same transaction.
func migrate(db *sql.DB, fsys fs.FS) error {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return err
	}
	var cur int
	if err := db.QueryRow("PRAGMA user_version").Scan(&cur); err != nil {
		return err
	}
	if cur > len(entries) {
		return fmt.Errorf("database schema version %d is newer than this binary (%d)", cur, len(entries))
	}
	for i, e := range entries {
		v := i + 1
		if !strings.HasPrefix(e.Name(), fmt.Sprintf("%03d_", v)) {
			return fmt.Errorf("migration %s: expected prefix %03d_", e.Name(), v)
		}
		if v <= cur {
			continue
		}
		q, err := fs.ReadFile(fsys, "migrations/"+e.Name())
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(q)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", v)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
