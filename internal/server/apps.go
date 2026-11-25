package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dermaga/internal/deploy"
	"dermaga/internal/name"
	"dermaga/internal/store"
)

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleListRepoBranches(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	repoRec, err := s.cfg.Store.GetGitHubRepositoryByFullName(r.Context(), owner+"/"+repo)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository is not accessible to the GitHub App")
		return
	}
	appID, pemBytes, err := s.loadGitHubCredentials(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	branches, err := s.githubClient().ListBranches(r.Context(), appID, pemBytes, repoRec.InstallationID, owner, repo)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	names := make([]string, 0, len(branches))
	for _, b := range branches {
		names = append(names, b.Name)
	}
	writeJSON(w, http.StatusOK, RepoBranchesResponse{Branches: names})
}

func (s *Server) handleInspectRepo(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	branch := strings.TrimSpace(r.URL.Query().Get("branch"))
	repoRec, err := s.cfg.Store.GetGitHubRepositoryByFullName(r.Context(), owner+"/"+repo)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository is not accessible to the GitHub App")
		return
	}
	if branch == "" {
		branch = repoRec.DefaultBranch
	}
	appID, pemBytes, err := s.loadGitHubCredentials(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	insp, err := s.githubClient().InspectRepo(r.Context(), appID, pemBytes, repoRec.InstallationID, owner, repo, branch)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	cands := make([]RepoContainerfileCandidate, 0, len(insp.Candidates))
	for _, c := range insp.Candidates {
		cands = append(cands, RepoContainerfileCandidate{
			Path:         c.Path,
			BuildContext: c.BuildContext,
			ExposedPort:  c.ExposedPort,
		})
	}
	writeJSON(w, http.StatusOK, RepoInspectResponse{
		Branch:           insp.Branch,
		HasContainerfile: insp.HasContainerfile,
		Containerfile:    insp.Containerfile,
		BuildContext:     insp.BuildContext,
		SuggestedPort:    insp.SuggestedPort,
		Candidates:       cands,
		HasSubmodules:    insp.HasSubmodules,
		HasLFS:           insp.HasLFS,
		TreeTruncated:    insp.TreeTruncated,
	})
}

