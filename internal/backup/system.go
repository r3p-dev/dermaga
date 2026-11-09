package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/host"
)

// BackupSystem creates a consistent archive of the control plane state (BRIEF §3.8).
// Crucially, it EXCLUDES master.key, which must be kept offline via 'keys export'.
func (m *Manager) BackupSystem(ctx context.Context) (Snapshot, error) {
	// 1. Consistent SQLite snapshot via VACUUM INTO
	tempDir, err := os.MkdirTemp("", "dermaga-system-backup-*")
	if err != nil {
		return Snapshot{}, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	snapDBPath := filepath.Join(tempDir, "dermaga.db")
	_, err = m.DB.ExecContext(ctx, "VACUUM INTO ?", snapDBPath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("sqlite VACUUM INTO: %w", err)
	}

	// 2. Build tar archive in memory or streaming buffer
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)

	// Helper to add a file
	addFile := func(archivePath, diskPath string) error {
		// Explicitly exclude master.key
		if filepath.Base(diskPath) == "master.key" || filepath.Base(archivePath) == "master.key" {
			return nil
		}
		data, err := os.ReadFile(diskPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		fi, err := os.Stat(diskPath)
		if err != nil {
			return err
		}
		hdr := &tar.Header{
			Name:    archivePath,
			Mode:    int64(fi.Mode().Perm()),
			Size:    int64(len(data)),
			ModTime: fi.ModTime(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	}

	// Helper to add a directory
	addDir := func(prefix, diskDir string) error {
		entries, err := os.ReadDir(diskDir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if e.Name() == "master.key" {
				continue
			}
			diskPath := filepath.Join(diskDir, e.Name())
			archivePath := filepath.Join(prefix, e.Name())
			if err := addFile(archivePath, diskPath); err != nil {
				return err
			}
		}
		return nil
	}

	// Add database snapshot
	if err := addFile("dermaga.db", snapDBPath); err != nil {
		return Snapshot{}, err
	}

	// Add config.toml
	if err := addFile("config.toml", m.Paths.ConfigFile()); err != nil {
		return Snapshot{}, err
	}

	// Add env files
	if err := addDir("env", filepath.Join(m.Paths.ConfigDir, "env")); err != nil {
		return Snapshot{}, err
	}

	// Add deploy keys
	if err := addDir("deploy-keys", filepath.Join(m.Paths.DataDir, "deploy-keys")); err != nil {
		return Snapshot{}, err
	}

	// Add known_hosts
	if err := addFile("known_hosts", m.Paths.KnownHosts()); err != nil {
		return Snapshot{}, err
	}

	// Add Traefik dynamic and static configs
	if err := addDir("traefik/dynamic", m.Paths.DynamicDir()); err != nil {
		return Snapshot{}, err
	}
	if err := addFile("traefik/traefik.yml", filepath.Join(m.Paths.TraefikDir, "traefik.yml")); err != nil {
		return Snapshot{}, err
	}

	if err := tw.Close(); err != nil {
		return Snapshot{}, fmt.Errorf("closing tar writer: %w", err)
	}

	// 3. Stream tarball into restic
	tags := []string{"system", "type:system"}
	snap, err := m.BackupStdin(ctx, "system.tar", tags, bytes.NewReader(tarBuf.Bytes()))
	if err != nil {
		return Snapshot{}, fmt.Errorf("restic backup system: %w", err)
	}

	// Automated retention pruning for system backups (keep daily 7, weekly 4, monthly 6)
	_ = m.ForgetPrune(ctx, "--keep-daily 7 --keep-weekly 4 --keep-monthly 6", "system")

	// Record in backups table
	_, _ = m.DB.ExecContext(ctx, `INSERT INTO backups
		(id, target_type, target_name, snapshot_id, tags, size_bytes, created_at)
		VALUES (?, 'system', 'system', ?, ?, ?, ?)`,
		snap.ShortID, snap.ID, strings.Join(tags, ","), int64(tarBuf.Len()), time.Now().Unix(),
	)

	return snap, nil
}

// RestoreSystem extracts and restores system configuration, databases, deploy keys,
// and Traefik configuration from a system snapshot (BRIEF §3.8).
func (m *Manager) RestoreSystem(ctx context.Context, snapshotID string) error {
	if snapshotID == "" {
		snapshotID = "latest"
	}

	var buf bytes.Buffer
	if err := m.Dump(ctx, snapshotID, "system.tar", &buf); err != nil {
		return fmt.Errorf("extracting system.tar from snapshot %s: %w", snapshotID, err)
	}

	tr := tar.NewReader(&buf)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar header: %w", err)
		}

		// Security: NEVER allow restoring master.key from an archive
		if filepath.Base(hdr.Name) == "master.key" {
			continue
		}

		var targetPath string
		perm := os.FileMode(hdr.Mode)
		if perm == 0 {
			perm = 0o600
		}

		switch {
		case hdr.Name == "dermaga.db":
			// Overwrite database
			targetPath = m.Paths.DB()
		case hdr.Name == "config.toml":
			targetPath = m.Paths.ConfigFile()
		case hdr.Name == "known_hosts":
			targetPath = m.Paths.KnownHosts()
		case strings.HasPrefix(hdr.Name, "env/"):
			targetPath = filepath.Join(m.Paths.ConfigDir, hdr.Name)
		case strings.HasPrefix(hdr.Name, "deploy-keys/"):
			targetPath = filepath.Join(m.Paths.DataDir, hdr.Name)
		case hdr.Name == "traefik/traefik.yml":
			targetPath = filepath.Join(m.Paths.TraefikDir, "traefik.yml")
		case strings.HasPrefix(hdr.Name, "traefik/dynamic/"):
			rel := strings.TrimPrefix(hdr.Name, "traefik/dynamic/")
			targetPath = filepath.Join(m.Paths.DynamicDir(), rel)
		default:
			// Unrecognized file in system archive; skip
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
			return fmt.Errorf("creating dir for %s: %w", targetPath, err)
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return fmt.Errorf("reading entry %s: %w", hdr.Name, err)
		}

		if err := host.WriteFile(targetPath, data, perm); err != nil {
			return fmt.Errorf("writing file %s: %w", targetPath, err)
		}
	}

	return nil
}

// ExportKeys retrieves the 32-byte master secret encryption key and formats
// offline disaster recovery storage instructions.
func (m *Manager) ExportKeys() (hexKey string, instructions string, err error) {
	keyPath := m.Paths.MasterKey()
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return "", "", fmt.Errorf("reading master key from %s: %w", keyPath, err)
	}
	if len(data) != 32 {
		return "", "", fmt.Errorf("invalid master key length (%d bytes, want 32)", len(data))
	}
	hexKey = hex.EncodeToString(data)

	var sb strings.Builder
	sb.WriteString("========================================================================\n")
	sb.WriteString("                       DERMAGA MASTER KEY EXPORT                        \n")
	sb.WriteString("========================================================================\n\n")
	sb.WriteString("MASTER KEY (HEX): " + hexKey + "\n\n")
	sb.WriteString("CRITICAL SECURITY INSTRUCTIONS:\n")
	sb.WriteString("1. This key encrypts all database passwords, environment variables, and\n")
	sb.WriteString("   secrets in your Dermaga SQLite database.\n")
	sb.WriteString("2. Dermaga system backups EXCLUDE this key by design.\n")
	sb.WriteString("3. Store this hex string in a secure offline password manager (1Password,\n")
	sb.WriteString("   Bitwarden, KeePass) or encrypted physical storage.\n")
	sb.WriteString("4. To restore on a fresh server during Disaster Recovery:\n")
	sb.WriteString("   - Write this 32-byte key back to: " + keyPath + "\n")
	sb.WriteString("   - Ensure permissions: chmod 0600 " + keyPath + "\n")
	sb.WriteString("   - Then run: dermaga restore-system\n\n")
	sb.WriteString("========================================================================\n")

	return hexKey, sb.String(), nil
}
