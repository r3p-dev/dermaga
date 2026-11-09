package databases

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r3p-dev/dermaga/internal/secrets"
	"github.com/r3p-dev/dermaga/internal/store"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := secrets.New(make([]byte, 32))
	if err != nil {
		t.Fatalf("secrets.New: %v", err)
	}
	return Store{DB: db, Box: box}
}

func TestDatabaseLifecycleAndCredentials(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	// Invalid name
	if _, _, err := st.Create(ctx, "Invalid_Name!", EnginePostgres, ""); err == nil {
		t.Error("expected error for invalid name")
	}

	// Unsupported engine
	if _, _, err := st.Create(ctx, "mydb", "oracle", ""); err == nil {
		t.Error("expected error for unsupported engine")
	}

	// Create Postgres
	pg, creds, err := st.Create(ctx, "pgdb", EnginePostgres, "18")
	if err != nil {
		t.Fatalf("creating pg: %v", err)
	}
	if pg.Name != "pgdb" || pg.Engine != EnginePostgres || pg.Version != "18" {
		t.Errorf("unexpected pg: %+v", pg)
	}
	if len(creds.Password) < 32 {
		t.Errorf("password length %d < 32", len(creds.Password))
	}
	if creds.Username != "postgres" || creds.Database != "pgdb" {
		t.Errorf("unexpected creds: %+v", creds)
	}

	// Duplicate create fails
	if _, _, err := st.Create(ctx, "pgdb", EnginePostgres, "18"); err == nil {
		t.Error("expected error on duplicate create")
	}

	// Get & GetByID
	got, gotCreds, err := st.Get(ctx, "pgdb")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != pg.ID || gotCreds.Password != creds.Password {
		t.Errorf("got != pg: %+v, %+v", got, gotCreds)
	}
	byID, _, err := st.GetByID(ctx, pg.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if byID.Name != "pgdb" {
		t.Errorf("expected pgdb, got %s", byID.Name)
	}

	// Create MariaDB & Valkey
	maria, mCreds, err := st.Create(ctx, "mariadb", EngineMariaDB, "")
	if err != nil {
		t.Fatalf("creating mariadb: %v", err)
	}
	if maria.Version != DefaultMariaDBVersion {
		t.Errorf("expected default version %s, got %s", DefaultMariaDBVersion, maria.Version)
	}
	if mCreds.Username != "mariadb" {
		t.Errorf("expected username mariadb, got %s", mCreds.Username)
	}

	valkey, vCreds, err := st.Create(ctx, "cache", EngineValkey, "")
	if err != nil {
		t.Fatalf("creating valkey: %v", err)
	}
	if valkey.Version != DefaultValkeyVersion {
		t.Errorf("expected default version %s, got %s", DefaultValkeyVersion, valkey.Version)
	}
	if len(vCreds.Password) < 32 {
		t.Errorf("valkey pass length < 32")
	}

	// List
	list, err := st.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 databases, got %d", len(list))
	}
}