func (s *Server) handleCheckDomainDNS(w http.ResponseWriter, r *http.Request) {
	var req DomainCheckRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if domain == "" {
		writeError(w, http.StatusBadRequest, "domain is required")
		return
	}
	info, errMsg := s.checkHookDomainDNS(r.Context(), domain)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	writeJSON(w, http.StatusOK, DomainCheckResponse{
		Domain:     domain,
		Matches:    info.AMatches,
		ExpectedIP: info.ServerIP,
		ResolvedIP: info.ARecords,
		Message:    info.Warning,
	})
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.cfg.Store.ListApps(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list apps")
		return
	}
	out := make([]AppSummary, 0, len(apps))
	for _, a := range apps {
		domains, _ := s.cfg.Store.ListDomainsByApp(r.Context(), a.ID)
		deps, _ := s.cfg.Store.ListDeploymentsByApp(r.Context(), a.ID)
		primary := ""
		for _, d := range domains {
			if d.IsPrimary {
				primary = d.Domain
				break
			}
		}
		var lastDep *DeploymentInfo
		if len(deps) > 0 {
			di := toDeploymentInfo(deps[0], false)
			lastDep = &di
		}
		out = append(out, AppSummary{
			ID:             a.ID,
			Name:           a.Name,
			RepoFullName:   a.RepoOwner + "/" + a.RepoName,
			Branch:         a.Branch,
			PrimaryDomain:  primary,
			Port:           a.Port,
			HealthType:     a.HealthType,
			LastDeployment: lastDep,
			CreatedAt:      a.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, AppsListResponse{Apps: out})
}

func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	var req CreateAppRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	fields := make(map[string]string)
	appName := strings.TrimSpace(req.Name)
	if err := name.Validate(appName); err != nil {
		fields["name"] = err.Error()
	} else if _, err := s.cfg.Store.GetAppByName(r.Context(), appName); err == nil {
		fields["name"] = fmt.Sprintf("app name %q already exists", appName)
	}

	repoFull := strings.TrimSpace(req.RepoFullName)
	owner, repoName, ok := strings.Cut(repoFull, "/")
	if !ok || owner == "" || repoName == "" {
		fields["repo_full_name"] = "select a valid repository"
	} else if _, err := s.cfg.Store.GetGitHubRepositoryByFullName(r.Context(), repoFull); err != nil {
		fields["repo_full_name"] = "repository is not accessible to the connected GitHub App"
	}

	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		fields["branch"] = "branch is required"
	}
	if req.Port < 1 || req.Port > 65535 {
		fields["port"] = "port must be between 1 and 65535"
	}

	healthType := strings.TrimSpace(req.HealthType)
	if healthType == "" {
		healthType = "http"
	}
	if healthType != "http" && healthType != "cmd" && healthType != "none" {
		fields["health_type"] = "health check type must be http, cmd, or none"
	}
	if healthType == "http" && strings.TrimSpace(req.HealthPath) == "" {
		fields["health_path"] = "health check path is required for HTTP health checks"
	}
	if healthType == "cmd" && strings.TrimSpace(req.HealthCmd) == "" {
		fields["health_cmd"] = "health check command is required for command health checks"
	}

	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if domain == "" {
		fields["domain"] = "primary domain is required"
	} else {
		if hookDomain, _ := s.cfg.Store.GetSetting(r.Context(), "hook_domain"); hookDomain != "" && strings.EqualFold(domain, hookDomain) {
			fields["domain"] = "domain cannot equal the reserved hook domain"
		} else if exists, err := s.cfg.Store.DomainExists(r.Context(), domain); err == nil && exists {
			fields["domain"] = fmt.Sprintf("domain %q is already assigned to another app", domain)
		}
	}

	runtimeEnv, buildEnv, envErr := splitAndValidateEnv(req.Env)
	if envErr != nil {
		fields["env"] = envErr.Error()
	}

	if len(fields) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "validation failed",
			"fields": fields,
		})
		return
	}

	envSealed, err := deploy.SealEnvMap(s.cfg.MasterKey, "env_encrypted", runtimeEnv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt runtime environment")
		return
	}
	buildEnvSealed, err := deploy.SealEnvMap(s.cfg.MasterKey, "build_env_encrypted", buildEnv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt build environment")
		return
	}

	cfPath := strings.TrimSpace(req.ContainerfilePath)
	if cfPath == "" {
		cfPath = "Containerfile"
	}
	buildCtx := strings.TrimSpace(req.BuildContext)
	if buildCtx == "" {
		buildCtx = "."
	}
	timeoutSec := req.HealthTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	consecutive := req.HealthConsecutive
	if consecutive <= 0 {
		consecutive = 3
	}

	var hpPtr, hcPtr *string
	if healthType == "http" {
		hp := strings.TrimSpace(req.HealthPath)
		if !strings.HasPrefix(hp, "/") {
			hp = "/" + hp
		}
		hpPtr = &hp
	} else if healthType == "cmd" {
		hc := strings.TrimSpace(req.HealthCmd)
		hcPtr = &hc
	}

	appID := "app_" + newRandomHex(12)
	app := &store.App{
		ID:                appID,
		Name:              appName,
		RepoOwner:         owner,
		RepoName:          repoName,
		Branch:            branch,
		ContainerfilePath: cfPath,
		BuildContext:      buildCtx,
		Port:              req.Port,
		HealthType:        healthType,
		HealthPath:        hpPtr,
		HealthCmd:         hcPtr,
		HealthTimeoutSec:  timeoutSec,
		HealthConsecutive: consecutive,
		MemoryLimit:       "512m",
		CPULimit:          "1.0",
		PidsLimit:         128,
		EnvEncrypted:      envSealed,
		BuildEnvEncrypted: buildEnvSealed,
		AutoDeploy:        true,
	}
	if err := s.cfg.Store.CreateApp(r.Context(), app); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create app")
		return
	}
	dom := &store.Domain{
		ID:        "dom_" + newRandomHex(12),
		AppID:     appID,
		Domain:    domain,
		IsPrimary: true,
	}
	if err := s.cfg.Store.CreateDomain(r.Context(), dom); err != nil {
		_ = s.cfg.Store.DeleteApp(r.Context(), appID)
		writeError(w, http.StatusInternalServerError, "failed to create primary domain")
		return
	}

	commitSHA := ""
	commitMsg := "Initial deploy"
	if appIDNum, pemBytes, credErr := s.loadGitHubCredentials(r.Context()); credErr == nil {
		if repoRec, rErr := s.cfg.Store.GetGitHubRepositoryByFullName(r.Context(), repoFull); rErr == nil {
			if ci, cErr := s.githubClient().GetCommit(r.Context(), appIDNum, pemBytes, repoRec.InstallationID, owner, repoName, branch); cErr == nil {
				commitSHA = ci.SHA
				if ci.Message != "" {
					commitMsg = ci.Message
				}
			}
		}
	}

	if s.cfg.Deploy == nil {
		writeError(w, http.StatusInternalServerError, "deployment engine is not configured")
		return
	}
	dep, err := s.cfg.Deploy.Enqueue(r.Context(), appID, "manual", commitSHA, commitMsg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue deployment")
		return
	}

	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "app_create",
		fmt.Sprintf("Created app %s (%s) on %s", app.Name, repoFull, domain), &app.ID, nil)

	di := toDeploymentInfo(*dep, false)
	writeJSON(w, http.StatusCreated, CreateAppResponse{
		App: AppSummary{
			ID:             app.ID,
			Name:           app.Name,
			RepoFullName:   repoFull,
			Branch:         app.Branch,
			PrimaryDomain:  domain,
			Port:           app.Port,
			HealthType:     app.HealthType,
			LastDeployment: &di,
			CreatedAt:      app.CreatedAt,
		},
		Deployment: di,
	})
}

