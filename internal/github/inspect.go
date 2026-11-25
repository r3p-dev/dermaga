package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Branch represents a Git branch returned by the GitHub REST API (WIZ-2).
type Branch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
}

// CommitInfo represents the resolved HEAD commit for a branch or ref.
type CommitInfo struct {
	SHA     string
	Message string
}

// ContainerfileCandidate describes a detected Containerfile or Dockerfile in a repository (WIZ-3).
type ContainerfileCandidate struct {
	Path         string `json:"path"`
	BuildContext string `json:"build_context"`
	ExposedPort  int    `json:"exposed_port"`
}

// RepoInspection summarizes Containerfile detection and unsupported Git feature checks (WIZ-3).
type RepoInspection struct {
	Branch           string                   `json:"branch"`
	HasContainerfile bool                     `json:"has_containerfile"`
	Containerfile    string                   `json:"containerfile"`
	BuildContext     string                   `json:"build_context"`
	SuggestedPort    int                      `json:"suggested_port"`
	Candidates       []ContainerfileCandidate `json:"candidates"`
	HasSubmodules    bool                     `json:"has_submodules"`
	HasLFS           bool                     `json:"has_lfs"`
	TreeTruncated    bool                     `json:"tree_truncated"`
}

// ListBranches lists branches of owner/repo using a repo-scoped installation token (WIZ-2, GHA-5).
func (c *Client) ListBranches(ctx context.Context, appID int64, pemBytes []byte, installationID int64, owner, repo string) ([]Branch, error) {
	token, _, err := c.CreateInstallationToken(ctx, appID, pemBytes, installationID, []string{repo})
	if err != nil {
		return nil, err
	}
	var all []Branch
	for page := 1; page <= 10; page++ {
		endpoint := fmt.Sprintf("%s/repos/%s/%s/branches?per_page=100&page=%d",
			c.apiBase(), url.PathEscape(owner), url.PathEscape(repo), page)
		var batch []Branch
		if err := c.doJSON(ctx, http.MethodGet, endpoint, "token "+token, nil, &batch); err != nil {
			return nil, fmt.Errorf("listing branches for %s/%s: %w", owner, repo, err)
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

// GetCommit resolves a branch or ref to its commit SHA and subject line (GHA-5).
func (c *Client) GetCommit(ctx context.Context, appID int64, pemBytes []byte, installationID int64, owner, repo, ref string) (*CommitInfo, error) {
	token, _, err := c.CreateInstallationToken(ctx, appID, pemBytes, installationID, []string{repo})
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/commits/%s",
		c.apiBase(), url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(ref))
	var resp struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	if err := c.doJSON(ctx, http.MethodGet, endpoint, "token "+token, nil, &resp); err != nil {
		return nil, fmt.Errorf("resolving commit %q for %s/%s: %w", ref, owner, repo, err)
	}
	subject, _, _ := strings.Cut(resp.Commit.Message, "\n")
	return &CommitInfo{
		SHA:     resp.SHA,
		Message: strings.TrimSpace(subject),
	}, nil
}

// InspectRepo reads the repository tree and relevant files via the GitHub REST API (no clone, WIZ-3).
func (c *Client) InspectRepo(ctx context.Context, appID int64, pemBytes []byte, installationID int64, owner, repo, branch string) (*RepoInspection, error) {
	token, _, err := c.CreateInstallationToken(ctx, appID, pemBytes, installationID, []string{repo})
	if err != nil {
		return nil, err
	}

	treeEndpoint := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1",
		c.apiBase(), url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(branch))
	var treeResp struct {
		Truncated bool `json:"truncated"`
		Tree      []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	if err := c.doJSON(ctx, http.MethodGet, treeEndpoint, "token "+token, nil, &treeResp); err != nil {
		if strings.Contains(err.Error(), "409 Conflict") {
			return &RepoInspection{
				Branch:     branch,
				Candidates: []ContainerfileCandidate{},
			}, nil
		}
		return nil, fmt.Errorf("reading repository tree for %s/%s@%s: %w", owner, repo, branch, err)
	}

	out := &RepoInspection{
		Branch:        branch,
		Candidates:    []ContainerfileCandidate{},
		TreeTruncated: treeResp.Truncated,
	}

	var gitattributesFound bool
	var candidatePaths []string
	for _, entry := range treeResp.Tree {
		if entry.Type != "blob" {
			continue
		}
		if entry.Path == ".gitmodules" {
			out.HasSubmodules = true
			continue
		}
		if entry.Path == ".gitattributes" {
			gitattributesFound = true
			continue
		}
		if isCandidateContainerfile(entry.Path) {
			candidatePaths = append(candidatePaths, entry.Path)
		}
	}

	if gitattributesFound {
		content, err := c.getRepoFile(ctx, token, owner, repo, ".gitattributes", branch)
		if err == nil && strings.Contains(content, "filter=lfs") {
			out.HasLFS = true
		}
	}

	sort.Slice(candidatePaths, func(i, j int) bool {
		return containerfilePriority(candidatePaths[i]) < containerfilePriority(candidatePaths[j])
	})

	for _, p := range candidatePaths {
		buildCtx := path.Dir(p)
		if buildCtx == "" {
			buildCtx = "."
		}
		exposed := 0
		if len(out.Candidates) < 5 {
			if content, err := c.getRepoFile(ctx, token, owner, repo, p, branch); err == nil {
				exposed = parseExposedPort(content)
			}
		}
		out.Candidates = append(out.Candidates, ContainerfileCandidate{
			Path:         p,
			BuildContext: buildCtx,
			ExposedPort:  exposed,
		})
	}

	if len(out.Candidates) > 0 {
		out.HasContainerfile = true
		out.Containerfile = out.Candidates[0].Path
		out.BuildContext = out.Candidates[0].BuildContext
		out.SuggestedPort = out.Candidates[0].ExposedPort
	}

	return out, nil
}

func (c *Client) getRepoFile(ctx context.Context, token, owner, repo, filePath, ref string) (string, error) {
	escapedPath := escapeRepoPath(filePath)
	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
		c.apiBase(), url.PathEscape(owner), url.PathEscape(repo), escapedPath, url.QueryEscape(ref))
	var resp struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := c.doJSON(ctx, http.MethodGet, endpoint, "token "+token, nil, &resp); err != nil {
		return "", err
	}
	if resp.Encoding != "base64" {
		return resp.Content, nil
	}
	cleaned := strings.ReplaceAll(resp.Content, "\n", "")
	raw, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return "", fmt.Errorf("decoding %s: %w", filePath, err)
	}
	return string(raw), nil
}

