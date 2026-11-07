// Package audit records every mutating action (BRIEF §3.4). Details never contain secrets.
package audit

import (
	"database/sql"
	"time"
)

type Entry struct {
	Time    time.Time `json:"time"`
	Actor   string    `json:"actor"` // panel, cli, webhook, timer
	Action  string    `json:"action"`
	Target  string    `json:"target,omitempty"`
	Details string    `json:"details,omitempty"`
}

func Log(db *sql.DB, actor, action, target, details string) error {
	_, err := db.Exec(`INSERT INTO audit_log (ts, actor, action, target, details) VALUES (?, ?, ?, ?, ?)`,
		time.Now().Unix(), actor, action, target, details)
	return err
}

// List returns the newest limit entries, newest first.
func List(db *sql.DB, limit int) ([]Entry, error) {
	rows, err := db.Query(`SELECT ts, actor, action, target, details FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var ts int64
		if err := rows.Scan(&ts, &e.Actor, &e.Action, &e.Target, &e.Details); err != nil {
			return nil, err
		}
		e.Time = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}