func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request) {
	app, err := s.cfg.Store.GetApp(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app")
		return
	}

	domains, _ := s.cfg.Store.ListDomainsByApp(r.Context(), app.ID)
	primary := ""
	for _, d := range domains {
		if d.IsPrimary {
			primary = d.Domain
			break
		}
	}
	deps, _ := s.cfg.Store.ListDeploymentsByApp(r.Context(), app.ID)
	depInfos := make([]DeploymentInfo, 0, len(deps))
	for _, d := range deps {
		depInfos = append(depInfos, toDeploymentInfo(d, true))
	}

	runtimeEnv, _ := deploy.OpenEnvMap(s.cfg.MasterKey, "env_encrypted", app.EnvEncrypted)
	buildEnv, _ := deploy.OpenEnvMap(s.cfg.MasterKey, "build_env_encrypted", app.BuildEnvEncrypted)
	envMeta := mergeEnvMetadata(runtimeEnv, buildEnv)

	var caps []string
	for _, c := range strings.Split(app.AddCapabilities, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			caps = append(caps, c)
		}
	}
	hp, hc := "", ""
	if app.HealthPath != nil {
		hp = *app.HealthPath
	}
	if app.HealthCmd != nil {
		hc = *app.HealthCmd
	}

	writeJSON(w, http.StatusOK, AppDetailResponse{
		ID:                app.ID,
		Name:              app.Name,
		RepoFullName:      app.RepoOwner + "/" + app.RepoName,
		Branch:            app.Branch,
		ContainerfilePath: app.ContainerfilePath,
		BuildContext:      app.BuildContext,
		Port:              app.Port,
		HealthType:        app.HealthType,
		HealthPath:        hp,
		HealthCmd:         hc,
		HealthTimeoutSec:  app.HealthTimeoutSec,
		HealthConsecutive: app.HealthConsecutive,
		MemoryLimit:       app.MemoryLimit,
		CPULimit:          app.CPULimit,
		PidsLimit:         app.PidsLimit,
		ReadOnlyRoot:      app.ReadOnlyRoot,
		AddCapabilities:   caps,
		PrimaryDomain:     primary,
		ActiveReleaseID:   app.ActiveReleaseID,
		Env:               envMeta,
		Deployments:       depInfos,
		CreatedAt:         app.CreatedAt,
		UpdatedAt:         app.UpdatedAt,
	})
}

