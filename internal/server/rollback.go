package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"dermaga/internal/deploy"
	"dermaga/internal/store"
)

var commitRE = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// EnvChange names one runtime variable that differs between the current settings and a release.
// Values are never sent (DEP-4: names only).
type EnvChange struct {
	Name   string `json:"name"`
	Change string `json:"change"` // "added" (only in the current settings), "removed" (only in the release), "changed"
}

// MemoryCheckResponse says whether a release with MemoryLimit fits next to everything running
// (owner decision: refuse when MemAvailable < the release's memory limit).
type MemoryCheckResponse struct {
	AvailableBytes int64  `json:"available_bytes"`
	NeededBytes    int64  `json:"needed_bytes"`
	OK             bool   `json:"ok"`
	MemoryLimit    string `json:"memory_limit"`
}

// RollbackPreview is what the rollback dialog shows before the operator confirms (DEP-4).
type RollbackPreview struct {
	Deployment DeploymentInfo      `json:"deployment"`
	EnvChanges []EnvChange         `json:"env_changes"`
	Memory     MemoryCheckResponse `json:"memory"`
}

func memoryResponse(mc deploy.MemoryCheck, limit string) MemoryCheckResponse {
	return MemoryCheckResponse{AvailableBytes: mc.AvailableBytes, NeededBytes: mc.NeededBytes, OK: mc.OK, MemoryLimit: limit}
}

// handleMemoryCheck tells the deploy dialog up front whether a new release fits next to the live
// one (owner decision: refuse when MemAvailable < the candidate's memory limit).
func (s *Server) handleMemoryCheck(w http.ResponseWriter, r *http.Request) {
	app, ok := s.loadAppForRequest(w, r)
	if !ok {
		return
	}
	mc, err := s.cfg.Deploy.CheckMemory(app.MemoryLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read the server's free memory: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, memoryResponse(mc, app.MemoryLimit))
}

// handleRollbackPreview returns the target release, the env variable names that will differ
// from the current settings after the rollback, and the memory check.
func (s *Server) handleRollbackPreview(w http.ResponseWriter, r *http.Request) {
	app, ok := s.loadAppForRequest(w, r)
	if !ok {
		return
	}
	target, err := s.cfg.Store.GetDeployment(r.Context(), r.URL.Query().Get("deployment_id"))
	if err != nil || target.AppID != app.ID {
		writeError(w, http.StatusNotFound, "release not found")
		return
	}
	if target.EnvVersionID == nil {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("r%d has no stored environment and cannot be rolled back to", target.ReleaseNumber))
		return
	}
	v, err := s.cfg.Store.GetEnvVersion(r.Context(), *target.EnvVersionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the release's environment")
		return
	}
	then, err := deploy.OpenEnvMap(s.cfg.MasterKey, "env_encrypted", v.EnvEncrypted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the release's environment")
		return
	}
	now, err := deploy.OpenEnvMap(s.cfg.MasterKey, "env_encrypted", app.EnvEncrypted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the current environment")
		return
	}
	rc, err := deploy.ReleaseRuntimeConfig(app, target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mc, err := s.cfg.Deploy.CheckMemory(rc.MemoryLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read the server's free memory: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, RollbackPreview{Deployment: toDeploymentInfo(*target, false), EnvChanges: envChanges(now, then), Memory: memoryResponse(mc, rc.MemoryLimit)})
}

// envChanges compares the current env with a release's env by name, sorted by name.
func envChanges(now, then map[string]string) []EnvChange {
	out := []EnvChange{}
	for k, v := range now {
		old, had := then[k]
		switch {
		case !had:
			out = append(out, EnvChange{Name: k, Change: "added"})
		case old != v:
			out = append(out, EnvChange{Name: k, Change: "changed"})
		}
	}
	for k := range then {
		if _, has := now[k]; !has {
			out = append(out, EnvChange{Name: k, Change: "removed"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// handleRollback starts a rollback to a retained release (DEP-4).
func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	app, ok := s.loadAppForRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		DeploymentID string `json:"deployment_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.DeploymentID == "" {
		writeError(w, http.StatusBadRequest, "deployment_id is required")
		return
	}
	target, err := s.cfg.Store.GetDeployment(r.Context(), req.DeploymentID)
	if err != nil || target.AppID != app.ID {
		writeError(w, http.StatusNotFound, "release not found")
		return
	}
	dep, err := s.cfg.Deploy.Rollback(r.Context(), app.ID, target.ID)
	switch {
	case errors.Is(err, deploy.ErrBuildRunning):
		writeError(w, http.StatusConflict, "Cancel the running build first")
		return
	case errors.Is(err, deploy.ErrSwitchRunning):
		writeError(w, http.StatusConflict, "Another release of this app is starting or switching traffic. Try again when it has finished.")
		return
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "rollback",
		fmt.Sprintf("Rolled back app %s to r%d (new release r%d)", app.Name, target.ReleaseNumber, dep.ReleaseNumber), &app.ID, nil)
	writeJSON(w, http.StatusAccepted, toDeploymentInfo(*dep, false))
}

// resolveCommit turns a commit chosen in the redeploy dialog into its full SHA and subject via
// the GitHub App. ok=false means a response was written.
func (s *Server) resolveCommit(w http.ResponseWriter, r *http.Request, app *store.App, commit string) (sha, msg string, ok bool) {
	commit = strings.ToLower(strings.TrimSpace(commit))
	if !commitRE.MatchString(commit) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation failed", "fields": map[string]string{
			"commit": "enter a commit SHA: 7 to 40 characters, 0-9 and a-f",
		}})
		return "", "", false
	}
	appIDNum, pemBytes, err := s.loadGitHubCredentials(r.Context())
	if err != nil {
		writeError(w, http.StatusConflict, "GitHub is not connected, so the commit cannot be checked. Connect GitHub in Settings first.")
		return "", "", false
	}
	repo, err := s.cfg.Store.GetGitHubRepositoryByFullName(r.Context(), app.RepoOwner+"/"+app.RepoName)
	if err != nil {
		writeError(w, http.StatusConflict, "the GitHub App can no longer read this repository")
		return "", "", false
	}
	ci, err := s.githubClient().GetCommit(r.Context(), appIDNum, pemBytes, repo.InstallationID, app.RepoOwner, app.RepoName, commit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation failed", "fields": map[string]string{
			"commit": fmt.Sprintf("commit %s was not found in %s/%s", commit, app.RepoOwner, app.RepoName),
		}})
		return "", "", false
	}
	return ci.SHA, ci.Message, true
}

func (s *Server) loadAppForRequest(w http.ResponseWriter, r *http.Request) (*store.App, bool) {
	if s.cfg.Deploy == nil {
		writeError(w, http.StatusInternalServerError, "deployment engine is not configured")
		return nil, false
	}
	app, err := s.cfg.Store.GetApp(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "app not found")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app")
		return nil, false
	}
	return app, true
}