func escapeRepoPath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func isCandidateContainerfile(p string) bool {
	base := path.Base(p)
	if base != "Containerfile" && base != "Dockerfile" {
		return false
	}
	parts := strings.Split(p, "/")
	if len(parts) > 4 {
		return false
	}
	for _, seg := range parts[:len(parts)-1] {
		switch seg {
		case ".git", "node_modules", "vendor", ".svelte-kit", "dist", "build":
			return false
		}
	}
	return true
}

func containerfilePriority(p string) string {
	depth := strings.Count(p, "/")
	base := path.Base(p)
	kindRank := "1"
	if base == "Containerfile" {
		kindRank = "0"
	}
	return fmt.Sprintf("%d:%s:%s", depth, kindRank, p)
}

// parseExposedPort extracts the first static EXPOSE port from a Containerfile/Dockerfile (WIZ-3).
// Returns 0 if no EXPOSE directive exists or if it references a variable (e.g. EXPOSE $PORT).
func parseExposedPort(content string) int {
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "EXPOSE") {
			continue
		}
		for _, tok := range fields[1:] {
			if strings.HasPrefix(tok, "#") {
				break
			}
			if strings.Contains(tok, "$") {
				return 0
			}
			portStr, _, _ := strings.Cut(tok, "/")
			port, err := strconv.Atoi(portStr)
			if err == nil && port >= 1 && port <= 65535 {
				return port
			}
		}
	}
	return 0
}
