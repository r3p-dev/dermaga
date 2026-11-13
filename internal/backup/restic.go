package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type resticSummary struct {
	MessageType string `json:"message_type"`
	SnapshotID  string `json:"snapshot_id"`
	TotalBytes  int64  `json:"total_bytes_processed"`
}

// EnsureRepo checks if the restic repository exists and initializes it if needed.
func (m *Manager) EnsureRepo(ctx context.Context) error {
	env, err := m.ResticEnv(ctx)
	if err != nil {
		return err
	}
	// Try snapshots or cat config
	_, err = m.Host.RunRestic(ctx, env, nil, nil, "snapshots", "--json")
	if err == nil {
		return nil
	}
	// Repository not initialized; run init
	_, err = m.Host.RunRestic(ctx, env, nil, nil, "init")
	if err != nil {
		return fmt.Errorf("initializing restic repository: %w", err)
	}
	return nil
}

// Snapshots lists all snapshots, optionally filtered by tag.
func (m *Manager) Snapshots(ctx context.Context, tag string) ([]Snapshot, error) {
	if err := m.EnsureRepo(ctx); err != nil {
		return nil, err
	}
	env, err := m.ResticEnv(ctx)
	if err != nil {
		return nil, err
	}
	argv := []string{"snapshots", "--json"}
	if tag != "" {
		argv = append(argv, "--tag", tag)
	}
	out, err := m.Host.RunRestic(ctx, env, nil, nil, argv...)
	if err != nil {
		return nil, err
	}
	var snaps []Snapshot
	if err := json.Unmarshal([]byte(out), &snaps); err != nil {
		return nil, fmt.Errorf("parsing snapshots json: %w", err)
	}
	return snaps, nil
}

// BackupStdinCommand runs restic backup --stdin-from-command (D10 streaming dump).
func (m *Manager) BackupStdinCommand(ctx context.Context, filename string, tags []string, cmd []string) (Snapshot, error) {
	if err := m.EnsureRepo(ctx); err != nil {
		return Snapshot{}, err
	}
	env, err := m.ResticEnv(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	argv := []string{"backup", "--json", "--stdin-filename", filename}
	for _, t := range tags {
		argv = append(argv, "--tag", t)
	}
	argv = append(argv, "--stdin-from-command", "--")
	argv = append(argv, cmd...)

	out, err := m.Host.RunRestic(ctx, env, nil, nil, argv...)
	if err != nil {
		return Snapshot{}, fmt.Errorf("streaming backup failed: %w", err)
	}

	snapID, totalBytes := parseResticSummary(out)
	if snapID == "" {
		// Fallback: look up latest snapshot with tag
		if len(tags) > 0 {
			snaps, err := m.Snapshots(ctx, tags[0])
			if err == nil && len(snaps) > 0 {
				return snaps[len(snaps)-1], nil
			}
		}
		snapID = "latest"
	}

	shortID := snapID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	return Snapshot{
		ID:        snapID,
		ShortID:   shortID,
		Time:      time.Now(),
		Paths:     []string{"/" + filename},
		Tags:      tags,
		SizeBytes: totalBytes,
	}, nil
}

// BackupStdin streams an io.Reader into restic via --stdin.
func (m *Manager) BackupStdin(ctx context.Context, filename string, tags []string, r io.Reader) (Snapshot, error) {
	if err := m.EnsureRepo(ctx); err != nil {
		return Snapshot{}, err
	}
	env, err := m.ResticEnv(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	argv := []string{"backup", "--json", "--stdin", "--stdin-filename", filename}
	for _, t := range tags {
		argv = append(argv, "--tag", t)
	}

	out, err := m.Host.RunRestic(ctx, env, r, nil, argv...)
	if err != nil {
		return Snapshot{}, fmt.Errorf("stdin backup failed: %w", err)
	}

	snapID, totalBytes := parseResticSummary(out)
	if snapID == "" {
		if len(tags) > 0 {
			snaps, err := m.Snapshots(ctx, tags[0])
			if err == nil && len(snaps) > 0 {
				return snaps[len(snaps)-1], nil
			}
		}
		snapID = "latest"
	}

	shortID := snapID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	return Snapshot{
		ID:        snapID,
		ShortID:   shortID,
		Time:      time.Now(),
		Paths:     []string{"/" + filename},
		Tags:      tags,
		SizeBytes: totalBytes,
	}, nil
}

// Dump extracts a specific file from a snapshot to an io.Writer.
func (m *Manager) Dump(ctx context.Context, snapshotID, filename string, out io.Writer) error {
	env, err := m.ResticEnv(ctx)
	if err != nil {
		return err
	}
	if snapshotID == "" {
		snapshotID = "latest"
	}
	_, err = m.Host.RunRestic(ctx, env, nil, out, "dump", "-q", snapshotID, filename)
	return err
}

// ForgetPrune applies retention flags and prunes old snapshots.
func (m *Manager) ForgetPrune(ctx context.Context, retention string, tag string) error {
	env, err := m.ResticEnv(ctx)
	if err != nil {
		return err
	}
	argv := []string{"forget", "--prune"}
	if tag != "" {
		argv = append(argv, "--tag", tag)
	}
	if retention != "" {
		argv = append(argv, strings.Fields(retention)...)
	} else {
		argv = append(argv, "--keep-daily", "7", "--keep-weekly", "4", "--keep-monthly", "6")
	}
	_, err = m.Host.RunRestic(ctx, env, nil, nil, argv...)
	return err
}

// Check verifies the restic repository integrity.
// It returns what restic printed, so callers can show the real result.
func (m *Manager) Check(ctx context.Context, readDataSubset string) (string, error) {
	env, err := m.ResticEnv(ctx)
	if err != nil {
		return "", err
	}
	argv := []string{"check"}
	if readDataSubset != "" {
		argv = append(argv, "--read-data-subset="+readDataSubset)
	}
	return m.Host.RunRestic(ctx, env, nil, nil, argv...)
}

func parseResticSummary(output string) (snapID string, totalBytes int64) {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var s resticSummary
		if err := json.Unmarshal([]byte(line), &s); err == nil && s.MessageType == "summary" {
			return s.SnapshotID, s.TotalBytes
		}
	}
	return "", 0
}
