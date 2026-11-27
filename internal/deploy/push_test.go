package deploy

import (
	"context"
	"strings"
	"testing"

	"dermaga/internal/store"
	"dermaga/internal/webhook"
)

func TestWatchPathsMatch_APP8(t *testing.T) {
	cases := []struct {
		watch string
		files []string
		want  bool
	}{
		{"", []string{"anything.go"}, true},
		{"web/", []string{"web/src/app.ts"}, true},
		{"web/", []string{"web"}, false}, // a file named web is not the folder
		{"web/", []string{"api/main.go", "README.md"}, false},
		{"web/*", []string{"web/index.html"}, true},
		{"web/*", []string{"web/src/app.ts"}, false}, // * stays within one folder
		{"Containerfile", []string{"Containerfile"}, true},
		{"Containerfile", []string{"sub/Containerfile"}, false},
		{"go.*", []string{"go.mod"}, true},
		{"services/*/", []string{"services/api/main.go"}, true},
		{"services/*/", []string{"services/README.md"}, false},
		{"docs/\n\n  web/  \n", []string{"web/a"}, true},
	}
	for _, c := range cases {
		if got := watchPathsMatch(c.watch, c.files); got != c.want {
			t.Errorf("watchPathsMatch(%q, %v) = %v, want %v", c.watch, c.files, got, c.want)
		}
	}
}

func TestNormalizeWatchPaths_APP8(t *testing.T) {
	got, err := NormalizeWatchPaths("  web/ \n\nContainerfile\r\n go.* ")
	if err != nil || got != "web/\nContainerfile\ngo.*" {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, err := NormalizeWatchPaths(" \n "); err != nil || got != "" {
		t.Fatalf("empty: got %q, %v", got, err)
	}
	for _, bad := range []string{"/web/", "../x", "a/../b", "..", "web//x", "[abc", "a\\", strings.Repeat("a", 257),
		strings.Repeat("x\n", 51)} {
		if _, err := NormalizeWatchPaths(bad); err == nil {
			t.Errorf("NormalizeWatchPaths(%q) accepted a bad value", bad)
		}
	}
}

// pushTestEngine starts an engine with one app on octo/web@main and auto-deploy on.
func pushTestEngine(t *testing.T) (*store.Store, *Engine, *store.App) {
	t.Helper()
	s, _, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = eng.Start(ctx)
	app := newTestApp(t, s, key, "shop", nil)
	app.AutoDeploy = true
	if err := s.UpdateApp(ctx, app); err != nil {
		t.Fatalf("UpdateApp: %v", err)
	}
	return s, eng, app
}

func pushDeployments(t *testing.T, s *store.Store, appID string) []store.Deployment {
	t.Helper()
	deps, err := s.ListDeploymentsByApp(context.Background(), appID)
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	return deps
}

func lastActivity(t *testing.T, s *store.Store, event string) string {
	t.Helper()
	logs, _ := s.ListAuditLogs(context.Background(), store.AuditFilter{Event: event, Limit: 1})
	if len(logs) == 0 {
		return ""
	}
	return logs[0].Description
}

func TestHandlePush_DeploysMatchingPush_APP8(t *testing.T) {
	s, eng, app := pushTestEngine(t)
	eng.HandlePush(context.Background(), webhook.Push{Repo: "Octo/Web", Ref: "refs/heads/main", SHA: "aaaaaaa1111", Message: "fix"})
	deps := pushDeployments(t, s, app.ID)
	if len(deps) != 1 || deps[0].Trigger != "push" || deps[0].CommitHash != "aaaaaaa1111" || deps[0].CommitMessage != "fix" {
		t.Fatalf("expected one push deployment of the commit, got %+v", deps)
	}
	waitForStatus(t, s, deps[0].ID, "active")
	if a := lastActivity(t, s, "push_deploy"); !strings.Contains(a, "Push to main (aaaaaaa) queued release r1 for shop") {
		t.Fatalf("activity: %q", a)
	}
}

func TestHandlePush_IgnoredPushes_APP8(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(a *store.App)
		push   webhook.Push
		reason string
	}{
		{"other branch", nil, webhook.Push{Ref: "refs/heads/dev", SHA: "b1"}, "Push to dev (b1) ignored for shop: the app deploys from main."},
		{"tag", nil, webhook.Push{Ref: "refs/tags/v1", SHA: "b2"}, "Push to refs/tags/v1 ignored for shop: the app deploys from main."},
		{"branch deleted", nil, webhook.Push{Ref: "refs/heads/main", SHA: "0000000000", Deleted: true}, "the branch was deleted"},
		{"auto-deploy off", func(a *store.App) { a.AutoDeploy = false }, webhook.Push{Ref: "refs/heads/main", SHA: "b3"}, "deploy on push is off"},
		{"no matching path", func(a *store.App) { a.WatchPaths = "web/" }, webhook.Push{Ref: "refs/heads/main", SHA: "b4", Files: []string{"api/x.go"}}, "no changed file matches the watch paths"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, eng, app := pushTestEngine(t)
			if c.setup != nil {
				c.setup(app)
				_ = s.UpdateApp(context.Background(), app)
			}
			c.push.Repo = "octo/web"
			eng.HandlePush(context.Background(), c.push)
			if deps := pushDeployments(t, s, app.ID); len(deps) != 0 {
				t.Fatalf("expected no deployment, got %+v", deps)
			}
			if a := lastActivity(t, s, "push_ignored"); !strings.Contains(a, c.reason) {
				t.Fatalf("activity %q does not contain %q", a, c.reason)
			}
		})
	}
}

