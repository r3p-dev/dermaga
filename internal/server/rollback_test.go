package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"dermaga/internal/deploy"
	"dermaga/internal/store"
)

// seedReleases creates app "shop": r1 previous (env K=a, X=1), r2 live (env K=b, Y=2), r3 failed.
func seedReleases(t *testing.T, srv *Server, s *store.Store) *store.App {
	t.Helper()
	ctx := context.Background()
	env1, _ := deploy.SealEnvMap(srv.cfg.MasterKey, "env_encrypted", map[string]string{"K": "secret-a", "X": "1"})
	app := &store.App{ID: "app_shop", Name: "shop", RepoOwner: "octo", RepoName: "portfolio", Port: 8080, HealthType: "none", MemoryLimit: "512m", EnvEncrypted: env1}
	if err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	_ = s.CreateDomain(ctx, &store.Domain{ID: "dom_shop", AppID: app.ID, Domain: "shop.example.com", IsPrimary: true})
	v1 := *app.EnvVersionID
	app.EnvEncrypted, _ = deploy.SealEnvMap(srv.cfg.MasterKey, "env_encrypted", map[string]string{"K": "secret-b", "Y": "2"})
	_ = s.UpdateApp(ctx, app)
	app, _ = s.GetApp(ctx, app.ID)
	rc := `{"port":8080,"health_type":"none","memory_limit":"512m","cpu_limit":"1.0","pids_limit":128}`
	for _, d := range []store.Deployment{
		{ID: "dep_1", ReleaseNumber: 1, Status: "inactive", EnvVersionID: &v1, ImageRef: "localhost/dermaga/shop:r1"},
		{ID: "dep_2", ReleaseNumber: 2, Status: "active", EnvVersionID: app.EnvVersionID, ImageRef: "localhost/dermaga/shop:r2"},
		{ID: "dep_3", ReleaseNumber: 3, Status: "failed", EnvVersionID: app.EnvVersionID},
	} {
		d.AppID, d.Trigger, d.CommitHash, d.CommitMessage, d.RuntimeConfig = app.ID, "manual", "c", "m", rc
		if err := s.CreateDeployment(ctx, &d); err != nil {
			t.Fatalf("CreateDeployment: %v", err)
		}
	}
	_ = s.SetActiveRelease(ctx, app.ID, "dep_2")
	return app
}

