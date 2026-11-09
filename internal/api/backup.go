package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/r3p-dev/dermaga/internal/audit"
	"github.com/r3p-dev/dermaga/internal/backup"
	"github.com/r3p-dev/dermaga/internal/notify"
)

type RunBackupRequest struct {
	Target string `json:"target"` // db:<name>, system, or volume:<app>:<vol>
}

type RestoreSystemRequest struct {
	SnapshotID string `json:"snapshotId,omitempty"`
	Confirm    bool   `json:"confirm"`
}

type CheckBackupRequest struct {
	ReadDataSubset string `json:"readDataSubset,omitempty"`
}

type FailureNotifyRequest struct {
	Unit string `json:"unit"`
}

func (s *Server) backupRoutes(mux *http.ServeMux, wrap func(http.HandlerFunc) http.Handler, actor string) {
	h := func(f func(http.ResponseWriter, *http.Request, string)) http.Handler {
		return wrap(func(w http.ResponseWriter, r *http.Request) { f(w, r, actor) })
	}
	mux.Handle("GET /api/backup/settings", h(s.getBackupSettings))
	mux.Handle("PUT /api/backup/settings", h(s.putBackupSettings))
	mux.Handle("GET /api/backup/snapshots", h(s.listSnapshots))
	mux.Handle("POST /api/backup/run", h(s.runBackup))
	mux.Handle("POST /api/backup/check", h(s.checkBackup))
	mux.Handle("POST /api/backup/system", h(s.systemBackup))
	mux.Handle("POST /api/backup/restore-system", h(s.restoreSystem))
	mux.Handle("GET /api/keys/export", h(s.exportKeys))
	mux.Handle("POST /api/backup/failure-notify", h(s.failureNotify))
}

func (s *Server) getBackupSettings(w http.ResponseWriter, r *http.Request, _ string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	st, err := s.Backup.GetSettings(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	// Mask secret key in GET response
	if st.S3SecretKey != "" {
		st.S3SecretKey = "••••••••"
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) putBackupSettings(w http.ResponseWriter, r *http.Request, actor string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	var req backup.Settings
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.Backup.SaveSettings(r.Context(), req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = audit.Log(s.DB, actor, "backup.settings_update", req.Repository, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listSnapshots(w http.ResponseWriter, r *http.Request, _ string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	tag := r.URL.Query().Get("tag")
	snaps, err := s.Backup.Snapshots(r.Context(), tag)
	if err != nil {
		internalError(w, err)
		return
	}
	if snaps == nil {
		snaps = []backup.Snapshot{}
	}
	writeJSON(w, http.StatusOK, snaps)
}

func (s *Server) runBackup(w http.ResponseWriter, r *http.Request, actor string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	var req RunBackupRequest
	if !readJSON(w, r, &req) {
		return
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}

	var snap backup.Snapshot
	var err error

	switch {
	case strings.HasPrefix(target, "db:"):
		dbName := strings.TrimPrefix(target, "db:")
		snap, err = s.Backup.BackupDatabase(r.Context(), dbName)
		if err == nil {
			_ = audit.Log(s.DB, actor, "db.backup", dbName, fmt.Sprintf("snapshot=%s size=%d", snap.ShortID, snap.SizeBytes))
		}

	case target == "system":
		snap, err = s.Backup.BackupSystem(r.Context())
		if err == nil {
			_ = audit.Log(s.DB, actor, "system.backup", "system", fmt.Sprintf("snapshot=%s size=%d", snap.ShortID, snap.SizeBytes))
		}

	case strings.HasPrefix(target, "volume:"):
		parts := strings.Split(strings.TrimPrefix(target, "volume:"), ":")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "volume target format must be volume:<app>:<vol>")
			return
		}
		snap, err = s.Backup.BackupVolume(r.Context(), parts[0], parts[1])
		if err == nil {
			_ = audit.Log(s.DB, actor, "volume.backup", parts[0]+":"+parts[1], fmt.Sprintf("snapshot=%s size=%d", snap.ShortID, snap.SizeBytes))
		}

	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown backup target: %s", target))
		return
	}

	if err != nil {
		_ = audit.Log(s.DB, actor, "backup.failed", target, err.Error())
		internalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) checkBackup(w http.ResponseWriter, r *http.Request, actor string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	var req CheckBackupRequest
	_ = readJSON(w, r, &req)

	if err := s.Backup.Check(r.Context(), req.ReadDataSubset); err != nil {
		_ = audit.Log(s.DB, actor, "backup.check_failed", "", err.Error())
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = audit.Log(s.DB, actor, "backup.check", "success", "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) systemBackup(w http.ResponseWriter, r *http.Request, actor string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	snap, err := s.Backup.BackupSystem(r.Context())
	if err != nil {
		_ = audit.Log(s.DB, actor, "system.backup_failed", "system", err.Error())
		internalError(w, err)
		return
	}
	_ = audit.Log(s.DB, actor, "system.backup", "system", fmt.Sprintf("snapshot=%s size=%d", snap.ShortID, snap.SizeBytes))
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) restoreSystem(w http.ResponseWriter, r *http.Request, actor string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	var req RestoreSystemRequest
	if !readJSON(w, r, &req) {
		return
	}
	if !req.Confirm {
		writeError(w, http.StatusBadRequest, "confirm=true is required to restore system")
		return
	}
	if err := s.Backup.RestoreSystem(r.Context(), req.SnapshotID); err != nil {
		_ = audit.Log(s.DB, actor, "system.restore_failed", "", err.Error())
		internalError(w, err)
		return
	}
	_ = audit.Log(s.DB, actor, "system.restore", "system", "snapshot="+req.SnapshotID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "system restored successfully"})
}

func (s *Server) exportKeys(w http.ResponseWriter, _ *http.Request, actor string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	hexKey, inst, err := s.Backup.ExportKeys()
	if err != nil {
		internalError(w, err)
		return
	}
	_ = audit.Log(s.DB, actor, "keys.export", "", "")
	writeJSON(w, http.StatusOK, map[string]string{
		"key":          hexKey,
		"instructions": inst,
	})
}

func (s *Server) failureNotify(w http.ResponseWriter, r *http.Request, _ string) {
	var req FailureNotifyRequest
	_ = readJSON(w, r, &req)
	target := req.Unit
	if target == "" {
		target = "unknown-unit"
	}
	_ = audit.Log(s.DB, "timer", "backup.failed", target, "systemd failure event triggered")
	if s.Notifier != nil && s.DB != nil && s.Box != nil {
		if cfg, err := notify.LoadSettings(r.Context(), s.DB, s.Box); err == nil {
			_ = s.Notifier.Dispatch(r.Context(), cfg, notify.Message{
				Event: "backup.failed",
				Title: "Dermaga Backup Failed",
				Body:  "Backup unit " + target + " failed execution",
				Level: "error",
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}
