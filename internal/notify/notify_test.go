package notify

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/r3p-dev/dermaga/internal/secrets"
	_ "modernc.org/sqlite"
)

type mockClient struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (m *mockClient) Do(req *http.Request) (*http.Response, error) {
	return m.fn(req)
}

func TestSettingsPersistence(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			notify_provider TEXT NOT NULL DEFAULT '',
			notify_target   TEXT NOT NULL DEFAULT '',
			notify_token    TEXT NOT NULL DEFAULT '',
			notify_events   TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO settings (id) VALUES (1);
	`)
	if err != nil {
		t.Fatal(err)
	}

	key := [32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	box, err := secrets.New(key[:])
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	initial, err := LoadSettings(ctx, db, box)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Provider != ProviderDisabled {
		t.Errorf("expected initial disabled, got %v", initial.Provider)
	}

	save := Settings{
		Provider: ProviderTelegram,
		Target:   "-1001234567890",
		Token:    "bot123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
		Events:   "deploy.failed,backup.failed,disk.warning",
	}
	if err := SaveSettings(ctx, db, box, save); err != nil {
		t.Fatalf("SaveSettings error: %v", err)
	}

	loaded, err := LoadSettings(ctx, db, box)
	if err != nil {
		t.Fatalf("LoadSettings error: %v", err)
	}
	if loaded.Provider != ProviderTelegram || loaded.Target != save.Target || loaded.Token != save.Token {
		t.Errorf("loaded settings mismatch: %+v; want %+v", loaded, save)
	}
}

func TestNtfyDispatch(t *testing.T) {
	var capturedReq *http.Request
	var capturedBody []byte

	client := &mockClient{
		fn: func(req *http.Request) (*http.Response, error) {
			capturedReq = req
			capturedBody, _ = io.ReadAll(req.Body)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"id":"1"}`))),
			}, nil
		},
	}

	d := &Dispatcher{Client: client}
	cfg := Settings{
		Provider: ProviderNtfy,
		Target:   "my-secret-topic",
		Token:    "tk_test123",
		Events:   "deploy.failed",
	}
	msg := Message{
		Event:     "deploy.failed",
		Title:     "Deployment Failed",
		Body:      "Container exited with code 1",
		Level:     "error",
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	if err := d.Dispatch(ctx, cfg, msg); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if capturedReq.URL.String() != "https://ntfy.sh/my-secret-topic" {
		t.Errorf("expected URL https://ntfy.sh/my-secret-topic, got %s", capturedReq.URL.String())
	}
	if capturedReq.Header.Get("Title") != "Deployment Failed" {
		t.Errorf("title mismatch: %s", capturedReq.Header.Get("Title"))
	}
	if capturedReq.Header.Get("Priority") != "high" {
		t.Errorf("priority mismatch: %s", capturedReq.Header.Get("Priority"))
	}
	if capturedReq.Header.Get("Authorization") != "Bearer tk_test123" {
		t.Errorf("authorization header mismatch: %s", capturedReq.Header.Get("Authorization"))
	}
	if string(capturedBody) != "Container exited with code 1" {
		t.Errorf("body mismatch: %s", string(capturedBody))
	}
}

func TestTelegramDispatch(t *testing.T) {
	var capturedReq *http.Request
	var capturedBody []byte

	client := &mockClient{
		fn: func(req *http.Request) (*http.Response, error) {
			capturedReq = req
			capturedBody, _ = io.ReadAll(req.Body)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"ok":true}`))),
			}, nil
		},
	}

	d := &Dispatcher{Client: client}
	cfg := Settings{
		Provider: ProviderTelegram,
		Target:   "12345678",
		Token:    "secret_bot_token",
		Events:   "backup.failed",
	}
	msg := Message{
		Event:     "backup.failed",
		Title:     "Backup Failed",
		Body:      "Restic snapshot returned error",
		Level:     "error",
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	if err := d.Dispatch(ctx, cfg, msg); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if capturedReq.URL.String() != "https://api.telegram.org/botsecret_bot_token/sendMessage" {
		t.Errorf("endpoint mismatch: %s", capturedReq.URL.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("invalid json payload: %v", err)
	}
	if payload["chat_id"] != "12345678" {
		t.Errorf("chat_id mismatch: %v", payload["chat_id"])
	}
	text, _ := payload["text"].(string)
	if !strings.Contains(text, "Backup Failed") || !strings.Contains(text, "Restic snapshot returned error") {
		t.Errorf("text content mismatch: %s", text)
	}
}

func TestDiscordDispatch(t *testing.T) {
	var capturedReq *http.Request
	var capturedBody []byte

	client := &mockClient{
		fn: func(req *http.Request) (*http.Response, error) {
			capturedReq = req
			capturedBody, _ = io.ReadAll(req.Body)
			return &http.Response{
				StatusCode: 204,
				Body:       io.NopCloser(bytes.NewReader(nil)),
			}, nil
		},
	}

	d := &Dispatcher{Client: client}
	cfg := Settings{
		Provider: ProviderDiscord,
		Target:   "https://discord.com/api/webhooks/1234/abcd",
		Events:   "disk.warning",
	}
	msg := Message{
		Event:     "disk.warning",
		Title:     "Disk Warning",
		Body:      "Disk usage at 89%",
		Level:     "warn",
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	if err := d.Dispatch(ctx, cfg, msg); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if capturedReq.URL.String() != "https://discord.com/api/webhooks/1234/abcd" {
		t.Errorf("url mismatch: %s", capturedReq.URL.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("invalid discord json payload: %v", err)
	}
	embeds, ok := payload["embeds"].([]any)
	if !ok || len(embeds) == 0 {
		t.Fatalf("missing embeds in discord payload: %+v", payload)
	}
	embed := embeds[0].(map[string]any)
	if embed["title"] != "Disk Warning" || embed["description"] != "Disk usage at 89%" {
		t.Errorf("embed mismatch: %+v", embed)
	}
}

func TestRetryBackoff(t *testing.T) {
	attempts := 0
	client := &mockClient{
		fn: func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts < 3 {
				return &http.Response{
					StatusCode: 500,
					Body:       io.NopCloser(bytes.NewReader([]byte("Internal Server Error"))),
				}, nil
			}
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader([]byte("ok"))),
			}, nil
		},
	}

	d := &Dispatcher{Client: client}
	cfg := Settings{
		Provider: ProviderNtfy,
		Target:   "retry-test",
		Events:   "test",
	}
	msg := Message{
		Event: "test",
		Title: "Test Retry",
		Body:  "Checking backoff",
	}

	ctx := context.Background()
	if err := d.Dispatch(ctx, cfg, msg); err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}
