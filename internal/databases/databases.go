// Package databases implements database management, credentials, Quadlet specs,
// and app network/env linking for PostgreSQL, MariaDB, and Valkey (BRIEF §3.7).
package databases

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/quadlet"
	"github.com/r3p-dev/dermaga/internal/secrets"
	"github.com/r3p-dev/dermaga/internal/spec"
)

var (
	ErrNotFound       = errors.New("no such database")
	ErrUnsupported    = errors.New("unsupported database engine")
	ErrAlreadyExists  = errors.New("database already exists")
	ErrAppStillLinked = errors.New("cannot remove database while apps are linked")
)

const (
	EnginePostgres = "postgres"
	EngineMariaDB  = "mariadb"
	EngineValkey   = "valkey"

	DefaultPostgresVersion = "18"
	DefaultMariaDBVersion  = "11.8"
	DefaultValkeyVersion   = "9"
)

type EngineConfig struct {
	DefaultVersion string
	DefaultImage   string
	DefaultPort    int
	VolumePath     string
	Caps           []string
	Exec           string
}

// Engines holds the pinned engine configurations and minimal verified capabilities.
var Engines = map[string]EngineConfig{
	EnginePostgres: {
		DefaultVersion: DefaultPostgresVersion,
		DefaultImage:   "docker.io/library/postgres:18",
		DefaultPort:    5432,
		VolumePath:     "/var/lib/postgresql",
		Caps:           []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "SETGID", "SETUID", "KILL"},
	},
	EngineMariaDB: {
		DefaultVersion: DefaultMariaDBVersion,
		DefaultImage:   "docker.io/library/mariadb:11.8",
		DefaultPort:    3306,
		VolumePath:     "/var/lib/mysql",
		Caps:           []string{"CHOWN", "DAC_OVERRIDE", "SETGID", "SETUID", "KILL"},
	},
	EngineValkey: {
		DefaultVersion: DefaultValkeyVersion,
		DefaultImage:   "docker.io/valkey/valkey:9",
		DefaultPort:    6379,
		VolumePath:     "/data",
		Caps:           []string{"SETGID", "SETUID", "KILL"},
		Exec:           `/bin/sh -c 'exec valkey-server --requirepass "$VALKEY_PASSWORD"'`,
	},
}

// Database represents a managed database instance.
type Database struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Engine          string    `json:"engine"`
	Version         string    `json:"version"`
	Volume          string    `json:"volume"`
	BackupSchedule  string    `json:"backupSchedule"`
	BackupRetention string    `json:"backupRetention"`
	CreatedAt       time.Time `json:"createdAt"`
	Status          string    `json:"status,omitempty"`
	LinkedApps      []string  `json:"linkedApps,omitempty"`
}

// Credentials holds unencrypted database credentials.
type Credentials struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password"`
	Database string `json:"database,omitempty"`
}

// DBLink represents an app linked to a database.
type DBLink struct {
	DatabaseID string            `json:"databaseId"`
	AppID      string            `json:"appId"`
	EnvNames   map[string]string `json:"envNames"`
	CreatedAt  time.Time         `json:"createdAt"`
}

// Store manages database records in SQLite.
type Store struct {
	DB  *sql.DB
	Box *secrets.Box
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ImageFor returns the fully qualified image reference for an engine and version.
func ImageFor(engine, version string) (string, error) {
	cfg, ok := Engines[engine]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnsupported, engine)
	}
	v := version
	if v == "" {
		v = cfg.DefaultVersion
	}
	switch engine {
	case EnginePostgres:
		return "docker.io/library/postgres:" + v, nil
	case EngineMariaDB:
		return "docker.io/library/mariadb:" + v, nil
	case EngineValkey:
		return "docker.io/valkey/valkey:" + v, nil
	}
	return "", fmt.Errorf("%w: %s", ErrUnsupported, engine)
}

