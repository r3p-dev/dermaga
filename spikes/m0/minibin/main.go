// Spike M0.5: static Go binary = modernc.org/sqlite (WAL) + embedded SvelteKit SPA.
// Listens on 127.0.0.1:7070 (also the target of the SSH-tunnel spike M0.7).
package main

import (
	"bufio"
	"database/sql"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed all:ui
var uiFS embed.FS

func rssKB() int {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return -1
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if v, ok := strings.CutPrefix(s.Text(), "VmRSS:"); ok {
			n, _ := strconv.Atoi(strings.Fields(v)[0])
			return n
		}
	}
	return -1
}

func main() {
	dsn := "file:" + os.Args[1] + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS apps (id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL);
		INSERT OR IGNORE INTO apps(name) VALUES ('portfolio');`); err != nil {
		log.Fatal(err)
	}
	var mode, ver string
	db.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	db.QueryRow(`SELECT sqlite_version()`).Scan(&ver)
	log.Printf("sqlite %s journal_mode=%s", ver, mode)

	ui, _ := fs.Sub(uiFS, "ui")
	files := http.FileServerFS(ui)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		var n int
		db.QueryRow(`SELECT count(*) FROM apps`).Scan(&n)
		json.NewEncoder(w).Encode(map[string]any{"goVersion": runtime.Version(), "sqlite": ver + " (" + mode + ")", "apps": n, "rssKB": rssKB()})
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(ui, strings.TrimPrefix(r.URL.Path, "/")); err != nil || r.URL.Path == "/" {
			r.URL.Path = "/" // SPA fallback: unknown paths get index.html
		}
		files.ServeHTTP(w, r)
	})
	log.Fatal(http.ListenAndServe("127.0.0.1:7070", mux))
}
