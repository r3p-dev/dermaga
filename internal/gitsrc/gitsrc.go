// Package gitsrc talks to GitHub for git apps (BRIEF §3.5 step 3, §5): per-app
// ed25519 deploy keys, GitHub's pinned SSH host keys, bare mirrors fetched
// over SSH with the deploy key, and exporting one commit into a build dir.
// Every command is an argv slice with a timeout (host.Run).
package gitsrc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/host"
	"github.com/r3p-dev/dermaga/internal/name"
)

const (
	fetchTimeout = 5 * time.Minute
	cmdTimeout   = 30 * time.Second
)

// plain absolute paths: GIT_SSH_COMMAND goes through git's shell
var safePathRE = regexp.MustCompile(`^(/[A-Za-z0-9._-]+)+$`)

// DeployKey creates an ed25519 key pair at path (0600) unless it exists, and
// returns the public key line to add to the repo as a read-only deploy key.
func DeployKey(ctx context.Context, path, comment string) (string, error) {
	if !safePathRE.MatchString(path) {
		return "", fmt.Errorf("deploy key path %q: not a plain absolute path", path)
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", err
		}
		if _, err := host.Run(ctx, cmdTimeout, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", comment, "-f", path); err != nil {
			return "", err
		}
	}
	pub, err := os.ReadFile(path + ".pub")
	return strings.TrimSpace(string(pub)), err
}

// Fingerprint is the SHA256 fingerprint of a public key file (ssh-keygen -lf).
func Fingerprint(ctx context.Context, pubFile string) (string, error) {
	out, err := host.Run(ctx, cmdTimeout, "ssh-keygen", "-l", "-E", "sha256", "-f", pubFile)
	if err != nil {
		return "", err
	}
	if f := strings.Fields(out); len(f) > 1 {
		return f[1], nil
	}
	return out, nil
}

// MetaURL is GitHub's published metadata, which lists its SSH host keys.
const MetaURL = "https://api.github.com/meta"

// KnownHosts fetches GitHub's SSH host keys from its metadata and returns a
// known_hosts file for github.com plus the fingerprints GitHub publishes next
// to them (to show to the user, BRIEF §5).
func KnownHosts(ctx context.Context, c *http.Client, url string) (file []byte, fingerprints map[string]string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", name.Name)
	resp, err := c.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	var meta struct {
		SSHKeys         []string          `json:"ssh_keys"`
		SSHFingerprints map[string]string `json:"ssh_key_fingerprints"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&meta); err != nil {
		return nil, nil, err
	}
	var b strings.Builder
	for _, k := range meta.SSHKeys {
		f := strings.Fields(k)
		if len(f) != 2 || !strings.HasPrefix(f[0], "ssh-") && !strings.HasPrefix(f[0], "ecdsa-") {
			return nil, nil, fmt.Errorf("unexpected SSH key in GitHub metadata: %q", k)
		}
		b.WriteString("github.com " + f[0] + " " + f[1] + "\n")
	}
	if b.Len() == 0 {
		return nil, nil, errors.New("GitHub metadata lists no SSH keys")
	}
	return []byte(b.String()), meta.SSHFingerprints, nil
}

// Repo is what fetching one app's repository needs.
type Repo struct {
	URL        string // git@github.com:owner/repo.git (spec-validated)
	Mirror     string // bare mirror directory
	Key        string // private deploy key
	KnownHosts string // dermaga's own known_hosts
}

func (r Repo) env() ([]string, error) {
	for _, p := range []string{r.Key, r.KnownHosts} {
		if !safePathRE.MatchString(p) {
			return nil, fmt.Errorf("path %q: not a plain absolute path", p)
		}
	}
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -i "+r.Key+" -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile="+
			r.KnownHosts+" -o BatchMode=yes -o ConnectTimeout=20",
	), nil
}

func (r Repo) git(ctx context.Context, timeout time.Duration, log io.Writer, args ...string) (string, error) {
	env, err := r.env()
	if err != nil {
		return "", err
	}
	return host.RunEnv(ctx, timeout, env, log, append([]string{"git"}, args...)...)
}

// Fetch creates or updates the bare mirror.
func (r Repo) Fetch(ctx context.Context, log io.Writer) error {
	if _, err := os.Stat(filepath.Join(r.Mirror, "HEAD")); errors.Is(err, fs.ErrNotExist) {
		os.RemoveAll(r.Mirror) // a half-done clone
		if err := os.MkdirAll(filepath.Dir(r.Mirror), 0o700); err != nil {
			return err
		}
		_, err := r.git(ctx, fetchTimeout, log, "clone", "--mirror", "--", r.URL, r.Mirror)
		return err
	}
	_, err := r.git(ctx, fetchTimeout, log, "--git-dir", r.Mirror, "fetch", "--prune", "origin")
	return err
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// Resolve turns a branch name or a (possibly abbreviated) SHA into the full
// commit SHA and its subject line.
func (r Repo) Resolve(ctx context.Context, branch, ref string) (sha, subject string, err error) {
	rev := "refs/heads/" + branch
	if ref != "" {
		if !shaRE.MatchString(ref) {
			return "", "", fmt.Errorf("ref %q: give a commit SHA (7-40 hex characters)", ref)
		}
		rev = ref
	}
	sha, err = r.git(ctx, cmdTimeout, nil, "--git-dir", r.Mirror, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", "", fmt.Errorf("%s: not found in %s", rev, r.URL)
	}
	subject, err = r.git(ctx, cmdTimeout, nil, "--git-dir", r.Mirror, "log", "-1", "--format=%s", sha)
	return sha, subject, err
}

// Export writes the tree of sha into dir (git archive, then tar), without .git.
func (r Repo) Export(ctx context.Context, sha, dir string) error {
	tarFile := dir + ".tar"
	defer os.Remove(tarFile)
	if _, err := r.git(ctx, fetchTimeout, nil, "--git-dir", r.Mirror, "archive", "--format=tar", "-o", tarFile, sha); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_, err := host.Run(ctx, fetchTimeout, "tar", "-x", "--no-same-owner", "-f", tarFile, "-C", dir)
	return err
}
