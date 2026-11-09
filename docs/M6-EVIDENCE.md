# Milestone 6 Evidence: Backups, Restores, & Disaster Recovery

**Date:** 2026-11-09  
**Host:** `dermaga-vm` (Debian GNU/Linux 13 trixie, Linux 6.12.63+deb13-amd64)  
**Binary:** `/usr/local/bin/dermaga` built from Milestone 6 implementation  
**Restic Engine:** restic 0.18.0 compiled with go1.24.0 on linux/amd64  

---

## 1. Pre-flight & Test Suite Verification

### 1.1 `make check` (Linter, Vulncheck, Tests)

```console
$ make check
go vet ./...
go tool staticcheck ./...
go tool govulncheck ./...
=== Symbol Results ===

No vulnerabilities found.

Your code is affected by 0 vulnerabilities.
This scan also found 0 vulnerabilities in packages you import and 1
vulnerability in modules you require, but your code doesn't appear to call these
vulnerabilities.
Use '-show verbose' for more details.
go test ./...
?   	github.com/r3p-dev/dermaga/cmd/dermaga	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/api	1.002s
ok  	github.com/r3p-dev/dermaga/internal/apps	(cached)
?   	github.com/r3p-dev/dermaga/internal/audit	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/auth	(cached)
ok  	github.com/r3p-dev/dermaga/internal/backup	0.056s
ok  	github.com/r3p-dev/dermaga/internal/cli	0.009s
ok  	github.com/r3p-dev/dermaga/internal/config	(cached)
ok  	github.com/r3p-dev/dermaga/internal/daemon	(cached)
ok  	github.com/r3p-dev/dermaga/internal/databases	(cached)
ok  	github.com/r3p-dev/dermaga/internal/deploy	(cached)
ok  	github.com/r3p-dev/dermaga/internal/gitsrc	(cached)
?   	github.com/r3p-dev/dermaga/internal/golden	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/host	(cached)
?   	github.com/r3p-dev/dermaga/internal/name	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/probe	(cached)
ok  	github.com/r3p-dev/dermaga/internal/quadlet	(cached)
ok  	github.com/r3p-dev/dermaga/internal/reconcile	(cached)
ok  	github.com/r3p-dev/dermaga/internal/relay	(cached)
ok  	github.com/r3p-dev/dermaga/internal/secrets	(cached)
ok  	github.com/r3p-dev/dermaga/internal/spec	(cached)
ok  	github.com/r3p-dev/dermaga/internal/store	(cached)
ok  	github.com/r3p-dev/dermaga/internal/traefik	(cached)
ok  	github.com/r3p-dev/dermaga/internal/webhook	(cached)
```

### 1.2 Race Detector Suite (`go test -count=1 -race ./...`)

```console
$ go test -count=1 -race ./...
?   	github.com/r3p-dev/dermaga/cmd/dermaga	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/api	4.340s
ok  	github.com/r3p-dev/dermaga/internal/apps	1.557s
?   	github.com/r3p-dev/dermaga/internal/audit	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/auth	3.778s
ok  	github.com/r3p-dev/dermaga/internal/backup	2.066s
ok  	github.com/r3p-dev/dermaga/internal/cli	1.038s
ok  	github.com/r3p-dev/dermaga/internal/config	1.020s
ok  	github.com/r3p-dev/dermaga/internal/daemon	1.033s
ok  	github.com/r3p-dev/dermaga/internal/databases	1.411s
ok  	github.com/r3p-dev/dermaga/internal/deploy	5.835s
ok  	github.com/r3p-dev/dermaga/internal/gitsrc	1.093s
?   	github.com/r3p-dev/dermaga/internal/golden	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/host	1.018s
?   	github.com/r3p-dev/dermaga/internal/name	[no test files]
ok  	github.com/r3p-dev/dermaga/internal/probe	1.661s
ok  	github.com/r3p-dev/dermaga/internal/quadlet	1.079s
ok  	github.com/r3p-dev/dermaga/internal/reconcile	1.184s
ok  	github.com/r3p-dev/dermaga/internal/relay	1.022s
ok  	github.com/r3p-dev/dermaga/internal/secrets	1.015s
ok  	github.com/r3p-dev/dermaga/internal/spec	1.023s
ok  	github.com/r3p-dev/dermaga/internal/store	1.355s
ok  	github.com/r3p-dev/dermaga/internal/traefik	1.070s
ok  	github.com/r3p-dev/dermaga/internal/webhook	1.057s
```

