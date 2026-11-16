package main

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	_ "modernc.org/sqlite"
)

//go:embed static/*
var staticFS embed.FS

func main() {
	dbPath := "spike_test.db"
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("Failed to open sqlite: %v", err)
	}
	defer func() {
		db.Close()
		os.Remove(dbPath)
	}()

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS test_apps (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO test_apps (name) VALUES ('dermaga-app-1'), ('dermaga-app-2');
	`)
	if err != nil {
		log.Fatalf("Failed to exec sqlite: %v", err)
	}

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("Failed to sub fs: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/apps", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query("SELECT id, name FROM test_apps")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var id int
			var name string
			_ = rows.Scan(&id, &name)
			fmt.Fprintf(w, "%d: %s\n", id, name)
		}
	})

	server := &http.Server{
		Addr:    "127.0.0.1:8088",
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	fmt.Println("Server running on 127.0.0.1:8088")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	fmt.Println("Shutting down...")
}
