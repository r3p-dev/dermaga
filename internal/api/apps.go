package api

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/apps"
	"github.com/r3p-dev/dermaga/internal/deploy"
	"github.com/r3p-dev/dermaga/internal/gitsrc"
	"github.com/r3p-dev/dermaga/internal/host"
	"github.com/r3p-dev/dermaga/internal/name"
	"github.com/r3p-dev/dermaga/internal/quadlet"
	"github.com/r3p-dev/dermaga/internal/spec"
	"github.com/r3p-dev/dermaga/internal/traefik"
)

// AppView is an app as the API shows it. It never contains secrets.
type AppView struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Domains  []spec.Domain   `json:"domains"`
	Spec     spec.App        `json:"spec"`
	Strategy string          `json:"strategy"` // effective
	Live     *apps.Release   `json:"live,omitempty"`
	Releases []apps.Release  `json:"releases,omitempty"`
	GitHub   *GitHubSettings `json:"github,omitempty"`
}

// GitHubSettings is what to set up in the repo (BRIEF §3.10 wizard, §5).
type GitHubSettings struct {
	DeployKey      string `json:"deployKey"`
	DeployKeySHA   string `json:"deployKeyFingerprint"`
	DeployKeysPage string `json:"deployKeysPage"`
	WebhookURL     string `json:"webhookUrl"`
	WebhooksPage   string `json:"webhooksPage"`
	WebhookSecret  string `json:"webhookSecret,omitempty"` // only on create and on explicit reveal
}

func view(a apps.App) AppView {
	return AppView{ID: a.ID, Name: a.Spec.Name, Domains: a.Spec.Domains, Spec: a.Spec, Strategy: a.Spec.EffectiveStrategy(), Live: a.Live}
}

// appRoutes registers the app API. wrap is identity on the socket and the
// session check on the panel; actor goes into the audit log.
func (s *Server) appRoutes(mux *http.ServeMux, wrap func(http.HandlerFunc) http.Handler, actor string) {
	h := func(f func(http.ResponseWriter, *http.Request, string)) http.Handler {
		return wrap(func(w http.ResponseWriter, r *http.Request) { f(w, r, actor) })
	}
	mux.Handle("GET /api/apps", h(s.listApps))
	mux.Handle("POST /api/apps", h(s.createApp))
	mux.Handle("GET /api/apps/{app}", h(s.getApp))
	mux.Handle("PUT /api/apps/{app}", h(s.putApp))
	mux.Handle("DELETE /api/apps/{app}", h(s.deleteApp))
	mux.Handle("POST /api/apps/{app}/deploy", h(s.deployApp))
	mux.Handle("POST /api/apps/{app}/rollback", h(s.rollbackApp))
	mux.Handle("GET /api/apps/{app}/releases/{rel8}/log", h(s.releaseLog))
	mux.Handle("GET /api/apps/{app}/logs", h(s.runtimeLogs))
	mux.Handle("GET /api/apps/{app}/env", h(s.getEnv))
	mux.Handle("PUT /api/apps/{app}/env", h(s.putEnv))
	mux.Handle("GET /api/apps/{app}/webhook", h(s.revealWebhook))
	mux.Handle("POST /api/github/known-hosts", h(s.knownHosts))
	mux.Handle("GET /api/settings", h(s.getSettings))
	mux.Handle("PUT /api/settings", h(s.putSettings))
}

func (s *Server) app(w http.ResponseWriter, r *http.Request) (apps.App, bool) {
	a, err := s.Apps.Get(r.Context(), r.PathValue("app"))
	if errors.Is(err, apps.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such app: "+r.PathValue("app"))
		return a, false
	}
	if err != nil {
		internalError(w, err)
		return a, false
	}
	return a, true
}

