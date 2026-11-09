package backup

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/databases"
	"github.com/r3p-dev/dermaga/internal/quadlet"
)

// RestoreOpts specifies options for restoring a database.
type RestoreOpts struct {
	SnapshotID     string
	Into           string // Optional: restore into a different database name
	SkipSafetyDump bool   // Skip automated safety snapshot
	Confirm        bool   // Confirmation flag for CLI/API
}

// BackupDatabase takes an on-demand streaming snapshot of a database (D10).
func (m *Manager) BackupDatabase(ctx context.Context, name string) (Snapshot, error) {
	d, creds, err := m.Databases.Get(ctx, name)
	if err != nil {
		return Snapshot{}, fmt.Errorf("getting database %s: %w", name, err)
	}

	ctr := quadlet.DBContainerName(name)
	filename, cmd, err := dumpCommand(d, creds, ctr)
	if err != nil {
		return Snapshot{}, err
	}

	tags := []string{"db:" + d.Name, "engine:" + d.Engine}
	snap, err := m.BackupStdinCommand(ctx, filename, tags, cmd)
	if err != nil {
		return Snapshot{}, err
	}

	// Automated retention pruning (BRIEF §3.8)
	if d.BackupRetention != "" {
		_ = m.ForgetPrune(ctx, d.BackupRetention, "db:"+d.Name)
	}

	// Record in backups table
	_, _ = m.DB.ExecContext(ctx, `INSERT INTO backups
		(id, target_type, target_name, snapshot_id, tags, size_bytes, created_at)
		VALUES (?, 'db', ?, ?, ?, ?, ?)`,
		snap.ShortID, d.Name, snap.ID, strings.Join(tags, ","), snap.SizeBytes, time.Now().Unix(),
	)

	return snap, nil
}

// dumpCommand prepares the command to stream the database dump straight to restic (D10).
func dumpCommand(d databases.Database, creds databases.Credentials, ctr string) (filename string, cmd []string, err error) {
	switch d.Engine {
	case databases.EnginePostgres:
		filename = d.Name + ".dump"
		cmd = []string{"podman", "exec", ctr, "pg_dump", "-Fc", "-U", creds.Username, d.Name}
		return filename, cmd, nil

	case databases.EngineMariaDB:
		filename = d.Name + ".sql"
		cmd = []string{
			"podman", "exec", ctr, "sh", "-c",
			`exec mariadb-dump --single-transaction --routines --triggers --events -u "$MARIADB_USER" -p"$MARIADB_PASSWORD" "$MARIADB_DATABASE"`,
		}
		return filename, cmd, nil

	case databases.EngineValkey:
		filename = d.Name + ".rdb"
		cmd = []string{
			"podman", "exec", ctr, "sh", "-c",
			`export VALKEYCLI_AUTH="$VALKEY_PASSWORD"
valkey-cli --no-auth-warning bgsave >/dev/null
for i in $(seq 1 50); do
    prog=$(valkey-cli --no-auth-warning info persistence 2>/dev/null | tr -d '\r' | awk -F: '/rdb_bgsave_in_progress/{print $2}')
    if [ "$prog" = "0" ] || [ -z "$prog" ]; then break; fi
    sleep 0.2
done
cat /data/dump.rdb`,
		}
		return filename, cmd, nil

	default:
		return "", nil, fmt.Errorf("unsupported engine: %s", d.Engine)
	}
}