### 1.3 Backup Unit Test Suite (`internal/backup`)

```console
$ go test -v ./internal/backup
=== RUN   TestSettingsGenerationAndEncryption
--- PASS: TestSettingsGenerationAndEncryption (0.01s)
=== RUN   TestDumpCommandsD10
--- PASS: TestDumpCommandsD10 (0.01s)
=== RUN   TestSystemBackupExcludesMasterKey
--- PASS: TestSystemBackupExcludesMasterKey (0.01s)
=== RUN   TestTimerGenerationAndOrchestration
--- PASS: TestTimerGenerationAndOrchestration (0.01s)
=== RUN   TestKeysExport
--- PASS: TestKeysExport (0.01s)
=== RUN   TestRestoreDatabaseSafetyDump
--- PASS: TestRestoreDatabaseSafetyDump (0.01s)
PASS
ok  	github.com/r3p-dev/dermaga/internal/backup	0.047s
```

---

## 2. Daemon Resource & Memory Budget (< 30 MB)

```console
$ systemctl --user status dermaga --no-pager
● dermaga.service - dermaga daemon (single-user PaaS)
     Loaded: loaded (/home/server/.config/systemd/user/dermaga.service; enabled; preset: enabled)
     Active: active (running) since Mon 2026-11-09 01:17:30 UTC; 1min 45s ago
 Invocation: fe560c2ad96d4cf9b00916998722aa6d
       Docs: https://github.com/r3p-dev/dermaga
   Main PID: 23440 (dermaga)
      Tasks: 8 (limit: 4676)
     Memory: 27.7M (peak: 91.4M)
        CPU: 4.534s
     CGroup: /user.slice/user-1001.slice/user@1001.service/app.slice/dermaga.service
             └─23440 /usr/local/bin/dermaga daemon
```
*Result:* Steady-state memory footprint is 27.7 MB, safely below the 30 MB operational budget.

---

## 3. Automated Systemd User Timer Orchestration

### 3.1 Active Timers on `dermaga-vm`

During active database hosting, systemd user timers are created per database and for system backups:

```console
$ systemctl --user list-timers --no-pager
NEXT                          LEFT LAST                              PASSED UNIT                           ACTIVATES
Mon 2026-11-09 19:26:13 UTC    18h -                                      - dg-backup-system.timer         dg-backup-system.service
Mon 2026-11-09 20:14:05 UTC    18h -                                      - dg-backup-db-maria-test.timer  dg-backup-db-maria-test.service
Mon 2026-11-09 20:20:51 UTC    19h -                                      - dg-backup-db-valkey-test.timer dg-backup-db-valkey-test.service
Mon 2026-11-09 20:27:43 UTC    19h -                                      - dg-backup-db-pg-test.timer     dg-backup-db-pg-test.service
Tue 2026-11-10 00:14:20 UTC    22h Mon 2026-11-09 00:02:29 UTC 1h 16min ago podman-auto-update.timer       podman-auto-update.service
Mon 2026-11-16 00:19:35 UTC 6 days Mon 2026-11-09 00:03:25 UTC 1h 15min ago vps-prune.timer                vps-prune.service

6 timers listed.
```

### 3.2 Unit Properties & Failure Alerts

Generated timer units configure `Persistent=true`, `RandomizedDelaySec=1800`, and `OnFailure=dg-backup-failed@%n.service`:

```ini
[Unit]
Description=dermaga backup timer for db pg-test

[Timer]
OnCalendar=*-*-* 03:00:00 Asia/Jakarta
Persistent=true
RandomizedDelaySec=1800

[Install]
WantedBy=timers.target
```

```ini
[Unit]
Description=dermaga backup runner for db pg-test
OnFailure=dg-backup-failed@%n.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/dermaga backup run db:pg-test
```

---

## 4. Master Key Exclusion & Keys Export

### 4.1 Master Key Export

```console
$ dermaga keys export
MASTER SECRET KEY (HEX):
b25e71887e0b5de9f2e3be665efb9a67fe97148eb8d58c89a05cb8f1e5693ff2

INSTRUCTIONS:
Store this 32-byte key in a secure offline password manager (1Password, Bitwarden, Vault).
This key decrypts all database credentials, TOTP secrets, and environment variables stored
in dermaga's SQLite database. It is NEVER included in backups.
If you rebuild this server, place this key at ~/.config/dermaga/master.key (mode 0600)
BEFORE starting dermaga to restore access to your encrypted data.
```

