package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/apps"
	"github.com/r3p-dev/dermaga/internal/audit"
	"github.com/r3p-dev/dermaga/internal/backup"
	"github.com/r3p-dev/dermaga/internal/databases"
	"github.com/r3p-dev/dermaga/internal/host"
	"github.com/r3p-dev/dermaga/internal/quadlet"
)

// DatabaseView is the JSON representation of a database.
type DatabaseView struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Engine          string                 `json:"engine"`
	Version         string                 `json:"version"`
	Port            int                    `json:"port"`
	Volume          string                 `json:"volume"`
	Status          string                 `json:"status"`
	BackupSchedule  string                 `json:"backupSchedule"`
	BackupRetention string                 `json:"backupRetention"`
	CreatedAt       time.Time              `json:"createdAt"`
	LinkedApps      []string               `json:"linkedApps"`
	Credentials     *databases.Credentials `json:"credentials,omitempty"`
	URL             string                 `json:"url,omitempty"`
	Snippet         string                 `json:"snippet,omitempty"`
}

type CreateDatabaseRequest struct {
	Name    string `json:"name"`
	Engine  string `json:"engine"`
	Version string `json:"version,omitempty"`
}

type LinkDatabaseRequest struct {
	App      string            `json:"app"`
	EnvNames map[string]string `json:"env_names,omitempty"`
}

type UnlinkDatabaseRequest struct {
	App string `json:"app"`
}

// LinkDatabaseResponse answers a link or unlink: the app is redeployed when it is live.
type LinkDatabaseResponse struct {
	Database       string            `json:"database"`
	App            string            `json:"app"`
	EnvNames       map[string]string `json:"env_names,omitempty"`
	RedeployQueued bool              `json:"redeployQueued"`
}

type RestoreDatabaseResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
}

func (s *Server) databaseRoutes(mux *http.ServeMux, wrap func(http.HandlerFunc) http.Handler, actor string) {
	h := func(f func(http.ResponseWriter, *http.Request, string)) http.Handler {
		return wrap(func(w http.ResponseWriter, r *http.Request) { f(w, r, actor) })
	}
	mux.Handle("GET /api/databases", h(s.listDatabases))
	mux.Handle("POST /api/databases", h(s.createDatabase))
	mux.Handle("GET /api/databases/{name}", h(s.getDatabase))
	mux.Handle("DELETE /api/databases/{name}", h(s.deleteDatabase))
	mux.Handle("POST /api/databases/{name}/restart", h(s.restartDatabase))
	mux.Handle("POST /api/databases/{name}/link", h(s.linkDatabase))
	mux.Handle("POST /api/databases/{name}/unlink", h(s.unlinkDatabase))
	mux.Handle("POST /api/databases/{name}/backup", h(s.backupDatabase))
	mux.Handle("POST /api/databases/{name}/restore", h(s.restoreDatabase))
	mux.Handle("POST /api/databases/{name}/restore-test", h(s.testRestoreDatabase))
}

func (s *Server) dbStatus(ctx context.Context, name string) string {
	unit := quadlet.DBContainerName(name) + ".service"
	state, err := host.Host{}.ActiveState(ctx, unit)
	if err != nil {
		return "inactive"
	}
	return state
}