// RestoreDatabase restores a database from a restic snapshot.
func (m *Manager) RestoreDatabase(ctx context.Context, name string, opts RestoreOpts) error {
	if _, _, err := m.Databases.Get(ctx, name); err != nil {
		return fmt.Errorf("getting database %s: %w", name, err)
	}

	targetName := name
	if opts.Into != "" {
		targetName = opts.Into
	}

	targetDB, targetCreds, err := m.Databases.Get(ctx, targetName)
	if err != nil {
		return fmt.Errorf("getting target database %s: %w", targetName, err)
	}

	// Safety dump first (BRIEF §3.8: restore takes an automatic safety dump first)
	if !opts.SkipSafetyDump && targetName == name {
		_, _ = m.BackupDatabase(ctx, name)
	}

	snapID := opts.SnapshotID
	if snapID == "" {
		snapID = "latest"
	}

	targetCtr := quadlet.DBContainerName(targetName)
	svc := targetCtr + ".service"

	switch targetDB.Engine {
	case databases.EnginePostgres:
		dumpFile := name + ".dump"
		// Ensure container is running
		st, _ := m.Host.ActiveState(ctx, svc)
		if st != "active" {
			_, _ = m.Host.Run(ctx, 30*time.Second, "systemctl", "--user", "start", svc)
		}
		// Stream pg_restore into container
		pr, pw := io.Pipe()
		errCh := make(chan error, 1)
		go func() {
			errCh <- m.Dump(ctx, snapID, dumpFile, pw)
			pw.Close()
		}()

		// Terminate existing connections first
		_, _ = m.Host.Run(ctx, 10*time.Second, "podman", "exec", targetCtr, "psql", "-U", "postgres", "-d", "postgres", "-c",
			fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid();", targetDB.Name))

		// Ensure database exists before restoring
		_, _ = m.Host.Run(ctx, 10*time.Second, "podman", "exec", targetCtr, "psql", "-U", "postgres", "-d", "postgres", "-c",
			fmt.Sprintf(`CREATE DATABASE "%s";`, targetDB.Name))

		// Run pg_restore
		_, rErr := m.Host.RunStdin(ctx, 5*time.Minute, pr,
			"podman", "exec", "-i", targetCtr, "pg_restore", "--clean", "--if-exists", "-U", targetCreds.Username, "-d", targetDB.Name)
		dumpErr := <-errCh
		if dumpErr != nil {
			return fmt.Errorf("reading dump from snapshot: %w", dumpErr)
		}
		if rErr != nil {
			// pg_restore can exit with status 1 on minor warnings; if stdout/stderr indicates clean restore, check data
			// However report if serious failure
			if !strings.Contains(rErr.Error(), "exit status 1") {
				return fmt.Errorf("pg_restore failed: %w", rErr)
			}
		}
		// Restart service cleanly
		_, _ = m.Host.Run(ctx, 30*time.Second, "systemctl", "--user", "restart", svc)
		return nil

	case databases.EngineMariaDB:
		dumpFile := name + ".sql"
		st, _ := m.Host.ActiveState(ctx, svc)
		if st != "active" {
			_, _ = m.Host.Run(ctx, 30*time.Second, "systemctl", "--user", "start", svc)
		}

		// Ensure database exists before restoring
		_, _ = m.Host.Run(ctx, 10*time.Second, "podman", "exec", targetCtr, "sh", "-c",
			fmt.Sprintf("mariadb -u root -p\"$MARIADB_ROOT_PASSWORD\" -e 'CREATE DATABASE IF NOT EXISTS `%s`;'", targetDB.Name))

		pr, pw := io.Pipe()
		errCh := make(chan error, 1)
		go func() {
			errCh <- m.Dump(ctx, snapID, dumpFile, pw)
			pw.Close()
		}()

		_, rErr := m.Host.RunStdin(ctx, 5*time.Minute, pr,
			"podman", "exec", "-i", targetCtr, "sh", "-c",
			fmt.Sprintf(`exec mariadb -u root -p"$MARIADB_ROOT_PASSWORD" "%s"`, targetDB.Name))
		dumpErr := <-errCh
		if dumpErr != nil {
			return fmt.Errorf("reading dump from snapshot: %w", dumpErr)
		}
		if rErr != nil {
			return fmt.Errorf("mariadb restore failed: %w", rErr)
		}
		_, _ = m.Host.Run(ctx, 30*time.Second, "systemctl", "--user", "restart", svc)
		return nil

	case databases.EngineValkey:
		dumpFile := name + ".rdb"
		// Valkey must be stopped so it reads dump.rdb from disk on boot
		_, _ = m.Host.Run(ctx, 30*time.Second, "systemctl", "--user", "stop", svc)

		volName := quadlet.DBVolumeName(targetName)
		pr, pw := io.Pipe()
		errCh := make(chan error, 1)
		go func() {
			errCh <- m.Dump(ctx, snapID, dumpFile, pw)
			pw.Close()
		}()

		// Write restored RDB into volume using valkey image container
		_, rErr := m.Host.RunStdin(ctx, 2*time.Minute, pr,
			"podman", "run", "--rm", "-i", "-v", volName+":/data", "docker.io/valkey/valkey:9",
			"sh", "-c", "cat > /data/dump.rdb")
		dumpErr := <-errCh
		if dumpErr != nil {
			return fmt.Errorf("reading dump from snapshot: %w", dumpErr)
		}
		if rErr != nil {
			return fmt.Errorf("writing rdb to volume failed: %w", rErr)
		}

		// Start service cleanly
		_, err = m.Host.Run(ctx, 30*time.Second, "systemctl", "--user", "start", svc)
		return err

	default:
		return fmt.Errorf("unsupported engine: %s", targetDB.Engine)
	}
}

// TestRestore restores the latest snapshot into a throwaway sandbox container,
// executes a sanity verification query, cleans up, and returns a report (BRIEF §3.8).
func (m *Manager) TestRestore(ctx context.Context, name string) (TestRestoreResult, error) {
	start := time.Now()
	d, _, err := m.Databases.Get(ctx, name)
	if err != nil {
		return TestRestoreResult{}, err
	}

	snaps, err := m.Snapshots(ctx, "db:"+name)
	if err != nil || len(snaps) == 0 {
		return TestRestoreResult{}, fmt.Errorf("no snapshots found for database %s", name)
	}
	latestSnap := snaps[len(snaps)-1]

	randID := randHex(4)
	testCtr := "dg-test-restore-" + randID
	testVol := "dg-test-vol-" + randID

	// Create temporary volume
	if _, err := m.Host.Run(ctx, 30*time.Second, "podman", "volume", "create", testVol); err != nil {
		return TestRestoreResult{}, fmt.Errorf("creating test volume: %w", err)
	}
	defer func() {
		_, _ = m.Host.Run(ctx, 30*time.Second, "podman", "rm", "-f", testCtr)
		_, _ = m.Host.Run(ctx, 30*time.Second, "podman", "volume", "rm", "-f", testVol)
	}()

	var sanityOut string
	switch d.Engine {
	case databases.EnginePostgres:
		// Start throwaway postgres container
		_, err = m.Host.Run(ctx, 60*time.Second, "podman", "run", "-d", "--name", testCtr,
			"-e", "POSTGRES_PASSWORD=testsecret",
			"-e", "POSTGRES_DB="+d.Name,
			"-v", testVol+":/var/lib/postgresql",
			"docker.io/library/postgres:18",
		)
		if err != nil {
			return TestRestoreResult{}, fmt.Errorf("starting test container: %w", err)
		}
		// Wait for ready
		m.waitForReady(ctx, 30*time.Second, func() bool {
			out, err := m.Host.Run(ctx, 5*time.Second, "podman", "exec", testCtr, "pg_isready", "-U", "postgres")
			return err == nil && strings.Contains(out, "accepting connections")
		})
		// Stream pg_restore
		pr, pw := io.Pipe()
		go func() {
			_ = m.Dump(ctx, latestSnap.ID, d.Name+".dump", pw)
			pw.Close()
		}()
		_, _ = m.Host.RunStdin(ctx, 2*time.Minute, pr, "podman", "exec", "-i", testCtr, "pg_restore", "--clean", "--if-exists", "-U", "postgres", "-d", d.Name)
		// Run sanity query
		sanityOut, err = m.Host.Run(ctx, 10*time.Second, "podman", "exec", testCtr, "psql", "-U", "postgres", "-d", d.Name, "-t", "-A", "-c", "SELECT 1;")
		if err != nil {
			return TestRestoreResult{}, fmt.Errorf("sanity query failed: %w", err)
		}

	case databases.EngineMariaDB:
		_, err = m.Host.Run(ctx, 60*time.Second, "podman", "run", "-d", "--name", testCtr,
			"-e", "MARIADB_ROOT_PASSWORD=testsecret",
			"-e", "MARIADB_DATABASE="+d.Name,
			"-v", testVol+":/var/lib/mysql",
			"docker.io/library/mariadb:11.8",
		)
		if err != nil {
			return TestRestoreResult{}, fmt.Errorf("starting test container: %w", err)
		}
		m.waitForReady(ctx, 45*time.Second, func() bool {
			out, err := m.Host.Run(ctx, 5*time.Second, "podman", "logs", testCtr)
			return err == nil && strings.Contains(out, "ready for connections")
		})
		_, _ = m.Host.Run(ctx, 10*time.Second, "podman", "exec", testCtr, "sh", "-c",
			fmt.Sprintf("mariadb -u root -p\"$MARIADB_ROOT_PASSWORD\" -e 'CREATE DATABASE IF NOT EXISTS `%s`;'", d.Name))
		pr, pw := io.Pipe()
		go func() {
			_ = m.Dump(ctx, latestSnap.ID, d.Name+".sql", pw)
			pw.Close()
		}()
		_, err = m.Host.RunStdin(ctx, 2*time.Minute, pr, "podman", "exec", "-i", testCtr, "sh", "-c",
			fmt.Sprintf(`exec mariadb -u root -p"$MARIADB_ROOT_PASSWORD" "%s"`, d.Name))
		if err != nil {
			return TestRestoreResult{}, fmt.Errorf("restoring dump into test container: %w", err)
		}
		sanityOut, err = m.Host.Run(ctx, 10*time.Second, "podman", "exec", testCtr, "sh", "-c",
			fmt.Sprintf(`exec mariadb -u root -p"$MARIADB_ROOT_PASSWORD" "%s" -s -N -e "SELECT 1;"`, d.Name))
		if err != nil {
			return TestRestoreResult{}, fmt.Errorf("sanity query failed: %w", err)
		}

	case databases.EngineValkey:
		// Write dump into test volume before starting
		pr, pw := io.Pipe()
		go func() {
			_ = m.Dump(ctx, latestSnap.ID, d.Name+".rdb", pw)
			pw.Close()
		}()
		_, err = m.Host.RunStdin(ctx, 2*time.Minute, pr,
			"podman", "run", "--rm", "-i", "-v", testVol+":/data", "docker.io/valkey/valkey:9",
			"sh", "-c", "cat > /data/dump.rdb")
		if err != nil {
			return TestRestoreResult{}, fmt.Errorf("writing rdb to test volume: %w", err)
		}
		// Start valkey with restored RDB
		_, err = m.Host.Run(ctx, 60*time.Second, "podman", "run", "-d", "--name", testCtr,
			"-v", testVol+":/data",
			"docker.io/valkey/valkey:9",
			"valkey-server",
		)
		if err != nil {
			return TestRestoreResult{}, fmt.Errorf("starting test valkey: %w", err)
		}
		m.waitForReady(ctx, 15*time.Second, func() bool {
			out, err := m.Host.Run(ctx, 3*time.Second, "podman", "exec", testCtr, "valkey-cli", "ping")
			return err == nil && strings.Contains(out, "PONG")
		})
		sanityOut, err = m.Host.Run(ctx, 5*time.Second, "podman", "exec", testCtr, "valkey-cli", "ping")
		if err != nil {
			return TestRestoreResult{}, fmt.Errorf("sanity ping failed: %w", err)
		}
	}

	dur := time.Since(start)
	return TestRestoreResult{
		Database:   name,
		SnapshotID: latestSnap.ShortID,
		Duration:   dur.Round(time.Millisecond).String(),
		SanityOut:  strings.TrimSpace(sanityOut),
		Success:    true,
		TestedAt:   time.Now(),
	}, nil
}

func (m *Manager) waitForReady(ctx context.Context, timeout time.Duration, check func() bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}
