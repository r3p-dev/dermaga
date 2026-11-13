package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/audit"
	"github.com/r3p-dev/dermaga/internal/auth"
	"github.com/r3p-dev/dermaga/internal/backup"
	"github.com/r3p-dev/dermaga/internal/notify"
)

type RunBackupRequest struct {
	Target string `json:"target"` // db:<name>, system, or volume:<app>:<vol>
	// Scheduled is set only by `backup run --scheduled`, which dermaga's timer
	// units call. The unit's OnFailure= then sends the notification, so the API
	// must not send a second one.
	Scheduled bool `json:"scheduled,omitempty"`
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

// BackupPasswordResponse carries the restic password for its one display
// (generate) or an audited reveal, with the recovery statement to show next to it.
type BackupPasswordResponse struct {
	Password          string `json:"password"`
	PasswordState     string `json:"passwordState"`
	RecoveryStatement string `json:"recoveryStatement"`
}

// RevealBackupPasswordRequest re-authenticates a panel reveal. The CLI socket
// sends an empty body: socket access already is full owner access.
type RevealBackupPasswordRequest struct {
	Password string `json:"password,omitempty"` // admin password
	TOTP     string `json:"totp,omitempty"`
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
	mux.Handle("POST /api/backup/password", h(s.generateBackupPassword))
	mux.Handle("POST /api/backup/password/reveal", h(s.revealBackupPassword))
	mux.Handle("POST /api/backup/password/confirm", h(s.confirmBackupPassword))
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
	// Mask secret key in GET response. The restic password is never part of
	// it (backup.Settings.Password is json:"-").
	if st.S3SecretKey != "" {
		st.S3SecretKey = "••••••••"
	}
	writeJSON(w, http.StatusOK, st)
}

// writeBackupPassword sends the password with caching forbidden. The password
// goes only into this response body: never into logs, audit details or errors.
func (s *Server) writeBackupPassword(w http.ResponseWriter, r *http.Request, pass string) {
	st, err := s.Backup.GetSettings(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, BackupPasswordResponse{Password: pass, PasswordState: st.PasswordState, RecoveryStatement: backup.RecoveryStatement})
}

func (s *Server) generateBackupPassword(w http.ResponseWriter, r *http.Request, actor string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	pass, err := s.Backup.GeneratePassword(r.Context())
	if errors.Is(err, backup.ErrPasswordExists) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	_ = audit.Log(s.DB, actor, "backup.password_generated", "", "source="+actor)
	s.writeBackupPassword(w, r, pass)
}

func (s *Server) revealBackupPassword(w http.ResponseWriter, r *http.Request, actor string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	var req RevealBackupPasswordRequest
	if r.ContentLength != 0 && !readJSON(w, r, &req) {
		return
	}
	if actor != "cli" { // the panel re-authenticates; failures share the login limiter
		err := s.Auth.Verify(req.Password, req.TOTP)
		var blocked *auth.BlockedError
		switch {
		case errors.As(err, &blocked):
			w.Header().Set("Retry-After", strconv.Itoa(int(blocked.RetryAfter.Seconds()+0.999)))
			writeError(w, http.StatusTooManyRequests, blocked.Error())
			return
		case errors.Is(err, auth.ErrInvalid):
			_ = audit.Log(s.DB, actor, "backup.password_reveal_denied", "", "source="+actor)
			// 403, not 401: the session is valid (the UI treats 401 as "log in again").
			writeError(w, http.StatusForbidden, "wrong admin password or TOTP code")
			return
		case err != nil:
			internalError(w, err)
			return
		}
	}
	pass, err := s.Backup.RevealPassword(r.Context())
	if errors.Is(err, backup.ErrNoPassword) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	_ = audit.Log(s.DB, actor, "backup.password_revealed", "", "source="+actor)
	s.writeBackupPassword(w, r, pass)
}

func (s *Server) confirmBackupPassword(w http.ResponseWriter, r *http.Request, actor string) {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup service unavailable")
		return
	}
	err := s.Backup.ConfirmPassword(r.Context())
	if errors.Is(err, backup.ErrNoPassword) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	_ = audit.Log(s.DB, actor, "backup.password_confirmed", "", "source="+actor)
	writeJSON(w, http.StatusOK, map[string]string{"passwordState": backup.PasswordConfirmed})
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
		if !req.Scheduled {
			s.notifyBackupFailed(r.Context(), target, err)
		}
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
		s.notifyBackupFailed(r.Context(), "system", err)
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
	s.notifyBackupFailed(r.Context(), target, errors.New("the scheduled unit failed; see journalctl --user -u "+target))
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

// notifyBackupFailed sends the backup.failed notification for every failed
// backup: scheduled (through OnFailure= and failure-notify), manual CLI runs
// and panel requests. It is detached from the request, so the alert still goes
// out when the client has hung up.
func (s *Server) notifyBackupFailed(ctx context.Context, target string, cause error) {
	if s.Notifier == nil || s.DB == nil || s.Box == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	cfg, err := notify.LoadSettings(ctx, s.DB, s.Box)
	if err != nil {
		return
	}
	_ = s.Notifier.Dispatch(ctx, cfg, notify.Message{
		Event: "backup.failed",
		Title: "Dermaga Backup Failed",
		Body:  "Backup of " + target + " failed: " + cause.Error(),
		Level: "error",
	})
}