func (s *Server) handleUpdateAppSettings(w http.ResponseWriter, r *http.Request) {
	app, err := s.cfg.Store.GetApp(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app")
		return
	}

	var req UpdateAppSettingsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Port < 1 || req.Port > 65535 {
		writeError(w, http.StatusBadRequest, "port must be between 1 and 65535")
		return
	}
	if req.HealthType != "http" && req.HealthType != "cmd" && req.HealthType != "none" {
		writeError(w, http.StatusBadRequest, "health_type must be http, cmd, or none")
		return
	}
	if strings.TrimSpace(req.Branch) == "" {
		writeError(w, http.StatusBadRequest, "branch is required")
		return
	}

	app.Branch = strings.TrimSpace(req.Branch)
	if cf := strings.TrimSpace(req.ContainerfilePath); cf != "" {
		app.ContainerfilePath = cf
	}
	if bc := strings.TrimSpace(req.BuildContext); bc != "" {
		app.BuildContext = bc
	}
	app.Port = req.Port
	app.HealthType = req.HealthType
	if req.HealthType == "http" {
		hp := strings.TrimSpace(req.HealthPath)
		if hp == "" {
			writeError(w, http.StatusBadRequest, "health_path is required for HTTP health checks")
			return
		}
		if !strings.HasPrefix(hp, "/") {
			hp = "/" + hp
		}
		app.HealthPath = &hp
		app.HealthCmd = nil
	} else if req.HealthType == "cmd" {
		hc := strings.TrimSpace(req.HealthCmd)
		if hc == "" {
			writeError(w, http.StatusBadRequest, "health_cmd is required for command health checks")
			return
		}
		app.HealthCmd = &hc
		app.HealthPath = nil
	} else {
		app.HealthPath = nil
		app.HealthCmd = nil
	}
	if req.HealthTimeoutSec > 0 {
		app.HealthTimeoutSec = req.HealthTimeoutSec
	}
	if req.HealthConsecutive > 0 {
		app.HealthConsecutive = req.HealthConsecutive
	}
	if strings.TrimSpace(req.MemoryLimit) != "" {
		app.MemoryLimit = strings.TrimSpace(req.MemoryLimit)
	}
	if strings.TrimSpace(req.CPULimit) != "" {
		app.CPULimit = strings.TrimSpace(req.CPULimit)
	}
	if req.PidsLimit > 0 {
		app.PidsLimit = req.PidsLimit
	}
	app.ReadOnlyRoot = req.ReadOnlyRoot
	app.AddCapabilities = strings.Join(req.AddCapabilities, ",")

	if err := s.cfg.Store.UpdateApp(r.Context(), app); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update app settings")
		return
	}

	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "app_settings_update",
		fmt.Sprintf("Updated settings for app %s", app.Name), &app.ID, nil)

	s.handleGetApp(w, r)
}

func (s *Server) handleUpdateAppEnv(w http.ResponseWriter, r *http.Request) {
	app, err := s.cfg.Store.GetApp(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app")
		return
	}

	var req UpdateAppEnvRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	runtimeEnv, buildEnv, err := splitAndValidateEnv(req.Env)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	envSealed, err := deploy.SealEnvMap(s.cfg.MasterKey, "env_encrypted", runtimeEnv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt runtime environment")
		return
	}
	buildSealed, err := deploy.SealEnvMap(s.cfg.MasterKey, "build_env_encrypted", buildEnv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt build environment")
		return
	}

	app.EnvEncrypted = envSealed
	app.BuildEnvEncrypted = buildSealed
	if err := s.cfg.Store.UpdateApp(r.Context(), app); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update environment variables")
		return
	}

	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "env_change",
		fmt.Sprintf("Updated environment variables for app %s (%d runtime, %d build-time)", app.Name, len(runtimeEnv), len(buildEnv)), &app.ID, nil)

	writeJSON(w, http.StatusOK, map[string]any{
		"env": mergeEnvMetadata(runtimeEnv, buildEnv),
	})
}

func (s *Server) handleRevealAppEnv(w http.ResponseWriter, r *http.Request) {
	app, err := s.cfg.Store.GetApp(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app")
		return
	}

	var req EnvRevealRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var vars map[string]string
	switch req.Scope {
	case "build_time":
		vars, err = deploy.OpenEnvMap(s.cfg.MasterKey, "build_env_encrypted", app.BuildEnvEncrypted)
	case "runtime", "":
		req.Scope = "runtime"
		vars, err = deploy.OpenEnvMap(s.cfg.MasterKey, "env_encrypted", app.EnvEncrypted)
	default:
		writeError(w, http.StatusBadRequest, "scope must be runtime or build_time")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decrypt environment")
		return
	}
	val, ok := vars[req.Key]
	if !ok {
		writeError(w, http.StatusNotFound, "environment variable not found")
		return
	}

	// UI-5 / AUD-1: Record explicit secret reveal in audit log (never the value).
	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "secret_reveal",
		fmt.Sprintf("Revealed %s variable %s for app %s", req.Scope, req.Key, app.Name), &app.ID, nil)

	writeJSON(w, http.StatusOK, EnvRevealResponse{
		Key:   req.Key,
		Scope: req.Scope,
		Value: val,
	})
}

