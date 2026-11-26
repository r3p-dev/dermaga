package reconciler

import (
	"context"
	"fmt"
	"path/filepath"

	"dermaga/internal/builder"
	"dermaga/internal/deploy"
	"dermaga/internal/name"
	"dermaga/internal/quadlet"
	"dermaga/internal/store"
	"dermaga/internal/traefik"
)

// recoverSwitches finishes or undoes a route switch the daemon was in when it stopped (DEP-10).
// The route file on disk is what Traefik serves, so it decides:
//   - it sends traffic to a release the DB still has in "switching": the switch had reached the
//     main router, so the DB adopts that release as live (the old one becomes previous);
//   - it sends traffic to any other release the DB does not have as live: the DB wins and the
//     route is written back for the DB's live release;
//   - a verify router is left in it: the route is rewritten without it.
//
// The file is rewritten in place, never removed, so Traefik is never left without a route. The
// later steps then stop every release that is not live and start the live one.
func (r *Reconciler) recoverSwitches(ctx context.Context, report *Report) error {
	apps, err := r.store.ListApps(ctx)
	if err != nil {
		return fmt.Errorf("listing apps: %w", err)
	}
	for i := range apps {
		app := &apps[i]
		fname := name.AppDynamicFilename(app.Name)
		data, err := r.host.ReadFile(filepath.Join(r.cfg.TraefikDynamicDir, fname))
		if err != nil || !traefik.HasAppMarker(string(data), app.Name) {
			continue // missing: re-rendered later; foreign: reported later
		}
		fileLive, hasVerify, ok := traefik.LiveRelease(data, app.Name)
		if !ok {
			continue
		}
		dbLive := 0
		if app.ActiveReleaseID != nil {
			if d, err := r.store.GetDeployment(ctx, *app.ActiveReleaseID); err == nil {
				dbLive = d.ReleaseNumber
			}
		}
		rewrite := hasVerify
		if fileLive != dbLive {
			cand, err := r.store.FindDeploymentByRelease(ctx, app.ID, fileLive)
			if err == nil && cand.Status == "switching" {
				oldID, oldStatus := "", "inactive"
				if app.ActiveReleaseID != nil {
					oldID = *app.ActiveReleaseID
				}
				if cand.Trigger == "rollback" {
					oldStatus = "rolled_back"
				}
				if err := r.store.FinishSwitch(ctx, app.ID, cand.ID, oldID, oldStatus); err != nil {
					return fmt.Errorf("adopting r%d of %s: %w", fileLive, app.Name, err)
				}
				report.SwitchesRecovered = append(report.SwitchesRecovered, fmt.Sprintf("%s: r%d live (switch completed)", app.Name, fileLive))
			} else if dbLive > 0 {
				rewrite = true
				report.SwitchesRecovered = append(report.SwitchesRecovered, fmt.Sprintf("%s: r%d live (route reset from r%d)", app.Name, dbLive, fileLive))
			}
		}
		if !rewrite {
			continue
		}
		fresh, err := r.store.GetApp(ctx, app.ID)
		if err != nil || fresh.ActiveReleaseID == nil {
			continue
		}
		live, err := r.store.GetDeployment(ctx, *fresh.ActiveReleaseID)
		if err != nil {
			continue
		}
		domains, err := r.store.ListDomainsByApp(ctx, app.ID)
		if err != nil {
			return fmt.Errorf("listing domains of %s: %w", app.Name, err)
		}
		if err := r.writeAppRoute(store.ActiveAppRecord{App: *fresh, Deployment: *live, Domains: domains}); err != nil {
			return err
		}
		if hasVerify {
			report.VerifyRoutersCleaned = append(report.VerifyRoutersCleaned, fname)
		}
	}
	return nil
}

// writeAppRoute renders an app's route for its live release (port from that release's config).
func (r *Reconciler) writeAppRoute(rec store.ActiveAppRecord) error {
	rc, err := deploy.ReleaseRuntimeConfig(&rec.App, &rec.Deployment)
	if err != nil {
		return err
	}
	domSpecs := make([]traefik.DomainSpec, 0, len(rec.Domains))
	for _, d := range rec.Domains {
		var redir, pfx string
		if d.RedirectTo != nil {
			redir = *d.RedirectTo
		}
		if d.PathPrefix != nil {
			pfx = *d.PathPrefix
		}
		domSpecs = append(domSpecs, traefik.DomainSpec{Domain: d.Domain, IsPrimary: d.IsPrimary, RedirectTo: redir, PathPrefix: pfx, StripPrefix: d.StripPrefix})
	}
	data, err := traefik.RenderApp(traefik.AppSpec{AppName: rec.App.Name, ReleaseNumber: rec.Deployment.ReleaseNumber, Port: rc.Port, Domains: domSpecs})
	if err != nil {
		return fmt.Errorf("rendering traefik config for %s: %w", rec.App.Name, err)
	}
	path := filepath.Join(r.cfg.TraefikDynamicDir, name.AppDynamicFilename(rec.App.Name))
	if err := r.host.WriteFileAtomic(path, data, 0600); err != nil {
		return fmt.Errorf("writing traefik config for %s: %w", rec.App.Name, err)
	}
	return nil
}

// unitSpecFor builds a live release's unit exactly as the deploy engine does (image, limits,
// health, and the env file when it is still on disk).
func (r *Reconciler) unitSpecFor(rec store.ActiveAppRecord) (quadlet.ContainerSpec, error) {
	rc, err := deploy.ReleaseRuntimeConfig(&rec.App, &rec.Deployment)
	if err != nil {
		return quadlet.ContainerSpec{}, err
	}
	image := rec.Deployment.ImageRef
	if image == "" {
		image = builder.ImageRef(rec.App.Name, rec.Deployment.ReleaseNumber)
	}
	envFile := ""
	if r.cfg.EnvDir != "" {
		p := deploy.EnvFilePath(r.cfg.EnvDir, rec.App.Name, rec.Deployment.ReleaseNumber)
		if _, err := r.host.Stat(p); err == nil {
			envFile = p
		}
	}
	return deploy.ContainerSpecFor(rec.App.Name, rec.Deployment.ReleaseNumber, image, rc, envFile), nil
}
