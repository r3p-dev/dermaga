// Package notify handles dispatching alerts to ntfy, Telegram, and Discord.
package notify

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/secrets"
)

type Provider string

const (
	ProviderDisabled Provider = ""
	ProviderNtfy     Provider = "ntfy"
	ProviderTelegram Provider = "telegram"
	ProviderDiscord  Provider = "discord"
)

// Settings stores user configuration for alert delivery.
type Settings struct {
	Provider Provider `json:"provider"`
	Target   string   `json:"target"` // URL, topic, or chat_id
	Token    string   `json:"token,omitempty"`
	Events   string   `json:"events"` // Comma-separated: "deploy.failed,backup.failed,disk.warning"
}

// Message is an individual alert notification to dispatch.
type Message struct {
	Event     string    `json:"event"` // e.g. "deploy.failed", "backup.failed", "disk.warning", "test"
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Level     string    `json:"level"` // "info", "warn", "error"
	Timestamp time.Time `json:"timestamp"`
}

// HTTPClient represents the minimal interface required for dispatching requests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Dispatcher manages outbound notification delivery.
type Dispatcher struct {
	Client     HTTPClient
	DefaultBox *secrets.Box
}

// NewDispatcher creates a notification dispatcher with a default HTTP client.
func NewDispatcher(box *secrets.Box) *Dispatcher {
	return &Dispatcher{
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
		DefaultBox: box,
	}
}

// LoadSettings reads notification configuration from the SQLite settings table.
func LoadSettings(ctx context.Context, db *sql.DB, box *secrets.Box) (Settings, error) {
	var s Settings
	var encToken string
	err := db.QueryRowContext(ctx, `
		SELECT notify_provider, notify_target, notify_token, notify_events
		FROM settings WHERE id = 1
	`).Scan(&s.Provider, &s.Target, &encToken, &s.Events)
	if err != nil {
		return s, err
	}

	if encToken != "" && box != nil {
		dec, err := box.Open(encToken, "settings.notify_token")
		if err == nil {
			s.Token = dec
		}
	}

	if s.Events == "" {
		s.Events = "deploy.failed,backup.failed,disk.warning"
	}
	return s, nil
}

// SaveSettings stores encrypted notification configuration to the SQLite settings table.
func SaveSettings(ctx context.Context, db *sql.DB, box *secrets.Box, s Settings) error {
	var encToken string
	if s.Token != "" && box != nil {
		encToken = box.Seal(s.Token, "settings.notify_token")
	}

	_, err := db.ExecContext(ctx, `
		UPDATE settings
		SET notify_provider = ?, notify_target = ?, notify_token = ?, notify_events = ?
		WHERE id = 1
	`, string(s.Provider), s.Target, encToken, s.Events)
	return err
}

// IsEventEnabled checks if a particular event string is active in settings.
func (s Settings) IsEventEnabled(event string) bool {
	if event == "test" {
		return true
	}
	if s.Events == "*" {
		return true
	}
	parts := strings.Split(s.Events, ",")
	for _, p := range parts {
		if strings.TrimSpace(p) == event {
			return true
		}
	}
	return false
}

// Dispatch sends a message to the target channel with backoff retry.
func (d *Dispatcher) Dispatch(ctx context.Context, cfg Settings, msg Message) error {
	if cfg.Provider == ProviderDisabled || cfg.Provider == "" {
		return nil
	}

	if !cfg.IsEventEnabled(msg.Event) {
		return nil
	}

	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	req, err := d.buildRequest(ctx, cfg, msg)
	if err != nil {
		return err
	}

	// Retry loop with exponential backoff
	backoffs := []time.Duration{0, 1 * time.Second, 2 * time.Second}
	var lastErr error

	for i, delay := range backoffs {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		// Rebuild request on retry because request body might be consumed
		if i > 0 {
			req, err = d.buildRequest(ctx, cfg, msg)
			if err != nil {
				return err
			}
		}

		resp, err := d.Client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		// If 4xx client error (except 429 rate limit), do not retry
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return lastErr
		}
	}

	return fmt.Errorf("notification dispatch failed after retries: %w", lastErr)
}

func (d *Dispatcher) buildRequest(ctx context.Context, cfg Settings, msg Message) (*http.Request, error) {
	switch cfg.Provider {
	case ProviderNtfy:
		target := cfg.Target
		if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
			target = "https://ntfy.sh/" + target
		}

		req, err := http.NewRequestWithContext(ctx, "POST", target, strings.NewReader(msg.Body))
		if err != nil {
			return nil, err
		}
		if msg.Title != "" {
			req.Header.Set("Title", msg.Title)
		}
		priority := "default"
		tags := "bell"
		switch msg.Level {
		case "error":
			priority = "high"
			tags = "rotating_light,warning"
		case "warn":
			priority = "default"
			tags = "warning"
		case "info":
			priority = "low"
			tags = "information_source"
		}
		req.Header.Set("Priority", priority)
		req.Header.Set("Tags", tags)
		if cfg.Token != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
		}
		return req, nil

	case ProviderTelegram:
		if cfg.Token == "" {
			return nil, errors.New("telegram bot token is required")
		}
		if cfg.Target == "" {
			return nil, errors.New("telegram chat_id is required")
		}

		endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.Token)
		emoji := "ℹ️"
		switch msg.Level {
		case "error":
			emoji = "🚨"
		case "warn":
			emoji = "⚠️"
		}

		text := fmt.Sprintf("%s *%s*\n\n%s\n\n_Event: %s • %s_",
			emoji, escapeMarkdown(msg.Title), escapeMarkdown(msg.Body),
			escapeMarkdown(msg.Event), msg.Timestamp.Format("2006-01-02 15:04:05 UTC"))

		payload := map[string]any{
			"chat_id":    cfg.Target,
			"text":       text,
			"parse_mode": "Markdown",
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil

	case ProviderDiscord:
		if cfg.Target == "" {
			return nil, errors.New("discord webhook URL is required")
		}

		color := 0x3498DB // Blue info
		switch msg.Level {
		case "error":
			color = 0xE74C3C // Red
		case "warn":
			color = 0xF39C12 // Amber
		}

		payload := map[string]any{
			"username": "Dermaga",
			"embeds": []map[string]any{
				{
					"title":       msg.Title,
					"description": msg.Body,
					"color":       color,
					"timestamp":   msg.Timestamp.Format(time.RFC3339),
					"footer": map[string]string{
						"text": fmt.Sprintf("Event: %s", msg.Event),
					},
				},
			},
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", cfg.Target, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil

	default:
		return nil, fmt.Errorf("unsupported notification provider: %s", cfg.Provider)
	}
}

func escapeMarkdown(s string) string {
	// Simple markdown escaping for Telegram
	r := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"`", "\\`",
	)
	return r.Replace(s)
}
