package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"dermaga/internal/store"
)

func TestStore_AppsAndDeployments(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	// 1. Create App
	appID := "app_01j7demo"
	cmd := "npm start"
	healthPath := "/healthz"
	app := &store.App{
		ID:                appID,
		Name:              "demo",
		RepoOwner:         "testorg",
		RepoName:          "demo-repo",
		Branch:            "main",
		ContainerfilePath: "Containerfile",
		BuildContext:      ".",
		Command:           &cmd,
		Port:              3000,
		HealthType:        "http",
		HealthPath:        &healthPath,
		HealthTimeoutSec:  15,
		AutoDeploy:        true,
	}

	if err := st.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	gotApp, err := st.GetApp(ctx, appID)
	if err != nil {
		t.Fatalf("GetApp failed: %v", err)
	}
	if gotApp.Name != "demo" || gotApp.Port != 3000 || !gotApp.AutoDeploy {
		t.Errorf("unexpected app values: %+v", gotApp)
	}

	gotByName, err := st.GetAppByName(ctx, "demo")
	if err != nil {
		t.Fatalf("GetAppByName failed: %v", err)
	}
	if gotByName.ID != appID {
		t.Errorf("GetAppByName ID = %s, want %s", gotByName.ID, appID)
	}

	// 2. Create Domain
	domID := "dom_01j7demo"
	domain := &store.Domain{
		ID:        domID,
		AppID:     appID,
		Domain:    "demo.example.com",
		IsPrimary: true,
	}
	if err := st.CreateDomain(ctx, domain); err != nil {
		t.Fatalf("CreateDomain failed: %v", err)
	}

	domains, err := st.ListDomainsByApp(ctx, appID)
	if err != nil {
		t.Fatalf("ListDomainsByApp failed: %v", err)
	}
	if len(domains) != 1 || domains[0].Domain != "demo.example.com" {
		t.Fatalf("unexpected domains: %+v", domains)
	}

	// 3. Create Deployment
	depID := "dep_01j7rel1"
	dep := &store.Deployment{
		ID:                   depID,
		AppID:                appID,
		ReleaseNumber:        1,
		Status:               "active",
		Trigger:              "manual",
		CommitHash:           "abcd123",
		CommitMessage:        "Initial release",
		EnvSnapshotEncrypted: "encrypted-data",
	}
	if err := st.CreateDeployment(ctx, dep); err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	gotDep, err := st.GetDeployment(ctx, depID)
	if err != nil {
		t.Fatalf("GetDeployment failed: %v", err)
	}
	if gotDep.ReleaseNumber != 1 || gotDep.Status != "active" {
		t.Errorf("unexpected deployment values: %+v", gotDep)
	}

	// 4. Set active release and verify GetActiveApps
	activeBefore, err := st.GetActiveApps(ctx)
	if err != nil {
		t.Fatalf("GetActiveApps failed: %v", err)
	}
	if len(activeBefore) != 0 {
		t.Errorf("expected 0 active apps before setting active release, got %d", len(activeBefore))
	}

	if err := st.SetActiveRelease(ctx, appID, depID); err != nil {
		t.Fatalf("SetActiveRelease failed: %v", err)
	}

	activeAfter, err := st.GetActiveApps(ctx)
	if err != nil {
		t.Fatalf("GetActiveApps failed: %v", err)
	}
	if len(activeAfter) != 1 {
		t.Fatalf("expected 1 active app, got %d", len(activeAfter))
	}
	if activeAfter[0].App.Name != "demo" || activeAfter[0].Deployment.ReleaseNumber != 1 || len(activeAfter[0].Domains) != 1 {
		t.Errorf("unexpected active record: %+v", activeAfter[0])
	}
}