### 4.2 Verification of System Backup Tarball Contents

```console
$ dermaga backup system
System backed up successfully (snapshot f6a59c4b, 141.5 KiB)
```

Inspecting the tarball snapshot via `restic dump` confirms that `master.key` is strictly excluded:
- Files included: `dermaga.db`, `config.toml`, `env/`, `deploy-keys/`, `known_hosts`, `traefik/`
- `master.key`: Excluded (verified absent).

---

## 5. Streaming Database Backups (ADR D10)

Creating databases and executing on-demand streaming snapshots:

```console
$ dermaga db create pg-test --engine postgres --version 18
Created database pg-test (postgres:18).

$ dermaga db create maria-test --engine mariadb --version 11.8
Created database maria-test (mariadb:11.8).

$ dermaga db create valkey-test --engine valkey --version 9
Created database valkey-test (valkey:9).
```

Taking non-buffering streaming dumps piped directly to restic stdin:

```console
$ dermaga db backup pg-test
Database pg-test backed up successfully (snapshot 7222b665, 2.7 KiB)

$ dermaga db backup maria-test
Database maria-test backed up successfully (snapshot c5c0ff0a, 2.2 KiB)

$ dermaga db backup valkey-test
Database valkey-test backed up successfully (snapshot 55f241ad, 145 B)
```

---

## 6. Sandboxed Test Restores (`dermaga db restore-test`)

Each engine was validated in an isolated throwaway sandbox container on a temporary volume with automated sanity verification queries (BRIEF §3.8):

### 6.1 PostgreSQL 18
```console
$ dermaga db restore-test pg-test
Testing restore for database pg-test in isolated sandbox...
Test restore PASSED (snapshot 7222b665, took 3.474s, sanity check: 1)
```

### 6.2 MariaDB 11.8
```console
$ dermaga db restore-test maria-test
Testing restore for database maria-test in isolated sandbox...
Test restore PASSED (snapshot c5c0ff0a, took 48.128s, sanity check: 1)
```

### 6.3 Valkey 9
```console
$ dermaga db restore-test valkey-test
Testing restore for database valkey-test in isolated sandbox...
Test restore PASSED (snapshot 55f241ad, took 2.732s, sanity check: PONG)
```

---

## 7. On-Demand Database Restores & Safety Snapshots

Testing manual on-demand restore workflow with automatic pre-flight safety snapshots:

### 7.1 PostgreSQL Restore
```console
# Seed extra uncommitted record
$ podman exec dg-db-pg-test psql -U postgres -d pg-test -c "INSERT INTO users (id, name, email) VALUES (3, 'eve', 'eve@dermaga.local');"
INSERT 0 1

# Restore from snapshot
$ dermaga db restore pg-test --snapshot 7222b665 -y
Database pg-test restored successfully

# Verified recovered data
$ podman exec dg-db-pg-test psql -U postgres -d pg-test -c "SELECT * FROM users;"
 id | name  |        email        
----+-------+---------------------
  1 | alice | alice@dermaga.local
  2 | bob   | bob@dermaga.local
(2 rows)
```

### 7.2 MariaDB Restore
```console
# Seed extra uncommitted record
$ podman exec dg-db-maria-test mariadb -u maria-test -p2672a9446145c795b024c983945d09a8ee0ee5ee7c6ea3817c5a3901d196f5da maria-test -e "INSERT INTO orders (customer, total) VALUES ('eve', 300.00);"

# Restore from snapshot
$ dermaga db restore maria-test --snapshot c5c0ff0a -y
Database maria-test restored successfully

# Verified recovered data
$ podman exec dg-db-maria-test mariadb -u maria-test -p2672a9446145c795b024c983945d09a8ee0ee5ee7c6ea3817c5a3901d196f5da maria-test -e "SELECT * FROM orders;"
id	customer	total
1	charlie	99.95
2	david	149.50
```

### 7.3 Valkey Restore
```console
# Seed transient key
$ podman exec dg-db-valkey-test sh -c 'export VALKEYCLI_AUTH="$VALKEY_PASSWORD"; valkey-cli --no-auth-warning set temp:foo bar'
OK

# Restore from snapshot
$ dermaga db restore valkey-test --snapshot 55f241ad -y
Database valkey-test restored successfully

# Verified recovered keys
$ podman exec dg-db-valkey-test sh -c 'export VALKEYCLI_AUTH="$VALKEY_PASSWORD"; valkey-cli --no-auth-warning get temp:foo; valkey-cli --no-auth-warning mget session:1001 config:theme'

user_alice_active
dark
```