// Create stores a new database record with generated random credentials.
func (s Store) Create(ctx context.Context, name, engine, version string) (Database, Credentials, error) {
	if !spec.ValidName(name) {
		return Database{}, Credentials{}, fmt.Errorf("database name %q is invalid", name)
	}
	cfg, ok := Engines[engine]
	if !ok {
		return Database{}, Credentials{}, fmt.Errorf("%w: %s", ErrUnsupported, engine)
	}
	if version == "" {
		version = cfg.DefaultVersion
	}

	id := randHex(8)
	// 32 random bytes = 64 hex characters (>= 32 bytes requirement)
	pass := randHex(32)

	creds := Credentials{Password: pass}
	switch engine {
	case EnginePostgres:
		creds.Username = "postgres"
		creds.Database = name
	case EngineMariaDB:
		creds.Username = name
		creds.Database = name
	}

	b, err := json.Marshal(creds)
	if err != nil {
		return Database{}, Credentials{}, err
	}
	encCreds := s.Box.Seal(string(b), "databases.credentials")

	volName := strings.TrimSuffix(quadlet.DBVolumeUnit(name), ".volume")
	now := time.Now()

	d := Database{
		ID:              id,
		Name:            name,
		Engine:          engine,
		Version:         version,
		Volume:          volName,
		BackupSchedule:  "03:00",
		BackupRetention: "--keep-daily 7 --keep-weekly 4 --keep-monthly 6",
		CreatedAt:       now,
	}

	_, err = s.DB.ExecContext(ctx, `INSERT INTO databases
		(id, name, engine, version, volume, credentials, backup_schedule, backup_retention, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.Name, d.Engine, d.Version, d.Volume, encCreds, d.BackupSchedule, d.BackupRetention, now.Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Database{}, Credentials{}, fmt.Errorf("%w: %s", ErrAlreadyExists, name)
		}
		return Database{}, Credentials{}, err
	}
	return d, creds, nil
}

// Get finds a database by name and decrypts its credentials.
func (s Store) Get(ctx context.Context, name string) (Database, Credentials, error) {
	var d Database
	var encCreds string
	var created int64
	err := s.DB.QueryRowContext(ctx, `SELECT id, name, engine, version, volume, credentials, backup_schedule, backup_retention, created_at
		FROM databases WHERE name = ?`, name).Scan(
		&d.ID, &d.Name, &d.Engine, &d.Version, &d.Volume, &encCreds, &d.BackupSchedule, &d.BackupRetention, &created,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Database{}, Credentials{}, ErrNotFound
	}
	if err != nil {
		return Database{}, Credentials{}, err
	}
	d.CreatedAt = time.Unix(created, 0)

	raw, err := s.Box.Open(encCreds, "databases.credentials")
	if err != nil {
		return Database{}, Credentials{}, fmt.Errorf("decrypting credentials for %s: %w", name, err)
	}
	var creds Credentials
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return Database{}, Credentials{}, fmt.Errorf("parsing credentials for %s: %w", name, err)
	}
	apps, err := s.GetLinkedApps(ctx, d.ID)
	if err != nil {
		return Database{}, Credentials{}, err
	}
	d.LinkedApps = apps
	return d, creds, nil
}

// GetByID finds a database by its ID and decrypts its credentials.
func (s Store) GetByID(ctx context.Context, id string) (Database, Credentials, error) {
	var name string
	err := s.DB.QueryRowContext(ctx, `SELECT name FROM databases WHERE id = ?`, id).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return Database{}, Credentials{}, ErrNotFound
	}
	if err != nil {
		return Database{}, Credentials{}, err
	}
	return s.Get(ctx, name)
}

// List returns all databases.
func (s Store) List(ctx context.Context) ([]Database, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, engine, version, volume, backup_schedule, backup_retention, created_at
		FROM databases ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	var out []Database
	for rows.Next() {
		var d Database
		var created int64
		if err := rows.Scan(&d.ID, &d.Name, &d.Engine, &d.Version, &d.Volume, &d.BackupSchedule, &d.BackupRetention, &created); err != nil {
			rows.Close()
			return nil, err
		}
		d.CreatedAt = time.Unix(created, 0)
		out = append(out, d)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		apps, err := s.GetLinkedApps(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].LinkedApps = apps
	}
	return out, nil
}

// Delete removes a database record from SQLite if no apps are linked.
func (s Store) Delete(ctx context.Context, name string, force bool) error {
	d, _, err := s.Get(ctx, name)
	if err != nil {
		return err
	}
	if len(d.LinkedApps) > 0 && !force {
		return fmt.Errorf("%w (%s linked to %v)", ErrAppStillLinked, name, d.LinkedApps)
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM databases WHERE id = ?`, d.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Link links a database to an app.
func (s Store) Link(ctx context.Context, dbName, appID string, envNames map[string]string) (DBLink, error) {
	d, _, err := s.Get(ctx, dbName)
	if err != nil {
		return DBLink{}, err
	}
	if envNames == nil {
		envNames = map[string]string{}
	}
	js, err := json.Marshal(envNames)
	if err != nil {
		return DBLink{}, err
	}
	now := time.Now()
	_, err = s.DB.ExecContext(ctx, `INSERT INTO db_links (database_id, app_id, env_names, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(database_id, app_id) DO UPDATE SET env_names = excluded.env_names`,
		d.ID, appID, string(js), now.Unix())
	if err != nil {
		return DBLink{}, err
	}
	return DBLink{
		DatabaseID: d.ID,
		AppID:      appID,
		EnvNames:   envNames,
		CreatedAt:  now,
	}, nil
}

// Unlink unlinks a database from an app.
func (s Store) Unlink(ctx context.Context, dbName, appID string) error {
	d, _, err := s.Get(ctx, dbName)
	if err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM db_links WHERE database_id = ? AND app_id = ?`, d.ID, appID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("link between %s and app does not exist", dbName)
	}
	return nil
}

// GetLinks returns all links for a given database ID.
func (s Store) GetLinks(ctx context.Context, dbID string) ([]DBLink, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT database_id, app_id, env_names, created_at FROM db_links WHERE database_id = ?`, dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DBLink
	for rows.Next() {
		var l DBLink
		var js string
		var created int64
		if err := rows.Scan(&l.DatabaseID, &l.AppID, &js, &created); err != nil {
			return nil, err
		}
		l.CreatedAt = time.Unix(created, 0)
		l.EnvNames = map[string]string{}
		_ = json.Unmarshal([]byte(js), &l.EnvNames)
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetLinkedApps returns the names of apps linked to a database ID.
func (s Store) GetLinkedApps(ctx context.Context, dbID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.name FROM db_links l JOIN apps a ON l.app_id = a.id WHERE l.database_id = ? ORDER BY a.name ASC`, dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// GetDatabaseNamesForApp returns database names linked to an app ID.
func (s Store) GetDatabaseNamesForApp(ctx context.Context, appID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT d.name FROM db_links l JOIN databases d ON l.database_id = d.id WHERE l.app_id = ? ORDER BY d.name ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// GetLinksForApp returns all DBLink records for an app ID.
func (s Store) GetLinksForApp(ctx context.Context, appID string) ([]DBLink, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT database_id, app_id, env_names, created_at FROM db_links WHERE app_id = ?`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DBLink
	for rows.Next() {
		var l DBLink
		var js string
		var created int64
		if err := rows.Scan(&l.DatabaseID, &l.AppID, &js, &created); err != nil {
			return nil, err
		}
		l.CreatedAt = time.Unix(created, 0)
		l.EnvNames = map[string]string{}
		_ = json.Unmarshal([]byte(js), &l.EnvNames)
		out = append(out, l)
	}
	return out, rows.Err()
}

// InjectedEnv calculates environment variables to inject into an app linked to a database,
// applying any custom name overrides defined in link.EnvNames.
// ConnectionString returns the canonical connection URL for the database (internal container hostname).
func ConnectionString(d Database, creds Credentials) string {
	cname := quadlet.DBContainerName(d.Name)
	switch d.Engine {
	case EnginePostgres:
		return fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable", creds.Username, creds.Password, cname, d.Name)
	case EngineMariaDB:
		return fmt.Sprintf("mysql://%s:%s@%s:3306/%s", creds.Username, creds.Password, cname, d.Name)
	case EngineValkey:
		return fmt.Sprintf("redis://:%s@%s:6379/0", creds.Password, cname)
	default:
		return ""
	}
}

func InjectedEnv(d Database, creds Credentials, link DBLink) map[string]string {
	cname := quadlet.DBContainerName(d.Name)
	raw := map[string]string{}

	switch d.Engine {
	case EnginePostgres:
		raw["DATABASE_URL"] = ConnectionString(d, creds)
		raw["PGHOST"] = cname
		raw["PGPORT"] = "5432"
		raw["PGDATABASE"] = d.Name
		raw["PGUSER"] = creds.Username
		raw["PGPASSWORD"] = creds.Password
	case EngineMariaDB:
		raw["DATABASE_URL"] = ConnectionString(d, creds)
		raw["MYSQL_HOST"] = cname
		raw["MYSQL_PORT"] = "3306"
		raw["MYSQL_DATABASE"] = d.Name
		raw["MYSQL_USER"] = creds.Username
		raw["MYSQL_PASSWORD"] = creds.Password
	case EngineValkey:
		raw["REDIS_URL"] = ConnectionString(d, creds)
		raw["VALKEY_URL"] = fmt.Sprintf("valkey://:%s@%s:6379/0", creds.Password, cname)
		raw["REDIS_HOST"] = cname
		raw["REDIS_PORT"] = "6379"
		raw["REDIS_PASSWORD"] = creds.Password
	}

	out := make(map[string]string, len(raw))
	for k, v := range raw {
		targetKey := k
		if custom, ok := link.EnvNames[k]; ok && custom != "" {
			targetKey = custom
		}
		out[targetKey] = v
	}
	return out
}

// DBEnvFile renders the environment file contents for the database container itself.
func DBEnvFile(d Database, creds Credentials) []byte {
	m := map[string]string{}
	switch d.Engine {
	case EnginePostgres:
		m["POSTGRES_DB"] = d.Name
		m["POSTGRES_USER"] = creds.Username
		m["POSTGRES_PASSWORD"] = creds.Password
	case EngineMariaDB:
		m["MARIADB_DATABASE"] = d.Name
		m["MARIADB_USER"] = creds.Username
		m["MARIADB_PASSWORD"] = creds.Password
		m["MARIADB_ROOT_PASSWORD"] = creds.Password
	case EngineValkey:
		m["VALKEY_PASSWORD"] = creds.Password
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + m[k] + "\n")
	}
	return []byte(b.String())
}

// SSHTunnelSnippet returns instructions and the SSH tunnel snippet for connecting
// a desktop database client to the database without exposing any ports.
func SSHTunnelSnippet(d Database, creds Credentials, host, sshPort, user string) string {
	port := 5432
	switch d.Engine {
	case EngineMariaDB:
		port = 3306
	case EngineValkey:
		port = 6379
	}
	if sshPort == "" {
		sshPort = "22"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("To connect a desktop DB client to %s (%s):\n\n", d.Name, d.Engine))
	sb.WriteString("Option 1: Desktop Client GUI SSH Tunnel (DBeaver, TablePlus, DataGrip, pgAdmin):\n")
	sb.WriteString("  - SSH Host: " + host + "\n")
	sb.WriteString(fmt.Sprintf("  - SSH Port: %s\n", sshPort))
	sb.WriteString("  - SSH User: " + user + "\n")
	sb.WriteString("  - Database Host: 127.0.0.1 (once tunnel is running)\n")
	sb.WriteString(fmt.Sprintf("  - Database Port: %d\n", port))
	if creds.Username != "" {
		sb.WriteString("  - User: " + creds.Username + "\n")
	}
	sb.WriteString("  - Password: " + creds.Password + "\n")
	if creds.Database != "" {
		sb.WriteString("  - Database: " + creds.Database + "\n")
	}
	sb.WriteString("\nOption 2: Terminal SSH port-forward tunnel:\n")
	sb.WriteString(fmt.Sprintf("  ssh -N -L %d:127.0.0.1:%d %s@%s \"dermaga db proxy %s\"\n\n", port, port, user, host, d.Name))
	sb.WriteString("Then connect your client locally to 127.0.0.1:" + fmt.Sprintf("%d", port) + ".\n")
	return sb.String()
}