// Negative control for the cases above: the same pushes, adjusted to match, do deploy.
func TestHandlePush_MatchingPathOrNoFileListDeploys_APP8(t *testing.T) {
	s, eng, app := pushTestEngine(t)
	app.WatchPaths = "web/"
	_ = s.UpdateApp(context.Background(), app)
	eng.HandlePush(context.Background(), webhook.Push{Repo: "octo/web", Ref: "refs/heads/main", SHA: "c1", Files: []string{"api/x.go", "web/a.ts"}})
	deps := pushDeployments(t, s, app.ID)
	if len(deps) != 1 {
		t.Fatalf("matching file: expected 1 deployment, got %d", len(deps))
	}
	waitForStatus(t, s, deps[0].ID, "active")
	waitIdle(t, eng, app.ID)
	// APP-8: a push that lists no files deploys anyway.
	eng.HandlePush(context.Background(), webhook.Push{Repo: "octo/web", Ref: "refs/heads/main", SHA: "c2"})
	if deps := pushDeployments(t, s, app.ID); len(deps) != 2 {
		t.Fatalf("no file list: expected 2 deployments, got %d", len(deps))
	}
}

func TestHandlePush_UnknownRepoRecordsNothing(t *testing.T) {
	s, eng, app := pushTestEngine(t)
	eng.HandlePush(context.Background(), webhook.Push{Repo: "octo/other", Ref: "refs/heads/main", SHA: "d1"})
	if len(pushDeployments(t, s, app.ID)) != 0 || lastActivity(t, s, "push_ignored") != "" {
		t.Fatal("a push to a repository no app uses must leave no trace")
	}
}

// A replayed or repeated push of the commit that is live is ignored; Redeploy is the way to
// deploy it again. Another commit (or the old one again once it is no longer live) deploys.
func TestHandlePush_SameCommitDeduped_APP8(t *testing.T) {
	s, eng, app := pushTestEngine(t)
	push := webhook.Push{Repo: "octo/web", Ref: "refs/heads/main", SHA: "e1", DeliveryID: "d-1"}
	eng.HandlePush(context.Background(), push)
	push.DeliveryID = "d-2" // same signed body under a new delivery ID
	eng.HandlePush(context.Background(), push)
	deps := pushDeployments(t, s, app.ID)
	if len(deps) != 1 {
		t.Fatalf("expected the second delivery of e1 to be ignored, got %d deployments", len(deps))
	}
	if a := lastActivity(t, s, "push_ignored"); !strings.Contains(a, "already queued, building or live") {
		t.Fatalf("activity: %q", a)
	}
	waitForStatus(t, s, deps[0].ID, "active")
	waitIdle(t, eng, app.ID)
	eng.HandlePush(context.Background(), webhook.Push{Repo: "octo/web", Ref: "refs/heads/main", SHA: "e2"})
	waitForStatus(t, s, pushDeployments(t, s, app.ID)[0].ID, "active")
	waitIdle(t, eng, app.ID)
	eng.HandlePush(context.Background(), webhook.Push{Repo: "octo/web", Ref: "refs/heads/main", SHA: "e1"}) // force-push back
	if deps := pushDeployments(t, s, app.ID); len(deps) != 3 || deps[0].CommitHash != "e1" {
		t.Fatalf("expected e1 to deploy again once not live, got %+v", deps)
	}
}

// DEP-5 example: A builds, push B queues, push C supersedes B, A keeps building, then C builds.
func TestHandlePush_QueueKeepsNewest_DEP5(t *testing.T) {
	s, fh, eng, key := setupTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = eng.Start(ctx)
	app := newTestApp(t, s, key, "shop", nil)
	app.AutoDeploy = true
	_ = s.UpdateApp(ctx, app)

	started, release := make(chan struct{}), make(chan struct{})
	fh.StreamMock = func(ctx context.Context, env []string, onLine func(string), name string, args ...string) error {
		select {
		case started <- struct{}{}:
			<-release
		default:
		}
		return nil
	}
	go eng.HandlePush(ctx, webhook.Push{Repo: "octo/web", Ref: "refs/heads/main", SHA: "A"})
	<-started // A is building
	eng.HandlePush(ctx, webhook.Push{Repo: "octo/web", Ref: "refs/heads/main", SHA: "B"})
	eng.HandlePush(ctx, webhook.Push{Repo: "octo/web", Ref: "refs/heads/main", SHA: "C"})
	byCommit := func() map[string]store.Deployment {
		m := map[string]store.Deployment{}
		for _, d := range pushDeployments(t, s, app.ID) {
			m[d.CommitHash] = d
		}
		return m
	}
	m := byCommit()
	if m["A"].Status != "building" || m["B"].Status != "superseded" || m["C"].Status != "queued" {
		t.Fatalf("while A builds: A=%s B=%s C=%s", m["A"].Status, m["B"].Status, m["C"].Status)
	}
	close(release) // later builds take the mock's default branch and finish at once
	waitForStatus(t, s, m["C"].ID, "active")
	if got, _ := s.GetDeployment(ctx, m["A"].ID); got.Status != "inactive" {
		t.Fatalf("A should have gone live before C replaced it, got %s", got.Status)
	}
}