---

## 8. Simulated Disaster Recovery Drill (Catastrophic Volume Wipe)

### Step 1: Pre-disaster Verification
```console
$ podman exec dg-db-pg-test psql -U postgres -d pg-test -c 'SELECT * FROM users;'
 id | name  |        email        
----+-------+---------------------
  1 | alice | alice@dermaga.local
  2 | bob   | bob@dermaga.local
(2 rows)
```

### Step 2: Simulated Catastrophic Wipe
```console
$ systemctl --user stop dg-db-pg-test.service
$ podman run --rm -v dg-db_pg-test:/var/lib/postgresql alpine sh -c 'rm -rf /var/lib/postgresql/* /var/lib/postgresql/.* 2>/dev/null; ls -la /var/lib/postgresql'
total 8
drwxrwxrwt    2 999      ping          4096 Nov  9 01:17 .
drwxr-xr-x    1 root     root          4096 Nov  9 01:17 ..
```

### Step 3: Service Startup with Empty Volume
```console
$ systemctl --user start dg-db-pg-test.service
```

### Step 4: Verification of Data Loss (Disaster Confirmed)
```console
$ podman exec dg-db-pg-test psql -U postgres -d pg-test -c 'SELECT * FROM users;'
ERROR:  relation "users" does not exist
LINE 1: SELECT * FROM users;
                      ^
```

### Step 5: Disaster Recovery Execution
```console
$ dermaga db restore pg-test --snapshot 7222b665 -y
Database pg-test restored successfully
```

### Step 6: Post-Recovery Integrity Verification (100% Restored)
```console
$ podman exec dg-db-pg-test psql -U postgres -d pg-test -c 'SELECT * FROM users;'
 id | name  |        email        
----+-------+---------------------
  1 | alice | alice@dermaga.local
  2 | bob   | bob@dermaga.local
(2 rows)
```
*Result:* Disaster recovery drill passed with 100% data recovery from restic snapshot.

---

## 9. Failure Alerting & Audit Logging

Triggering `dg-backup-failed@dg-backup-db-pg-test.service`:

```console
$ systemctl --user start dg-backup-failed@dg-backup-db-pg-test.service
$ dermaga audit --limit 7
TIME                 ACTOR  ACTION           TARGET                DETAILS
2026-11-09 01:18:17  timer  backup.failed    dg-backup-db-pg-test  systemd failure event triggered
2026-11-09 01:18:06  cli    db.restore       pg-test               snapshot=7222b665 into=
2026-11-09 01:16:23  cli    db.restore       valkey-test           snapshot=55f241ad into=
2026-11-09 01:16:10  cli    db.restore       maria-test            snapshot=c5c0ff0a into=
2026-11-09 01:15:43  cli    db.restore       pg-test               snapshot=7222b665 into=
2026-11-09 01:15:24  cli    db.restore_test  valkey-test           snapshot=55f241ad duration=2.732s success=true
2026-11-09 01:15:18  cli    db.restore_test  maria-test            snapshot=c5c0ff0a duration=48.128s success=true
```

---

## 10. Repository Integrity Verification

```console
$ dermaga backup check
Running restic repository integrity check...
Repository check passed: no errors found.
```

---

## 11. Acceptance Checklist Summary

- [x] Restic repository initialized with cryptographically random 32-byte password, sealed in SQLite.
- [x] Streaming non-buffering database dumps implemented per ADR D10 (`pg_dump -Fc`, `mariadb-dump`, Valkey `BGSAVE` + RDB cat).
- [x] Sandboxed test restores (`dermaga db restore-test`) verified passing for PostgreSQL 18, MariaDB 11.8, and Valkey 9.
- [x] Automated systemd user timers configured with `RandomizedDelaySec=1800`, `Persistent=true`, and `OnFailure=dg-backup-failed@%n.service`.
- [x] Failure alerts caught by `dg-backup-failed@.service` and recorded in persistent `audit_log`.
- [x] System backup (`dermaga backup system`) atomically captures SQLite (`VACUUM INTO`), configs, and deploy keys while strictly excluding `master.key`.
- [x] `dermaga keys export` outputs master key with operational instructions.
- [x] Full disaster recovery drill executed: volume wiped, corruption confirmed, restored cleanly, 100% data recovered.
- [x] All unit, race detector, and linter tests pass (`make check` and `go test -count=1 -race ./...`).
