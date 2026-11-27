package deploy

import (
	"fmt"
	"path"
	"strings"
)

// Watch-path limits (APP-8): enough for a monorepo, small enough to read at a glance.
const (
	maxWatchPathLines = 50
	maxWatchPathLen   = 256
)

// watchPatterns splits the stored watch paths into trimmed, non-empty lines.
func watchPatterns(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// NormalizeWatchPaths validates watch paths (APP-8): one path.Match glob per line, relative to the
// repository root, a trailing "/" meaning the whole folder. It returns them one per line, trimmed.
func NormalizeWatchPaths(s string) (string, error) {
	lines := watchPatterns(s)
	if len(lines) > maxWatchPathLines {
		return "", fmt.Errorf("at most %d watch paths are allowed", maxWatchPathLines)
	}
	for _, p := range lines {
		switch {
		case len(p) > maxWatchPathLen:
			return "", fmt.Errorf("watch path %q is longer than %d characters", p[:32]+"…", maxWatchPathLen)
		case strings.HasPrefix(p, "/"):
			return "", fmt.Errorf("watch path %q must be relative to the repository root (no leading /)", p)
		case p == ".." || strings.HasPrefix(p, "../") || strings.Contains(p, "/../") || strings.HasSuffix(p, "/.."):
			return "", fmt.Errorf("watch path %q must not contain \"..\" (a parent folder)", p)
		case strings.Contains(p, "//"):
			return "", fmt.Errorf("watch path %q contains an empty folder name (//)", p)
		}
		// path.Match reports a malformed pattern only when it reaches the bad part, so test a
		// full match attempt against the pattern's own text too.
		if _, err := path.Match(strings.TrimSuffix(p, "/"), ""); err != nil {
			return "", fmt.Errorf("watch path %q is not a valid pattern", p)
		}
		if _, err := path.Match(strings.TrimSuffix(p, "/"), strings.TrimSuffix(p, "/")); err != nil {
			return "", fmt.Errorf("watch path %q is not a valid pattern", p)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// watchPathsMatch reports whether any changed file matches the watch paths (APP-8). Empty watch
// paths match every push. "web/" matches everything under web/; "web/*" only web's direct files.
func watchPathsMatch(watch string, files []string) bool {
	patterns := watchPatterns(watch)
	if len(patterns) == 0 {
		return true
	}
	for _, f := range files {
		for _, p := range patterns {
			if dir, ok := strings.CutSuffix(p, "/"); ok {
				// The folder part may itself be a glob ("services/*/"): match it against every
				// leading folder of the file.
				parts := strings.Split(f, "/")
				for i := 1; i < len(parts); i++ {
					if m, _ := path.Match(dir, strings.Join(parts[:i], "/")); m {
						return true
					}
				}
				continue
			}
			if m, _ := path.Match(p, f); m {
				return true
			}
		}
	}
	return false
}