func (s *Server) handleTriggerDeploy(w http.ResponseWriter, r *http.Request) {
	app, err := s.cfg.Store.GetApp(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app")
		return
	}
	if s.cfg.Deploy == nil {
		writeError(w, http.StatusInternalServerError, "deployment engine is not configured")
		return
	}

	commitSHA := ""
	commitMsg := "Manual deploy"
	if appIDNum, pemBytes, credErr := s.loadGitHubCredentials(r.Context()); credErr == nil {
		fullName := app.RepoOwner + "/" + app.RepoName
		if repoRec, rErr := s.cfg.Store.GetGitHubRepositoryByFullName(r.Context(), fullName); rErr == nil {
			if ci, cErr := s.githubClient().GetCommit(r.Context(), appIDNum, pemBytes, repoRec.InstallationID, app.RepoOwner, app.RepoName, app.Branch); cErr == nil {
				commitSHA = ci.SHA
				if ci.Message != "" {
					commitMsg = ci.Message
				}
			}
		}
	}

	dep, err := s.cfg.Deploy.Enqueue(r.Context(), app.ID, "manual", commitSHA, commitMsg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue deployment")
		return
	}
	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "deploy",
		fmt.Sprintf("Triggered manual deploy r%d for app %s", dep.ReleaseNumber, app.Name), &app.ID, nil)

	writeJSON(w, http.StatusAccepted, toDeploymentInfo(*dep, false))
}

