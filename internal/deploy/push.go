package deploy

import (
	"context"
	"fmt"
	"strings"

	"dermaga/internal/webhook"
)

// HandlePush turns a verified GitHub push into deployments (APP-8, DEP-5): every app built from the
// pushed repository and branch, with auto-deploy on, whose watch paths match the changed files and
// that is not already queued, building or running that commit, gets a queued release. Every app that
// does not, records the plain reason in Activity. Pushes to repositories no app uses are not recorded.
func (e *Engine) HandlePush(ctx context.Context, p webhook.Push) {
	owner, repo, ok := strings.Cut(p.Repo, "/")
	if !ok {
		return
	}
	apps, err := e.store.ListAppsByRepo(ctx, owner, repo)
	if err != nil || len(apps) == 0 {
		return
	}

	branch, isBranch := strings.CutPrefix(p.Ref, "refs/heads/")
	shortSHA := p.SHA
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}
	what := fmt.Sprintf("Push to %s (%s)", branch, shortSHA)
	if !isBranch {
		what = fmt.Sprintf("Push to %s", p.Ref)
	}

	// Two deliveries of the same commit (a replayed body under a new delivery ID) must not both pass
	// the duplicate check before either is queued.
	e.pushMu.Lock()
	defer e.pushMu.Unlock()

	for _, app := range apps {
		ignore := func(reason string) {
			_ = e.store.RecordAudit(ctx, "webhook", "github", "push_ignored",
				fmt.Sprintf("%s ignored for %s: %s.", what, app.Name, reason), &app.ID,
				map[string]any{"ref": p.Ref, "commit": p.SHA, "delivery_id": p.DeliveryID})
		}
		switch {
		case !isBranch || branch != app.Branch:
			if p.Deleted {
				continue // another branch or a tag was deleted: nothing this app cares about
			}
			ignore(fmt.Sprintf("the app deploys from %s", app.Branch))
			continue
		case p.Deleted:
			ignore("the branch was deleted, the live release keeps running")
			continue
		case !app.AutoDeploy:
			ignore("deploy on push is off")
			continue
		// APP-8: a push that lists no files (or more commits than the payload carries) deploys anyway.
		case len(p.Files) > 0 && !watchPathsMatch(app.WatchPaths, p.Files):
			ignore("no changed file matches the watch paths")
			continue
		}
		dup, err := e.store.HasCurrentDeploymentOfCommit(ctx, app.ID, p.SHA)
		if err != nil {
			ignore("the deployment history could not be read")
			continue
		}
		if dup {
			ignore("this commit is already queued, building or live (use Redeploy to deploy it again)")
			continue
		}
		dep, err := e.Enqueue(ctx, app.ID, "push", p.SHA, p.Message)
		if err != nil {
			ignore("the deployment could not be queued: " + err.Error())
			continue
		}
		_ = e.store.RecordAudit(ctx, "webhook", "github", "push_deploy",
			fmt.Sprintf("%s queued release r%d for %s.", what, dep.ReleaseNumber, app.Name), &app.ID,
			map[string]any{"ref": p.Ref, "commit": p.SHA, "delivery_id": p.DeliveryID, "deployment_id": dep.ID})
	}
}
