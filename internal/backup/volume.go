package backup

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/r3p-dev/dermaga/internal/quadlet"
)

// BackupVolume streams a named app volume into a restic snapshot (BRIEF §3.8).
func (m *Manager) BackupVolume(ctx context.Context, appName, volName string) (Snapshot, error) {
	podmanVol := quadlet.AppVolumeName(appName, volName)
	filename := volName + ".tar"
	tags := []string{"volume:" + appName + ":" + volName, "app:" + appName}

	cmd := []string{"podman", "volume", "export", podmanVol}
	snap, err := m.BackupStdinCommand(ctx, filename, tags, cmd)
	if err != nil {
		return Snapshot{}, fmt.Errorf("backing up volume %s for app %s: %w", volName, appName, err)
	}

	_ = m.ForgetPrune(ctx, "--keep-daily 7 --keep-weekly 4 --keep-monthly 6", "volume:"+appName+":"+volName)
	return snap, nil
}

// RestoreVolume restores a named app volume from a restic snapshot.
func (m *Manager) RestoreVolume(ctx context.Context, appName, volName, snapshotID string) error {
	podmanVol := quadlet.AppVolumeName(appName, volName)
	filename := volName + ".tar"

	if snapshotID == "" {
		snapshotID = "latest"
	}

	// Ensure volume exists
	_, _ = m.Host.Run(ctx, 15*time.Second, "podman", "volume", "create", podmanVol)

	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		errCh <- m.Dump(ctx, snapshotID, filename, pw)
		pw.Close()
	}()

	_, rErr := m.Host.RunStdin(ctx, 5*time.Minute, pr, "podman", "volume", "import", podmanVol, "-")
	dumpErr := <-errCh
	if dumpErr != nil {
		return fmt.Errorf("reading volume dump from snapshot %s: %w", snapshotID, dumpErr)
	}
	if rErr != nil {
		return fmt.Errorf("podman volume import failed: %w", rErr)
	}
	return nil
}