func (s *Server) handleListAppDeployments(w http.ResponseWriter, r *http.Request) {
	deps, err := s.cfg.Store.ListDeploymentsByApp(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deployments")
		return
	}
	out := make([]DeploymentInfo, 0, len(deps))
	for _, d := range deps {
		out = append(out, toDeploymentInfo(d, true))
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployments": out})
}

func (s *Server) handleCancelDeployment(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Deploy == nil {
		writeError(w, http.StatusInternalServerError, "deployment engine is not configured")
		return
	}
	depID := r.PathValue("id")
	dep, err := s.cfg.Store.GetDeployment(r.Context(), depID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}
	if err := s.cfg.Deploy.Cancel(r.Context(), depID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "deploy_cancel",
		fmt.Sprintf("Cancelled deployment %s (r%d)", dep.ID, dep.ReleaseNumber), &dep.AppID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleDeleteApp(w http.ResponseWriter, r *http.Request) {
	app, err := s.cfg.Store.GetApp(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app")
		return
	}
	var req DeleteAppRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ConfirmName != app.Name {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("type %q to confirm deletion", app.Name))
		return
	}
	if s.cfg.Deploy == nil {
		writeError(w, http.StatusInternalServerError, "deployment engine is not configured")
		return
	}
	if err := s.cfg.Deploy.RemoveApp(r.Context(), app.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove app")
		return
	}
	_ = s.cfg.Store.RecordAudit(r.Context(), "panel", "admin", "app_delete",
		fmt.Sprintf("Removed app %s", app.Name), nil, nil)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleStreamBuildLogs(w http.ResponseWriter, r *http.Request) {
	depID := r.PathValue("id")
	dep, err := s.cfg.Store.GetDeployment(r.Context(), depID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deployment")
		return
	}

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	writeSSE := func(event, data string) {
		b, _ := json.Marshal(data)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		flusher.Flush()
	}

	if s.cfg.Deploy == nil || isTerminalDeployment(dep.Status) {
		if dep.BuildLogs != "" {
			writeSSE("log", dep.BuildLogs)
		}
		writeSSE("status", store.DeploymentStatusLabel(dep.Status))
		writeSSE("done", dep.ErrorMessage)
		return
	}

	snapshot, ch, done, cancel := s.cfg.Deploy.Logs().Subscribe(depID)
	defer cancel()

	if snapshot != "" {
		writeSSE("log", snapshot)
	}
	if done {
		latest, _ := s.cfg.Store.GetDeployment(r.Context(), depID)
		if latest != nil {
			writeSSE("status", store.DeploymentStatusLabel(latest.Status))
			writeSSE("done", latest.ErrorMessage)
		} else {
			writeSSE("done", "")
		}
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case line, open := <-ch:
			if !open {
				latest, _ := s.cfg.Store.GetDeployment(r.Context(), depID)
				if latest != nil {
					writeSSE("status", store.DeploymentStatusLabel(latest.Status))
					writeSSE("done", latest.ErrorMessage)
				} else {
					writeSSE("done", "")
				}
				return
			}
			writeSSE("log", line)
		case <-ticker.C:
			latest, err := s.cfg.Store.GetDeployment(r.Context(), depID)
			if err == nil {
				writeSSE("status", store.DeploymentStatusLabel(latest.Status))
				if isTerminalDeployment(latest.Status) {
					writeSSE("done", latest.ErrorMessage)
					return
				}
			}
		}
	}
}

func (s *Server) handleStreamRuntimeLogs(w http.ResponseWriter, r *http.Request) {
	app, err := s.cfg.Store.GetApp(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app")
		return
	}
	if app.ActiveReleaseID == nil || *app.ActiveReleaseID == "" {
		writeError(w, http.StatusConflict, "app has no live release")
		return
	}
	dep, err := s.cfg.Store.GetDeployment(r.Context(), *app.ActiveReleaseID)
	if err != nil {
		writeError(w, http.StatusNotFound, "active release not found")
		return
	}

	lines := 200
	if q := r.URL.Query().Get("lines"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 2000 {
			lines = n
		}
	}
	follow := r.URL.Query().Get("follow") != "false"

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	if s.cfg.Host == nil {
		writeError(w, http.StatusInternalServerError, "host runner is not configured")
		return
	}

	serviceName := name.AppServiceName(app.Name, dep.ReleaseNumber)
	args := []string{"--user", "-u", serviceName, "-n", strconv.Itoa(lines), "--no-pager", "-o", "short-iso"}
	if follow {
		args = append(args, "-f")
	}

	err = s.cfg.Host.StreamCommandWithEnv(r.Context(), nil, func(line string) {
		b, _ := json.Marshal(line)
		_, _ = fmt.Fprintf(w, "event: log\ndata: %s\n\n", b)
		flusher.Flush()
	}, "journalctl", args...)
	if err != nil && !errors.Is(r.Context().Err(), r.Context().Err()) {
		b, _ := json.Marshal(err.Error())
		_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", b)
		flusher.Flush()
	}
	_, _ = fmt.Fprintf(w, "event: done\ndata: \"\"\n\n")
	flusher.Flush()
}

func splitAndValidateEnv(items []EnvVarInput) (map[string]string, map[string]string, error) {
	runtimeEnv := make(map[string]string)
	buildEnv := make(map[string]string)
	for _, item := range items {
		k := strings.TrimSpace(item.Key)
		if err := deploy.ValidateEnvKey(k); err != nil {
			return nil, nil, err
		}
		if err := deploy.ValidateEnvValue(k, item.Value); err != nil {
			return nil, nil, err
		}
		if !item.Runtime && !item.BuildTime {
			return nil, nil, fmt.Errorf("variable %q must apply to runtime, build time, or both", k)
		}
		if item.Runtime {
			runtimeEnv[k] = item.Value
		}
		if item.BuildTime {
			buildEnv[k] = item.Value
		}
	}
	return runtimeEnv, buildEnv, nil
}

func mergeEnvMetadata(runtimeEnv, buildEnv map[string]string) []EnvVarMetadata {
	seen := make(map[string]EnvVarMetadata)
	for k := range runtimeEnv {
		seen[k] = EnvVarMetadata{Key: k, Runtime: true}
	}
	for k := range buildEnv {
		m := seen[k]
		m.Key = k
		m.BuildTime = true
		seen[k] = m
	}
	out := make([]EnvVarMetadata, 0, len(seen))
	for _, m := range seen {
		out = append(out, m)
	}
	// Deterministic order by key
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Key < out[i].Key {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func toDeploymentInfo(d store.Deployment, includeLogs bool) DeploymentInfo {
	info := DeploymentInfo{
		ID:            d.ID,
		AppID:         d.AppID,
		ReleaseNumber: d.ReleaseNumber,
		Status:        d.Status,
		StatusLabel:   store.DeploymentStatusLabel(d.Status),
		Trigger:       d.Trigger,
		CommitHash:    d.CommitHash,
		CommitMessage: d.CommitMessage,
		ErrorMessage:  d.ErrorMessage,
		StartedAt:     d.StartedAt,
		FinishedAt:    d.FinishedAt,
	}
	if includeLogs {
		info.BuildLogs = d.BuildLogs
	}
	return info
}

func isTerminalDeployment(status string) bool {
	switch status {
	case "active", "inactive", "failed", "cancelled", "superseded":
		return true
	default:
		return false
	}
}

func newRandomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