func (s *Server) listApps(w http.ResponseWriter, r *http.Request, _ string) {
	l, err := s.Apps.List(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	out := []AppView{}
	for _, a := range l {
		out = append(out, view(a))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) github(ctx context.Context, a apps.App, secret string) (*GitHubSettings, error) {
	if a.Spec.Source.Type != "git" {
		return nil, nil
	}
	key := s.Paths.DeployKey(a.Spec.Name)
	pub, err := gitsrc.DeployKey(ctx, key, name.Name+"-"+a.Spec.Name)
	if err != nil {
		return nil, err
	}
	fp, _ := gitsrc.Fingerprint(ctx, key+".pub")
	hook, err := s.Apps.HookHost(ctx)
	if err != nil {
		return nil, err
	}
	url := "(set the hook host first: " + name.Name + " settings --hook-host <host>)"
	if hook != "" {
		url = "https://" + hook + "/hooks/github/" + a.ID
	}
	repo := "https://github.com/" + a.Spec.Source.RepoFullName()
	return &GitHubSettings{DeployKey: pub, DeployKeySHA: fp, DeployKeysPage: repo + "/settings/keys/new",
		WebhookURL: url, WebhooksPage: repo + "/settings/hooks/new", WebhookSecret: secret}, nil
}

type appRequest struct {
	Name    string        `json:"name"`
	Domains []spec.Domain `json:"domains"`
	Spec    spec.App      `json:"spec"`
}

func (s *Server) createApp(w http.ResponseWriter, r *http.Request, actor string) {
	var req appRequest
	if !readJSON(w, r, &req) {
		return
	}
	a := req.Spec
	a.Name, a.Domains = req.Name, req.Domains
	a.Source.Repo = spec.NormalizeRepo(a.Source.Repo)
	id, secret, err := s.Apps.Create(r.Context(), a)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created := apps.App{ID: id, Spec: a}
	gh, err := s.github(r.Context(), created, secret)
	if err != nil {
		internalError(w, err)
		return
	}
	s.audit(actor, "app.create", a.Name)
	v := view(created)
	v.GitHub = gh
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) getApp(w http.ResponseWriter, r *http.Request, _ string) {
	a, ok := s.app(w, r)
	if !ok {
		return
	}
	v := view(a)
	var err error
	if v.Releases, err = s.Apps.Releases(r.Context(), a.ID, 10); err != nil {
		internalError(w, err)
		return
	}
	if v.GitHub, err = s.github(r.Context(), a, ""); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// putApp replaces an app's spec and domains (apps edit, domains, middleware)
// and re-renders its route at once if it is live.
func (s *Server) putApp(w http.ResponseWriter, r *http.Request, actor string) {
	var req appRequest
	if !readJSON(w, r, &req) {
		return
	}
	defer s.Engine.Lock(r.PathValue("app"))()
	a, ok := s.app(w, r)
	if !ok {
		return
	}
	n := req.Spec
	n.Name, n.Domains = a.Spec.Name, req.Domains
	n.Source.Repo = spec.NormalizeRepo(n.Source.Repo)
	if n.Source.Type != a.Spec.Source.Type || n.Source.Repo != a.Spec.Source.Repo {
		writeError(w, http.StatusBadRequest, "source type and repo can't change: create a new app")
		return
	}
	if err := s.Apps.Save(r.Context(), a.ID, n); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.Spec = n
	if err := s.Engine.RenderRoute(r.Context(), a); err != nil {
		internalError(w, err)
		return
	}
	s.audit(actor, "app.edit", a.Spec.Name)
	writeJSON(w, http.StatusOK, view(a))
}

func (s *Server) deleteApp(w http.ResponseWriter, r *http.Request, actor string) {
	purge := r.URL.Query().Get("purge") == "1"
	defer s.Engine.Lock(r.PathValue("app"))()
	a, ok := s.app(w, r)
	if !ok {
		return
	}
	s.Engine.Drop(a.Spec.Name)
	if err := s.Remove(r.Context(), a, purge); err != nil {
		internalError(w, err)
		return
	}
	if err := s.Apps.Remove(r.Context(), a.ID); err != nil {
		internalError(w, err)
		return
	}
	s.audit(actor, "app.remove", a.Spec.Name)
	w.WriteHeader(http.StatusNoContent)
}

// Remove takes an app off the host: its container, units, route, env file,
// deploy key, mirror, images and build logs; volumes only with purge.
func (s *Server) Remove(ctx context.Context, a apps.App, purge bool) error {
	h := host.Host{QuadletDir: s.Paths.QuadletDir, DynamicDir: s.Paths.DynamicDir()}
	n := a.Spec.Name
	if a.Live != nil {
		if err := h.RemoveContainer(ctx, quadlet.ContainerName(n, a.Live.Rel8), 10); err != nil {
			return err
		}
	}
	if err := h.RemoveAppFile(traefik.AppFileName(n)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, v := range a.Spec.Volumes {
		os.Remove(s.Paths.QuadletDir + "/" + quadlet.VolumeUnit(n, v.Name))
	}
	h.DaemonReload(ctx)
	if purge {
		for _, v := range a.Spec.Volumes {
			if _, err := host.Run(ctx, time.Minute, "podman", "volume", "rm", "--force", "--", strings.TrimSuffix(quadlet.VolumeUnit(n, v.Name), ".volume")); err != nil {
				return err
			}
		}
	}
	if ids, err := host.Run(ctx, time.Minute, "podman", "images", "--quiet", "--filter", "reference="+deploy.ImageRepo(n)); err == nil && ids != "" {
		host.Run(ctx, 2*time.Minute, append([]string{"podman", "rmi", "--force", "--"}, strings.Fields(ids)...)...)
	}
	for _, p := range []string{s.Paths.EnvFile(n), s.Paths.DeployKey(n), s.Paths.DeployKey(n) + ".pub"} {
		os.Remove(p)
	}
	os.RemoveAll(s.Paths.Mirror(n))
	os.RemoveAll(s.Paths.StateDir + "/builds/" + n)
	return nil
}

func (s *Server) deployApp(w http.ResponseWriter, r *http.Request, actor string) {
	var req struct {
		Ref string `json:"ref"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if _, ok := s.app(w, r); !ok {
		return
	}
	rel, err := s.Engine.Enqueue(r.Context(), r.PathValue("app"), "manual", req.Ref)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(actor, "deploy.queued", r.PathValue("app")+" "+rel.Rel8)
	writeJSON(w, http.StatusAccepted, rel)
}

func (s *Server) rollbackApp(w http.ResponseWriter, r *http.Request, actor string) {
	var req struct {
		To string `json:"to"`
	}
	readJSON(w, r, &req)
	a, ok := s.app(w, r)
	if !ok {
		return
	}
	target, err := s.Apps.RollbackTarget(r.Context(), a.ID, req.To)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rel, err := s.Engine.Enqueue(r.Context(), a.Spec.Name, "rollback", target.Rel8)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(actor, "rollback.queued", a.Spec.Name+" "+rel.Rel8+" -> "+target.Rel8)
	writeJSON(w, http.StatusAccepted, rel)
}

// sse prepares a server-sent event stream without the server's write timeout.
func sse(w http.ResponseWriter) func(string) error {
	http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flush := http.NewResponseController(w).Flush
	flush()
	return func(line string) error {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", line); err != nil {
			return err
		}
		return flush()
	}
}

func (s *Server) releaseLog(w http.ResponseWriter, r *http.Request, _ string) {
	a, ok := s.app(w, r)
	if !ok {
		return
	}
	rel, err := s.Apps.Release(r.Context(), a.ID, r.PathValue("rel8"))
	if err != nil {
		writeError(w, http.StatusNotFound, "no such release")
		return
	}
	path := s.Paths.BuildLog(a.Spec.Name, rel.Rel8)
	follow := r.URL.Query().Get("follow") == "1"
	done := func() bool {
		cur, err := s.Apps.Release(r.Context(), a.ID, rel.Rel8)
		return !follow || err != nil || apps.Terminal(cur.Status)
	}
	send := sse(w)
	if err := deploy.Follow(r.Context(), path, done, send); err != nil && !errors.Is(err, os.ErrNotExist) {
		send("error: " + err.Error())
	}
	cur, _ := s.Apps.Release(r.Context(), a.ID, rel.Rel8)
	fmt.Fprintf(w, "event: end\ndata: %s\n\n", cur.Status)
}

// runtimeLogs streams the live release's journal (BRIEF §3.9).
func (s *Server) runtimeLogs(w http.ResponseWriter, r *http.Request, _ string) {
	a, ok := s.app(w, r)
	if !ok {
		return
	}
	if a.Live == nil {
		writeError(w, http.StatusNotFound, "no live release")
		return
	}
	lines := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("lines")); err == nil && v > 0 && v <= 10000 {
		lines = v
	}
	args := []string{"--user", "-u", quadlet.ContainerName(a.Spec.Name, a.Live.Rel8) + ".service", "-o", "cat", "-n", strconv.Itoa(lines), "--no-pager"}
	if r.URL.Query().Get("follow") == "1" {
		args = append(args, "-f")
	}
	cmd := exec.CommandContext(r.Context(), "journalctl", args...)
	out, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		internalError(w, fmt.Errorf("journalctl: %v", err))
		return
	}
	send := sse(w)
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if send(sc.Text()) != nil {
			break
		}
	}
	cmd.Process.Kill()
	cmd.Wait()
}

func (s *Server) getEnv(w http.ResponseWriter, r *http.Request, actor string) {
	a, ok := s.app(w, r)
	if !ok {
		return
	}
	env, err := s.Apps.Env(r.Context(), a.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	if r.URL.Query().Get("reveal") == "1" {
		s.audit(actor, "env.reveal", a.Spec.Name)
		writeJSON(w, http.StatusOK, env)
		return
	}
	keys := []string{}
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	writeJSON(w, http.StatusOK, keys)
}

func (s *Server) putEnv(w http.ResponseWriter, r *http.Request, actor string) {
	var req struct {
		Set   map[string]string `json:"set"`
		Unset []string          `json:"unset"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	defer s.Engine.Lock(r.PathValue("app"))()
	a, ok := s.app(w, r)
	if !ok {
		return
	}
	if err := s.Apps.SetEnv(r.Context(), a.ID, req.Set, req.Unset); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	keys := make([]string, 0, len(req.Set))
	for k := range req.Set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s.audit(actor, "env.set", a.Spec.Name+" set "+strings.Join(keys, ",")+" unset "+strings.Join(req.Unset, ","))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) revealWebhook(w http.ResponseWriter, r *http.Request, actor string) {
	a, ok := s.app(w, r)
	if !ok {
		return
	}
	_, secret, err := s.Apps.ByID(r.Context(), a.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	gh, err := s.github(r.Context(), a, secret)
	if err != nil || gh == nil {
		writeError(w, http.StatusBadRequest, "not a git app")
		return
	}
	s.audit(actor, "webhook.reveal", a.Spec.Name)
	writeJSON(w, http.StatusOK, gh)
}

func (s *Server) knownHosts(w http.ResponseWriter, r *http.Request, actor string) {
	file, fps, err := gitsrc.KnownHosts(r.Context(), &http.Client{Timeout: 20 * time.Second}, gitsrc.MetaURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := os.MkdirAll(s.Paths.DataDir, 0o700); err != nil {
		internalError(w, err)
		return
	}
	if err := host.WriteFile(s.Paths.KnownHosts(), file, 0o600); err != nil {
		internalError(w, err)
		return
	}
	s.audit(actor, "github.known_hosts", "")
	writeJSON(w, http.StatusOK, map[string]any{"file": s.Paths.KnownHosts(), "fingerprints": fps})
}

type Settings struct {
	HookHost string `json:"hookHost"`
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request, _ string) {
	h, err := s.Apps.HookHost(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Settings{HookHost: h})
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request, actor string) {
	var req Settings
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.Apps.SetHookHost(r.Context(), req.HookHost); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.OnSettings != nil {
		if err := s.OnSettings(r.Context()); err != nil {
			internalError(w, err)
			return
		}
	}
	s.audit(actor, "settings.edit", "hookHost="+req.HookHost)
	writeJSON(w, http.StatusOK, req)
}