func TestDatabaseLinkingAndInjectedEnv(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	// Create an app in apps table first
	_, err := st.DB.ExecContext(ctx, `INSERT INTO apps (id, name, spec, webhook_secret, created_at)
		VALUES ('app1', 'shop', '{}', 'sec', 1000)`)
	if err != nil {
		t.Fatalf("inserting app: %v", err)
	}

	pg, pgCreds, err := st.Create(ctx, "shopdb", EnginePostgres, "18")
	if err != nil {
		t.Fatalf("create pg: %v", err)
	}

	valkey, vCreds, err := st.Create(ctx, "cache", EngineValkey, "9")
	if err != nil {
		t.Fatalf("create valkey: %v", err)
	}

	// Link shopdb to app1 with default names
	link1, err := st.Link(ctx, "shopdb", "app1", nil)
	if err != nil {
		t.Fatalf("link pg: %v", err)
	}
	if link1.DatabaseID != pg.ID || link1.AppID != "app1" {
		t.Errorf("unexpected link: %+v", link1)
	}

	// Injected env for postgres
	env1 := InjectedEnv(pg, pgCreds, link1)
	if !strings.HasPrefix(env1["DATABASE_URL"], "postgres://postgres:") || !strings.Contains(env1["DATABASE_URL"], "@dg-db-shopdb:5432/shopdb") {
		t.Errorf("unexpected DATABASE_URL: %s", env1["DATABASE_URL"])
	}
	if env1["PGHOST"] != "dg-db-shopdb" || env1["PGPORT"] != "5432" || env1["PGDATABASE"] != "shopdb" {
		t.Errorf("unexpected PG vars: %+v", env1)
	}

	// Link valkey with custom env name override: REDIS_URL -> CACHE_URL
	link2, err := st.Link(ctx, "cache", "app1", map[string]string{
		"REDIS_URL": "CACHE_URL",
	})
	if err != nil {
		t.Fatalf("link valkey: %v", err)
	}

	env2 := InjectedEnv(valkey, vCreds, link2)
	if _, ok := env2["REDIS_URL"]; ok {
		t.Errorf("REDIS_URL should have been remapped to CACHE_URL")
	}
	if !strings.HasPrefix(env2["CACHE_URL"], "redis://:") || !strings.Contains(env2["CACHE_URL"], "@dg-db-cache:6379/0") {
		t.Errorf("unexpected CACHE_URL: %s", env2["CACHE_URL"])
	}
	if env2["REDIS_HOST"] != "dg-db-cache" {
		t.Errorf("unexpected REDIS_HOST: %s", env2["REDIS_HOST"])
	}

	// Check linked apps
	linkedApps, err := st.GetLinkedApps(ctx, pg.ID)
	if err != nil {
		t.Fatalf("get linked apps: %v", err)
	}
	if len(linkedApps) != 1 || linkedApps[0] != "shop" {
		t.Errorf("expected [shop], got %v", linkedApps)
	}

	dbNames, err := st.GetDatabaseNamesForApp(ctx, "app1")
	if err != nil {
		t.Fatalf("get db names: %v", err)
	}
	if len(dbNames) != 2 || dbNames[0] != "cache" || dbNames[1] != "shopdb" {
		t.Errorf("expected [cache shopdb], got %v", dbNames)
	}

	// Deleting db while linked must fail without force
	if err := st.Delete(ctx, "shopdb", false); err == nil {
		t.Error("expected error deleting linked database without force")
	}

	// Unlink
	if err := st.Unlink(ctx, "shopdb", "app1"); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	linkedAppsAfter, _ := st.GetLinkedApps(ctx, pg.ID)
	if len(linkedAppsAfter) != 0 {
		t.Errorf("expected 0 linked apps after unlink, got %v", linkedAppsAfter)
	}

	// Delete now succeeds
	if err := st.Delete(ctx, "shopdb", false); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestDBEnvFileAndSnippet(t *testing.T) {
	d := Database{Name: "mydb", Engine: EnginePostgres}
	creds := Credentials{Username: "postgres", Password: "mysecretpassword12345678901234567890", Database: "mydb"}
	b := DBEnvFile(d, creds)
	content := string(b)
	if !strings.Contains(content, "POSTGRES_DB=mydb\n") ||
		!strings.Contains(content, "POSTGRES_PASSWORD=mysecretpassword12345678901234567890\n") ||
		!strings.Contains(content, "POSTGRES_USER=postgres\n") {
		t.Errorf("unexpected env file content:\n%s", content)
	}

	snip := SSHTunnelSnippet(d, creds, "vps.example.com", "2222", "server")
	if !strings.Contains(snip, "ssh -N -L 5432:127.0.0.1:5432 server@vps.example.com \"dermaga db proxy mydb\"") {
		t.Errorf("unexpected snippet:\n%s", snip)
	}
}

func TestImageFor(t *testing.T) {
	cases := []struct {
		engine, version string
		want            string
	}{
		{EnginePostgres, "", "docker.io/library/postgres:18"},
		{EnginePostgres, "17", "docker.io/library/postgres:17"},
		{EngineMariaDB, "", "docker.io/library/mariadb:11.8"},
		{EngineValkey, "", "docker.io/valkey/valkey:9"},
	}
	for _, c := range cases {
		img, err := ImageFor(c.engine, c.version)
		if err != nil {
			t.Errorf("%s %s: %v", c.engine, c.version, err)
		}
		if img != c.want {
			t.Errorf("got %s, want %s", img, c.want)
		}
	}
}
