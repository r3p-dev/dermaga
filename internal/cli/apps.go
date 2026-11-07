package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/r3p-dev/dermaga/internal/api"
	"github.com/r3p-dev/dermaga/internal/apps"
	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/name"
	"github.com/r3p-dev/dermaga/internal/spec"
	"github.com/r3p-dev/dermaga/internal/traefik"
)

// list is a repeatable string flag.
type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }

// sub splits "<name> [args]" for commands that take an app name first.
func sub(cmd string, args []string) (string, []string, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(os.Stderr, "usage: %s %s <app> …\n", name.Name, cmd)
		return "", nil, errQuiet
	}
	return args[0], args[1:], nil
}

func cmdApps(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		return appsList(args[1:])
	case "show":
		return appsShow(args[1:])
	case "create":
		return appsCreate(args[1:])
	case "edit":
		return appsEdit(args[1:])
	case "remove":
		return appsRemove(args[1:])
	case "webhook":
		return appsWebhook(args[1:])
	}
	fmt.Fprintln(os.Stderr, "usage: "+name.Name+" apps [list | show <app> | create <app> … | edit <app> | remove <app> [--purge] | webhook <app>]")
	return errQuiet
}

func appsList(args []string) error {
	fs := flag.NewFlagSet("apps list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parse(fs, args); err != nil {
		return err
	}
	var l []api.AppView
	if err := call("GET", "/api/apps", nil, &l); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(l)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSOURCE\tDOMAIN\tSTRATEGY\tLIVE")
	for _, a := range l {
		src := a.Spec.Source.Image
		if a.Spec.Source.Type == "git" {
			src = a.Spec.Source.RepoFullName() + "@" + a.Spec.Source.Branch
		}
		live := "-"
		if a.Live != nil {
			live = a.Live.Rel8 + " " + short(a.Live.SHA) + " " + a.Live.FinishedAt.Format(time.DateTime)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", a.Name, src, a.Spec.Primary(), a.Strategy, live)
	}
	return tw.Flush()
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func appsShow(args []string) error {
	app, rest, err := sub("apps show", args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("apps show", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parse(fs, rest); err != nil {
		return err
	}
	var v api.AppView
	if err := call("GET", "/api/apps/"+url.PathEscape(app), nil, &v); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(v)
	}
	s := v.Spec
	fmt.Printf("%s (id %s)\n", v.Name, v.ID)
	if s.Source.Type == "git" {
		fmt.Printf("  source    %s, branch %s, auto-deploy %s", s.Source.Repo, s.Source.Branch, yesNo(s.Source.AutoDeploy, "on", "off"))
		if s.Source.Trigger == "workflow_run" {
			fmt.Printf(" after workflow %q succeeds", s.Source.Workflow)
		}
		fmt.Println()
	} else {
		fmt.Printf("  source    image %s\n", s.Source.Image)
	}
	for _, d := range s.Domains {
		kind := "alias"
		switch {
		case d.Primary:
			kind = "primary"
		case d.Redirect:
			kind = "redirects to primary"
		}
		fmt.Printf("  domain    %s (%s)\n", d.Host, kind)
	}
	health := "none (weak: only 'running without restarts')"
	switch {
	case s.Health.Path != "":
		health = "HTTP GET " + s.Health.Path
	case s.Health.Cmd != "":
		health = "command " + s.Health.Cmd
	}
	fmt.Printf("  port      %d, health %s, strategy %s (recreate until M4)\n", s.Port, health, v.Strategy)
	lim := s.Limits.Effective()
	fmt.Printf("  limits    memory %s, cpus %s, pids %d\n", lim.Memory, lim.CPUs, lim.Pids)
	if gh := v.GitHub; gh != nil {
		fmt.Printf("  deploy key %s (%s)\n  webhook   %s\n", gh.DeployKeySHA, gh.DeployKeysPage, gh.WebhookURL)
	}
	if len(v.Releases) > 0 {
		fmt.Println("\nRELEASE   STATUS      TRIGGER  COMMIT        CREATED              MESSAGE / ERROR")
		for _, r := range v.Releases {
			msg := r.Message
			if r.Error != "" {
				msg = r.Error
			}
			if len(msg) > 70 {
				msg = msg[:70] + "…"
			}
			fmt.Printf("%-9s %-11s %-8s %-13s %-20s %s\n", r.Rel8, r.Status, r.Trigger, short(r.SHA), r.CreatedAt.Format(time.DateTime), msg)
		}
	}
	return nil
}

func appsCreate(args []string) error {
	app, rest, err := sub("apps create", args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("apps create", flag.ContinueOnError)
	var aliases, redirects, volumes, caps, watch list
	repo := fs.String("repo", "", "GitHub repo (owner/repo, https://github.com/owner/repo or git@github.com:owner/repo.git)")
	branch := fs.String("branch", "main", "branch to deploy")
	image := fs.String("image", "", "deploy this pinned image instead of building a repo")
	port := fs.Int("port", 0, "port the app listens on (required)")
	domain := fs.String("domain", "", "primary domain (required)")
	fs.Var(&aliases, "alias", "extra domain served as well (repeatable)")
	fs.Var(&redirects, "redirect", "extra domain that 301-redirects to the primary, e.g. www (repeatable)")
	healthPath := fs.String("health-path", "", "HTTP health path, e.g. /healthz (recommended)")
	healthCmd := fs.String("health-cmd", "", "health command run inside the container")
	context := fs.String("context", "", "build context in the repo")
	containerfile := fs.String("containerfile", "", "Containerfile relative to the context")
	fs.Var(&volumes, "volume", "named volume NAME:/path (repeatable; makes recreate the default)")
	strategy := fs.String("strategy", "", "recreate or blue-green (default: recreate with volumes)")
	noAuto := fs.Bool("no-auto-deploy", false, "don't deploy on pushes")
	workflow := fs.String("workflow", "", "deploy only after this GitHub Actions workflow succeeds (CI gate)")
	fs.Var(&watch, "watch", "deploy only if a push touches a matching path, e.g. src/** (repeatable)")
	memory := fs.String("memory", "", "memory limit, e.g. 512m")
	cpus := fs.String("cpus", "", "CPU limit, e.g. 1 or 0.5")
	readOnly := fs.Bool("read-only", false, "read-only root filesystem")
	fs.Var(&caps, "cap", "capability to keep after dropping all, e.g. CHOWN (repeatable)")
	if err := parse(fs, rest); err != nil {
		return err
	}
	a := spec.App{Port: *port, Strategy: *strategy, ReadOnly: *readOnly, AddCaps: caps,
		Health: spec.Health{Path: *healthPath, Cmd: *healthCmd}, Limits: spec.Limits{Memory: *memory, CPUs: *cpus}}
	if *image != "" {
		a.Source = spec.Source{Type: "image", Image: *image}
	} else {
		a.Source = spec.Source{Type: "git", Repo: *repo, Branch: *branch, Context: *context, Containerfile: *containerfile,
			AutoDeploy: !*noAuto, WatchPaths: watch}
		if *workflow != "" {
			a.Source.Trigger, a.Source.Workflow = "workflow_run", *workflow
		}
	}
	for _, v := range volumes {
		n, p, _ := strings.Cut(v, ":")
		a.Volumes = append(a.Volumes, spec.Volume{Name: n, Path: p})
	}
	domains := []spec.Domain{{Host: *domain, Primary: true}}
	for _, d := range aliases {
		domains = append(domains, spec.Domain{Host: d})
	}
	for _, d := range redirects {
		domains = append(domains, spec.Domain{Host: d, Redirect: true})
	}
	var v api.AppView
	if err := call("POST", "/api/apps", map[string]any{"name": app, "domains": domains, "spec": a}, &v); err != nil {
		return err
	}
	fmt.Printf("Created %s (id %s).\n", v.Name, v.ID)
	if gh := v.GitHub; gh != nil {
		printGitHub(gh)
	}
	fmt.Printf("\nDeploy with: %s deploy %s\n", name.Name, v.Name)
	return nil
}

func printGitHub(gh *api.GitHubSettings) {
	fmt.Printf(`
1. Add this read-only deploy key to the repo (do NOT tick "Allow write access"):
     %s
   Page:        %s
   Fingerprint: %s

2. Add a webhook:
     Payload URL:  %s
     Content type: application/json
     Secret:       %s
     Events:       "Just the push event" (CI gate: "Let me select individual events" → Workflow runs)
   Page: %s
`, gh.DeployKey, gh.DeployKeysPage, gh.DeployKeySHA, gh.WebhookURL, yesNo(gh.WebhookSecret != "", gh.WebhookSecret, "(apps webhook <app> shows it)"), gh.WebhooksPage)
}

func appsWebhook(args []string) error {
	app, rest, err := sub("apps webhook", args)
	if err != nil {
		return err
	}
	if err := parse(flag.NewFlagSet("apps webhook", flag.ContinueOnError), rest); err != nil {
		return err
	}
	var gh api.GitHubSettings
	if err := call("GET", "/api/apps/"+url.PathEscape(app)+"/webhook", nil, &gh); err != nil {
		return err
	}
	printGitHub(&gh)
	return nil
}

// appsEdit opens the app's settings as JSON in $EDITOR (or reads --file),
// then saves them. Domains and middleware are part of it.
func appsEdit(args []string) error {
	app, rest, err := sub("apps edit", args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("apps edit", flag.ContinueOnError)
	file := fs.String("file", "", "read the new settings from this JSON file instead of opening $EDITOR")
	if err := parse(fs, rest); err != nil {
		return err
	}
	var v api.AppView
	if err := call("GET", "/api/apps/"+url.PathEscape(app), nil, &v); err != nil {
		return err
	}
	var b []byte
	if *file != "" {
		if b, err = os.ReadFile(*file); err != nil {
			return err
		}
	} else {
		cur, _ := json.MarshalIndent(map[string]any{"domains": v.Domains, "spec": v.Spec}, "", "  ")
		if b, err = editText(cur, "json"); err != nil {
			return err
		}
	}
	var req struct {
		Domains []spec.Domain `json:"domains"`
		Spec    spec.App      `json:"spec"`
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return saveApp(app, req.Domains, req.Spec)
}

func saveApp(app string, domains []spec.Domain, s spec.App) error {
	if err := call("PUT", "/api/apps/"+url.PathEscape(app), map[string]any{"domains": domains, "spec": s}, nil); err != nil {
		return err
	}
	fmt.Printf("Saved %s. Routing changes are live now; other changes apply at the next deploy.\n", app)
	return nil
}

func editText(cur []byte, ext string) ([]byte, error) {
	f, err := os.CreateTemp("", name.Name+"-*."+ext)
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(cur); err != nil {
		return nil, err
	}
	f.Close()
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, f.Name()) // the user's own editor on the user's own terminal
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return os.ReadFile(f.Name())
}

func appsRemove(args []string) error {
	app, rest, err := sub("apps remove", args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("apps remove", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "also delete the app's named volumes (data!)")
	yes := fs.Bool("yes", false, "don't ask for confirmation")
	if err := parse(fs, rest); err != nil {
		return err
	}
	if !*yes {
		what := "its container, route, env vars, deploy key and images"
		if *purge {
			what += " AND ITS VOLUMES (data)"
		}
		ans, err := readLine(fmt.Sprintf("This removes %s: %s.\nType the app name to confirm: ", app, what))
		if err != nil || ans != app {
			return errors.New("not confirmed")
		}
	}
	q := ""
	if *purge {
		q = "?purge=1"
	}
	if err := call("DELETE", "/api/apps/"+url.PathEscape(app)+q, nil, nil); err != nil {
		return err
	}
	fmt.Printf("Removed %s.\n", app)
	return nil
}

func cmdDeploy(args []string) error {
	app, rest, err := sub("deploy", args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	ref := fs.String("ref", "", "commit SHA to deploy (default: the branch head)")
	noFollow := fs.Bool("no-follow", false, "return once queued instead of streaming the log")
	if err := parse(fs, rest); err != nil {
		return err
	}
	var r apps.Release
	if err := call("POST", "/api/apps/"+url.PathEscape(app)+"/deploy", map[string]string{"ref": *ref}, &r); err != nil {
		return err
	}
	fmt.Printf("Queued release %s of %s.\n", r.Rel8, app)
	if *noFollow {
		return nil
	}
	status, err := stream("/api/apps/"+url.PathEscape(app)+"/releases/"+r.Rel8+"/log?follow=1", func(l string) { fmt.Println(l) })
	if err != nil {
		return err
	}
	if status != "live" {
		fmt.Fprintf(os.Stderr, "release %s: %s\n", r.Rel8, status)
		return errQuiet
	}
	return nil
}

func cmdLogs(args []string) error {
	app, rest, err := sub("logs", args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	follow := fs.Bool("f", false, "follow")
	lines := fs.Int("n", 100, "runtime log lines to show first")
	build := fs.String("build", "", `show a release's deploy log ("latest" or a release id)`)
	if err := parse(fs, rest); err != nil {
		return err
	}
	base := "/api/apps/" + url.PathEscape(app)
	f := ""
	if *follow {
		f = "follow=1"
	}
	if *build != "" {
		rel := *build
		if rel == "latest" {
			var v api.AppView
			if err := call("GET", base, nil, &v); err != nil {
				return err
			}
			if len(v.Releases) == 0 {
				return errors.New("no releases yet")
			}
			rel = v.Releases[0].Rel8
		}
		_, err := stream(base+"/releases/"+url.PathEscape(rel)+"/log?"+f, func(l string) { fmt.Println(l) })
		return err
	}
	_, err = stream(fmt.Sprintf("%s/logs?lines=%d&%s", base, *lines, f), func(l string) { fmt.Println(l) })
	return err
}

// stream reads a server-sent event stream; it returns the data of the final "end" event.
func stream(path string, line func(string)) (string, error) {
	p, err := config.DefaultPaths()
	if err != nil {
		return "", err
	}
	c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", p.Socket())
	}}}
	resp, err := c.Get("http://" + name.Name + path)
	if err != nil {
		return "", fmt.Errorf("daemon not reachable at %s: %w", p.Socket(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&e)
		return "", fmt.Errorf("%s", yesNo(e.Error != "", e.Error, resp.Status))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	end := false
	for sc.Scan() {
		t := sc.Text()
		switch {
		case t == "event: end":
			end = true
		case strings.HasPrefix(t, "data: "):
			if end {
				return strings.TrimPrefix(t, "data: "), nil
			}
			line(strings.TrimPrefix(t, "data: "))
		}
	}
	return "", sc.Err()
}

func cmdEnv(args []string) error {
	app, rest, err := sub("env", args)
	if err != nil {
		return err
	}
	base := "/api/apps/" + url.PathEscape(app) + "/env"
	op := "list"
	if len(rest) > 0 {
		op, rest = rest[0], rest[1:]
	}
	switch op {
	case "list":
		fs := flag.NewFlagSet("env list", flag.ContinueOnError)
		reveal := fs.Bool("reveal", false, "show values (recorded in the audit log)")
		if err := parse(fs, rest); err != nil {
			return err
		}
		if !*reveal {
			var keys []string
			if err := call("GET", base, nil, &keys); err != nil {
				return err
			}
			for _, k := range keys {
				fmt.Println(k)
			}
			return nil
		}
		var env map[string]string
		if err := call("GET", base+"?reveal=1", nil, &env); err != nil {
			return err
		}
		os.Stdout.Write(apps.EnvFile(env))
		return nil
	case "set", "unset":
		if len(rest) == 0 {
			return fmt.Errorf("usage: %s env %s %s", name.Name, app, yesNo(op == "set", "set KEY=VALUE…", "unset KEY…"))
		}
		req := map[string]any{"set": map[string]string{}, "unset": []string{}}
		for _, a := range rest {
			if op == "unset" {
				req["unset"] = append(req["unset"].([]string), a)
				continue
			}
			k, v, ok := strings.Cut(a, "=")
			if !ok {
				return fmt.Errorf("%q: use KEY=VALUE", a)
			}
			req["set"].(map[string]string)[k] = v
		}
		if err := call("PUT", base, req, nil); err != nil {
			return err
		}
		fmt.Printf("Saved. Redeploy %s to apply: %s deploy %s\n", app, name.Name, app)
		return nil
	case "edit":
		var env map[string]string
		if err := call("GET", base+"?reveal=1", nil, &env); err != nil {
			return err
		}
		b, err := editText(apps.EnvFile(env), "env")
		if err != nil {
			return err
		}
		set, unset := map[string]string{}, []string{}
		for _, l := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			k, v, ok := strings.Cut(l, "=")
			if !ok {
				return fmt.Errorf("line %q: use KEY=VALUE", l)
			}
			set[k] = v
		}
		for k := range env {
			if _, ok := set[k]; !ok {
				unset = append(unset, k)
			}
		}
		if err := call("PUT", base, map[string]any{"set": set, "unset": unset}, nil); err != nil {
			return err
		}
		fmt.Printf("Saved. Redeploy %s to apply: %s deploy %s\n", app, name.Name, app)
		return nil
	}
	return fmt.Errorf("usage: %s env <app> [list [--reveal] | set KEY=VALUE… | unset KEY… | edit]", name.Name)
}

func getApp(app string) (api.AppView, error) {
	var v api.AppView
	err := call("GET", "/api/apps/"+url.PathEscape(app), nil, &v)
	return v, err
}

func cmdDomains(args []string) error {
	app, rest, err := sub("domains", args)
	if err != nil {
		return err
	}
	v, err := getApp(app)
	if err != nil {
		return err
	}
	op, host := "list", ""
	if len(rest) > 0 {
		op = rest[0]
	}
	if op != "list" {
		fs := flag.NewFlagSet("domains "+op, flag.ContinueOnError)
		redirect := fs.Bool("redirect", false, "301-redirect this domain to the primary")
		if len(rest) < 2 {
			return fmt.Errorf("usage: %s domains %s [add HOST [--redirect] | remove HOST | primary HOST]", name.Name, app)
		}
		host = rest[1]
		if err := parse(fs, rest[2:]); err != nil {
			return err
		}
		ds := v.Domains
		switch op {
		case "add":
			ds = append(ds, spec.Domain{Host: host, Redirect: *redirect})
		case "remove":
			ds = slices.DeleteFunc(ds, func(d spec.Domain) bool { return d.Host == host })
		case "primary":
			found := false
			for i := range ds {
				ds[i].Primary = ds[i].Host == host
				if ds[i].Primary {
					ds[i].Redirect, found = false, true
				}
			}
			if !found {
				return fmt.Errorf("%s is not a domain of %s", host, app)
			}
		default:
			return fmt.Errorf("unknown: domains %s", op)
		}
		if err := saveApp(app, ds, v.Spec); err != nil {
			return err
		}
		v.Domains = ds
	}
	for _, d := range v.Domains {
		fmt.Printf("%s%s\n", d.Host, yesNo(d.Primary, " (primary)", yesNo(d.Redirect, " (redirects)", "")))
	}
	return nil
}

func cmdMiddleware(args []string) error {
	app, rest, err := sub("middleware", args)
	if err != nil {
		return err
	}
	v, err := getApp(app)
	if err != nil {
		return err
	}
	if len(rest) >= 3 && rest[0] == "basic-auth" {
		user, mw := rest[2], &v.Spec.Middleware
		switch rest[1] {
		case "add":
			pw, err := readSecret("Password for " + user + ": ")
			if err != nil {
				return err
			}
			if len(pw) < 12 {
				return errors.New("use at least 12 characters")
			}
			// cost 10: Traefik checks the hash on every request, so higher costs show up as latency
			h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
			if err != nil {
				return err
			}
			mw.BasicAuth = slices.DeleteFunc(mw.BasicAuth, func(u spec.BasicAuthUser) bool { return u.User == user })
			mw.BasicAuth = append(mw.BasicAuth, spec.BasicAuthUser{User: user, Hash: string(h)})
		case "remove":
			mw.BasicAuth = slices.DeleteFunc(mw.BasicAuth, func(u spec.BasicAuthUser) bool { return u.User == user })
		default:
			return fmt.Errorf("usage: %s middleware %s basic-auth add|remove USER", name.Name, app)
		}
		return saveApp(app, v.Domains, v.Spec)
	}
	if len(rest) > 0 && rest[0] != "show" {
		return fmt.Errorf("usage: %s middleware %s [show | basic-auth add|remove USER] (everything else: %s apps edit %s)", name.Name, app, name.Name, app)
	}
	mw := v.Spec.Middleware
	users := []string{}
	for _, u := range mw.BasicAuth {
		users = append(users, u.User)
	}
	rl := "default"
	switch {
	case mw.NoRateLimit:
		rl = "off"
	case mw.RateLimit != nil:
		rl = fmt.Sprintf("%d/s, burst %d", mw.RateLimit.Average, mw.RateLimit.Burst)
	}
	fmt.Printf("headers:     %s\n", yesNo(mw.Headers.Off, "defaults off", "defaults on"))
	hs := mw.Headers.EffectiveHeaders()
	keys := make([]string, 0, len(hs))
	for k := range hs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Printf("  %s: %s\n", k, yesNo(hs[k] == "", "(removed from responses)", hs[k]))
	}
	fmt.Printf("compression: %s\nrate limit:  %s\nbasic auth:  %s\nallowlist:   %s\nmax body:    %d bytes (0 = no limit)\npath prefix: %s (strip %v)\n",
		yesNo(mw.NoCompress, "off", "on"), rl, strings.Join(users, ", "), strings.Join(mw.IPAllowList, ", "), mw.MaxBodyBytes, mw.PathPrefix, mw.StripPrefix)
	return nil
}

func cmdGitHub(args []string) error {
	if len(args) != 1 || args[0] != "known-hosts" {
		return fmt.Errorf("usage: %s github known-hosts", name.Name)
	}
	var out struct {
		File         string            `json:"file"`
		Fingerprints map[string]string `json:"fingerprints"`
	}
	if err := call("POST", "/api/github/known-hosts", nil, &out); err != nil {
		return err
	}
	fmt.Printf("Pinned GitHub's SSH host keys in %s (from %s).\nCompare with https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints:\n", out.File, "api.github.com/meta")
	keys := make([]string, 0, len(out.Fingerprints))
	for k := range out.Fingerprints {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Printf("  %-15s SHA256:%s\n", k, out.Fingerprints[k])
	}
	return nil
}

func cmdSettings(args []string) error {
	fs := flag.NewFlagSet("settings", flag.ContinueOnError)
	hook := fs.String("hook-host", "", "public host for GitHub webhooks, e.g. hooks.example.com")
	if err := parse(fs, args); err != nil {
		return err
	}
	var s api.Settings
	if err := call("GET", "/api/settings", nil, &s); err != nil {
		return err
	}
	if *hook != "" {
		s.HookHost = *hook
		if err := call("PUT", "/api/settings", s, &s); err != nil {
			return err
		}
	}
	fmt.Printf("hook host: %s\n", yesNo(s.HookHost != "", s.HookHost, "(not set)"))
	return nil
}

// cmdTraefik prints the static traefik.yml (the installer writes it, M8).
func cmdTraefik(args []string) error {
	if len(args) == 0 || args[0] != "static" {
		return fmt.Errorf("usage: %s traefik static [--acme-email E] [--acme-staging] [--trusted-proxy CIDR]…", name.Name)
	}
	fs := flag.NewFlagSet("traefik static", flag.ContinueOnError)
	var proxies list
	email := fs.String("acme-email", "", "Let's Encrypt account email")
	staging := fs.Bool("acme-staging", false, "use Let's Encrypt's staging CA")
	fs.Var(&proxies, "trusted-proxy", "CIDR of a proxy/CDN in front (repeatable)")
	if err := parse(fs, args[1:]); err != nil {
		return err
	}
	b, err := traefik.StaticConfig(traefik.Site{ACMEEmail: *email, ACMEStaging: *staging, TrustedProxies: proxies})
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(b)
	return err
}