func (s *Server) listDatabases(w http.ResponseWriter, r *http.Request, _ string) {
	dbs, err := s.Databases.List(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	out := make([]DatabaseView, 0, len(dbs))
	for _, d := range dbs {
		st := s.dbStatus(r.Context(), d.Name)
		port := databases.Engines[d.Engine].DefaultPort
		out = append(out, DatabaseView{
			ID:              d.ID,
			Name:            d.Name,
			Engine:          d.Engine,
			Version:         d.Version,
			Port:            port,
			Volume:          d.Volume,
			Status:          st,
			BackupSchedule:  d.BackupSchedule,
			BackupRetention: d.BackupRetention,
			CreatedAt:       d.CreatedAt,
			LinkedApps:      d.LinkedApps,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createDatabase(w http.ResponseWriter, r *http.Request, actor string) {
	var req CreateDatabaseRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Name == "" || req.Engine == "" {
		writeError(w, http.StatusBadRequest, "name and engine are required")
		return
	}

	cfg, ok := databases.Engines[req.Engine]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported engine: %s", req.Engine))
		return
	}
	ver := req.Version
	if ver == "" {
		ver = cfg.DefaultVersion
	}
	img, err := databases.ImageFor(req.Engine, ver)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	db, creds, err := s.Databases.Create(r.Context(), req.Name, req.Engine, ver)
	if err != nil {
		if errors.Is(err, databases.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Generate and install Quadlet units and env file
	envFile := s.Paths.DBEnvFile(db.Name)
	envData := databases.DBEnvFile(db, creds)
	if err := os.MkdirAll(filepath.Dir(envFile), 0o700); err != nil {
		internalError(w, fmt.Errorf("creating env dir: %w", err))
		return
	}
	if err := host.WriteFile(envFile, envData, 0o600); err != nil {
		internalError(w, fmt.Errorf("writing env file: %w", err))
		return
	}

	netFn, netData, err := quadlet.Network(db.Name)
	if err != nil {
		internalError(w, fmt.Errorf("rendering network unit: %w", err))
		return
	}
	volFn, volData, err := quadlet.DBVolume(db.Name)
	if err != nil {
		internalError(w, fmt.Errorf("rendering volume unit: %w", err))
		return
	}
	ctrFn, ctrData, err := quadlet.DBContainer(quadlet.DBRelease{
		Name:       db.Name,
		Engine:     db.Engine,
		Image:      img,
		EnvFile:    envFile,
		VolumePath: cfg.VolumePath,
		Caps:       cfg.Caps,
		Exec:       cfg.Exec,
	})
	if err != nil {
		internalError(w, fmt.Errorf("rendering container unit: %w", err))
		return
	}

	files := map[string][]byte{
		netFn: netData,
		volFn: volData,
		ctrFn: ctrData,
	}

	if s.Engine != nil && s.Engine.Host != nil {
		if err := s.Engine.Host.InstallUnits(r.Context(), files); err != nil {
			internalError(w, fmt.Errorf("installing units: %w", err))
			return
		}
		unit := quadlet.DBContainerName(db.Name) + ".service"
		if err := s.Engine.Host.Start(r.Context(), unit); err != nil {
			internalError(w, fmt.Errorf("starting database service %s: %w", unit, err))
			return
		}
	}

	if s.Backup != nil {
		_ = s.Backup.InstallDBTimer(r.Context(), db)
	}

	_ = audit.Log(s.DB, actor, "db.create", db.Name, fmt.Sprintf("engine=%s version=%s", db.Engine, db.Version))

	hostName, sshPort, userName := sshTarget()
	snippet := databases.SSHTunnelSnippet(db, creds, hostName, sshPort, userName)

	port := databases.Engines[db.Engine].DefaultPort
	connURL := databases.ConnectionString(db, creds)

	view := DatabaseView{
		ID:              db.ID,
		Name:            db.Name,
		Engine:          db.Engine,
		Version:         db.Version,
		Port:            port,
		Volume:          db.Volume,
		Status:          s.dbStatus(r.Context(), db.Name),
		BackupSchedule:  db.BackupSchedule,
		BackupRetention: db.BackupRetention,
		CreatedAt:       db.CreatedAt,
		LinkedApps:      db.LinkedApps,
		Credentials:     &creds,
		URL:             connURL,
		Snippet:         snippet,
	}
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) getDatabase(w http.ResponseWriter, r *http.Request, _ string) {
	name := r.PathValue("name")
	db, creds, err := s.Databases.Get(r.Context(), name)
	if errors.Is(err, databases.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such database: "+name)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}

	reveal := r.URL.Query().Get("reveal") == "true"
	var credsPtr *databases.Credentials
	var connURL string
	if reveal {
		credsPtr = &creds
		connURL = databases.ConnectionString(db, creds)
	}

	hostName, sshPort, userName := sshTarget()
	snippet := databases.SSHTunnelSnippet(db, creds, hostName, sshPort, userName)

	port := databases.Engines[db.Engine].DefaultPort
	view := DatabaseView{
		ID:              db.ID,
		Name:            db.Name,
		Engine:          db.Engine,
		Version:         db.Version,
		Port:            port,
		Volume:          db.Volume,
		Status:          s.dbStatus(r.Context(), db.Name),
		BackupSchedule:  db.BackupSchedule,
		BackupRetention: db.BackupRetention,
		CreatedAt:       db.CreatedAt,
		LinkedApps:      db.LinkedApps,
		Credentials:     credsPtr,
		URL:             connURL,
		Snippet:         snippet,
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) deleteDatabase(w http.ResponseWriter, r *http.Request, actor string) {
	name := r.PathValue("name")
	purge := r.URL.Query().Get("purge") == "true"
	force := r.URL.Query().Get("force") == "true"

	db, _, err := s.Databases.Get(r.Context(), name)
	if errors.Is(err, databases.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such database: "+name)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if len(db.LinkedApps) > 0 && !force {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot delete %s: still linked to %v (unlink first or use force)", name, db.LinkedApps))
		return
	}

	cname := quadlet.DBContainerName(db.Name)
	if s.Engine != nil && s.Engine.Host != nil {
		_ = s.Engine.Host.Stop(r.Context(), cname+".service")
		_ = s.Engine.Host.RemoveContainer(r.Context(), cname, 15)
	}

	// Remove unit files, network and env file
	_ = os.Remove(filepath.Join(s.Paths.QuadletDir, cname+".container"))
	_ = os.Remove(filepath.Join(s.Paths.QuadletDir, quadlet.DBNetwork(db.Name)))
	_ = os.Remove(s.Paths.DBEnvFile(db.Name))

	if purge {
		_ = os.Remove(filepath.Join(s.Paths.QuadletDir, quadlet.DBVolumeUnit(db.Name)))
		volName := strings.TrimSuffix(quadlet.DBVolumeUnit(db.Name), ".volume")
		_, _ = host.Run(r.Context(), 30*time.Second, "podman", "volume", "rm", "-f", volName)
	}

	if s.Engine != nil && s.Engine.Host != nil {
		_ = s.Engine.Host.DaemonReload(r.Context())
	}

	if s.Backup != nil {
		_ = s.Backup.RemoveDBTimer(r.Context(), db.Name)
	}

	if err := s.Databases.Delete(r.Context(), db.Name, force); err != nil {
		internalError(w, err)
		return
	}

	_ = audit.Log(s.DB, actor, "db.delete", db.Name, fmt.Sprintf("purge=%v", purge))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) restartDatabase(w http.ResponseWriter, r *http.Request, actor string) {
	name := r.PathValue("name")
	db, _, err := s.Databases.Get(r.Context(), name)
	if errors.Is(err, databases.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such database: "+name)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}

	unit := quadlet.DBContainerName(db.Name) + ".service"
	if s.Engine != nil && s.Engine.Host != nil {
		_, err := host.Run(r.Context(), 60*time.Second, "systemctl", "--user", "restart", unit)
		if err != nil {
			internalError(w, fmt.Errorf("restarting %s: %w", unit, err))
			return
		}
	}
	_ = audit.Log(s.DB, actor, "db.restart", db.Name, "")
	writeJSON(w, http.StatusOK, StatusResponse{Status: "restarted"})
}

func (s *Server) linkDatabase(w http.ResponseWriter, r *http.Request, actor string) {
	name := r.PathValue("name")
	var req LinkDatabaseRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.App == "" {
		writeError(w, http.StatusBadRequest, "app is required")
		return
	}

	a, err := s.Apps.Get(r.Context(), req.App)
	if errors.Is(err, apps.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such app: "+req.App)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}

	link, err := s.Databases.Link(r.Context(), name, a.ID, req.EnvNames)
	if err != nil {
		if errors.Is(err, databases.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no such database: "+name)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Trigger automated zero-downtime redeploy if app is currently live
	var redeployQueued bool
	if a.Live != nil && s.Engine != nil {
		if _, err := s.Engine.Enqueue(r.Context(), a.Spec.Name, "manual", ""); err == nil {
			redeployQueued = true
		}
	}

	_ = audit.Log(s.DB, actor, "db.link", name, fmt.Sprintf("app=%s redeploy=%v", a.Spec.Name, redeployQueued))
	writeJSON(w, http.StatusOK, LinkDatabaseResponse{Database: name, App: a.Spec.Name, EnvNames: link.EnvNames, RedeployQueued: redeployQueued})
}

func (s *Server) unlinkDatabase(w http.ResponseWriter, r *http.Request, actor string) {
	name := r.PathValue("name")
	var req UnlinkDatabaseRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.App == "" {
		writeError(w, http.StatusBadRequest, "app is required")
		return
	}

	a, err := s.Apps.Get(r.Context(), req.App)
	if errors.Is(err, apps.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such app: "+req.App)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}

	if err := s.Databases.Unlink(r.Context(), name, a.ID); err != nil {
		if errors.Is(err, databases.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no such database: "+name)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Trigger automated zero-downtime redeploy if app is currently live
	var redeployQueued bool
	if a.Live != nil && s.Engine != nil {
		if _, err := s.Engine.Enqueue(r.Context(), a.Spec.Name, "manual", ""); err == nil {
			redeployQueued = true
		}
	}

	_ = audit.Log(s.DB, actor, "db.unlink", name, fmt.Sprintf("app=%s redeploy=%v", a.Spec.Name, redeployQueued))
	writeJSON(w, http.StatusOK, LinkDatabaseResponse{Database: name, App: a.Spec.Name, RedeployQueued: redeployQueued})
}

type RestoreDatabaseRequest struct {
	SnapshotID     string `json:"snapshotId,omitempty"`
	Into           string `json:"into,omitempty"`
	SkipSafetyDump bool   `json:"skipSafetyDump,omitempty"`
	Confirm        bool   `json:"confirm"`
}

func (s *Server) backupDatabase(w http.ResponseWriter, r *http.Request, actor string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	name := r.PathValue("name")
	snap, err := s.Backup.BackupDatabase(r.Context(), name)
	if err != nil {
		_ = audit.Log(s.DB, actor, "db.backup_failed", name, err.Error())
		s.notifyBackupFailed(r.Context(), "db:"+name, err)
		internalError(w, err)
		return
	}
	_ = audit.Log(s.DB, actor, "db.backup", name, fmt.Sprintf("snapshot=%s size=%d", snap.ShortID, snap.SizeBytes))
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) restoreDatabase(w http.ResponseWriter, r *http.Request, actor string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	name := r.PathValue("name")
	var req RestoreDatabaseRequest
	_ = readJSON(w, r, &req)

	if !req.Confirm {
		writeError(w, http.StatusBadRequest, "confirm=true is required to restore database")
		return
	}

	opts := backup.RestoreOpts{
		SnapshotID:     req.SnapshotID,
		Into:           req.Into,
		SkipSafetyDump: req.SkipSafetyDump,
		Confirm:        req.Confirm,
	}

	if err := s.Backup.RestoreDatabase(r.Context(), name, opts); err != nil {
		_ = audit.Log(s.DB, actor, "db.restore_failed", name, err.Error())
		internalError(w, err)
		return
	}

	_ = audit.Log(s.DB, actor, "db.restore", name, fmt.Sprintf("snapshot=%s into=%s", req.SnapshotID, req.Into))
	writeJSON(w, http.StatusOK, RestoreDatabaseResponse{Status: "restored", Database: name})
}

func (s *Server) testRestoreDatabase(w http.ResponseWriter, r *http.Request, actor string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	name := r.PathValue("name")
	res, err := s.Backup.TestRestore(r.Context(), name)
	if err != nil {
		_ = audit.Log(s.DB, actor, "db.restore_test_failed", name, err.Error())
		internalError(w, err)
		return
	}
	_ = audit.Log(s.DB, actor, "db.restore_test", name, fmt.Sprintf("snapshot=%s duration=%s success=%v", res.SnapshotID, res.Duration, res.Success))
	writeJSON(w, http.StatusOK, res)
}