// TestRollbackAPI_DEP4 covers the deployment list flags, the preview (names only), the memory
// check, rollback refusals and the happy path, and authentication on every new route.
func TestRollbackAPI_DEP4(t *testing.T) {
	srv, s, _, token := setupAppsTestServer(t)
	app := seedReleases(t, srv, s)

	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/apps/app_shop/rollback-preview?deployment_id=dep_1"},
		{"POST", "/api/apps/app_shop/rollback"},
		{"GET", "/api/apps/app_shop/memory-check"},
	} {
		if rr := doAPIReq(srv, tc.method, tc.path, "", map[string]string{"deployment_id": "dep_1"}); rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without session: %d", tc.method, tc.path, rr.Code)
		}
	}

	rr := doAPIReq(srv, "GET", "/api/apps/app_shop/deployments", token, nil)
	var list struct {
		Deployments []DeploymentInfo `json:"deployments"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &list)
	flags := map[string]bool{}
	for _, d := range list.Deployments {
		flags[d.ID] = d.RollbackEligible
	}
	if !flags["dep_1"] || flags["dep_2"] || flags["dep_3"] {
		t.Fatalf("rollback_eligible: %v", flags)
	}

	rr = doAPIReq(srv, "GET", "/api/apps/app_shop/rollback-preview?deployment_id=dep_1", token, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "secret-a") || strings.Contains(rr.Body.String(), "secret-b") {
		t.Fatalf("the preview must never contain env values: %s", rr.Body.String())
	}
	var prev RollbackPreview
	_ = json.Unmarshal(rr.Body.Bytes(), &prev)
	want := []EnvChange{{"K", "changed"}, {"X", "removed"}, {"Y", "added"}}
	if len(prev.EnvChanges) != 3 || prev.EnvChanges[0] != want[0] || prev.EnvChanges[1] != want[1] || prev.EnvChanges[2] != want[2] || !prev.Memory.OK {
		t.Fatalf("preview: %+v", prev)
	}

	rr = doAPIReq(srv, "GET", "/api/apps/app_shop/memory-check", token, nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"ok":true`) || !strings.Contains(rr.Body.String(), `"memory_limit":"512m"`) {
		t.Fatalf("memory-check: %d %s", rr.Code, rr.Body.String())
	}

	if rr = doAPIReq(srv, "POST", "/api/apps/app_shop/rollback", token, map[string]string{"deployment_id": "dep_3"}); rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "only the last 5") {
		t.Fatalf("rollback to a failed release: %d %s", rr.Code, rr.Body.String())
	}
	if rr = doAPIReq(srv, "POST", "/api/apps/app_shop/rollback", token, map[string]string{"deployment_id": "nope"}); rr.Code != http.StatusNotFound {
		t.Fatalf("rollback to an unknown release: %d", rr.Code)
	}

	rr = doAPIReq(srv, "POST", "/api/apps/app_shop/rollback", token, map[string]string{"deployment_id": "dep_1"})
	if rr.Code != http.StatusAccepted || !strings.Contains(rr.Body.String(), `"trigger":"rollback"`) {
		t.Fatalf("rollback: %d %s", rr.Code, rr.Body.String())
	}
	var started DeploymentInfo
	_ = json.Unmarshal(rr.Body.Bytes(), &started)
	deadline := time.Now().Add(3 * time.Second)
	for {
		d, _ := s.GetDeployment(context.Background(), started.ID)
		if d.Status == "active" {
			break
		}
		if time.Now().After(deadline) || d.Status == "failed" {
			t.Fatalf("rollback release ended as %s: %s", d.Status, d.ErrorMessage)
		}
		time.Sleep(10 * time.Millisecond)
	}
	logs, _ := s.ListAuditLogs(context.Background(), store.AuditFilter{AppID: &app.ID})
	found := false
	for _, l := range logs {
		found = found || (l.Event == "rollback" && strings.Contains(l.Description, "to r1"))
	}
	if !found {
		t.Fatalf("rollback must be in the audit log (AUD-1)")
	}
}

// TestRedeployCommitValidation: a chosen commit must look like a SHA (field error) and needs
// GitHub to be resolved.
func TestRedeployCommitValidation(t *testing.T) {
	srv, s, _, token := setupAppsTestServer(t)
	seedReleases(t, srv, s)
	rr := doAPIReq(srv, "POST", "/api/apps/app_shop/deploy", token, map[string]string{"commit": "not-a-sha!"})
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), `"commit"`) {
		t.Fatalf("invalid commit: %d %s", rr.Code, rr.Body.String())
	}
	rr = doAPIReq(srv, "POST", "/api/apps/app_shop/deploy", token, map[string]string{"commit": "abc1234"})
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "GitHub is not connected") {
		t.Fatalf("commit without GitHub: %d %s", rr.Code, rr.Body.String())
	}
}

// TestRollbackAPI_RefusedWhileBuilding: owner decision, 409 "Cancel the running build first".
func TestRollbackAPI_RefusedWhileBuilding(t *testing.T) {
	srv, s, _, token := setupAppsTestServer(t)
	seedReleases(t, srv, s)
	// The test server's engine is not started, so this deploy stays queued for the app.
	if _, err := srv.cfg.Deploy.Enqueue(context.Background(), "app_shop", "manual", "", "queued"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	rr := doAPIReq(srv, "POST", "/api/apps/app_shop/rollback", token, map[string]string{"deployment_id": "dep_1"})
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), `"Cancel the running build first"`) {
		t.Fatalf("rollback while a build is queued: %d %s", rr.Code, rr.Body.String())
	}
}
