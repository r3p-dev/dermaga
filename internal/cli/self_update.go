package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/r3p-dev/dermaga/internal/name"
)

const defaultRepo = "r3p-dev/dermaga"

// SelfUpdateOptions configures the self-update execution.
type SelfUpdateOptions struct {
	Check       bool
	Version     string
	Force       bool
	Repo        string
	BaseURL     string
	BinaryPath  string
	SkipRestart bool
	Stdout      io.Writer
	Stderr      io.Writer
	HTTPClient  *http.Client
}

// cmdSelfUpdate is the CLI entry point for 'dermaga self-update'.
func cmdSelfUpdate(args []string) error {
	fs := flag.NewFlagSet("self-update", flag.ContinueOnError)
	check := fs.Bool("check", false, "check for updates without applying")
	targetVersion := fs.String("version", "", "update to a specific version or tag (e.g. v0.8.0)")
	force := fs.Bool("force", false, "force update even if already on the target version")
	repo := fs.String("repo", "", "override GitHub repository (owner/repo)")
	baseURL := fs.String("url", "", "custom URL prefix for release downloads (testing/internal)")

	if err := parse(fs, args); err != nil {
		return err
	}

	opts := SelfUpdateOptions{
		Check:   *check,
		Version: *targetVersion,
		Force:   *force,
		Repo:    *repo,
		BaseURL: *baseURL,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return RunSelfUpdate(ctx, opts)
}

// RunSelfUpdate executes the update workflow with the provided options.
func RunSelfUpdate(ctx context.Context, opts SelfUpdateOptions) error {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	repo := opts.Repo
	if repo == "" {
		repo = os.Getenv("DERMAGA_REPO")
	}
	if repo == "" {
		repo = defaultRepo
	}

	if runtime.GOOS != "linux" {
		return fmt.Errorf("self-update is only supported on linux, not %s", runtime.GOOS)
	}
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("self-update is only supported on amd64 and arm64, not %s", arch)
	}
	binaryName := fmt.Sprintf("%s-linux-%s", name.Name, arch)

	currentVer := name.Version()

	targetVer := opts.Version
	if targetVer != "" {
		if !strings.HasPrefix(targetVer, "v") && !strings.Contains(targetVer, ".") {
			targetVer = "v" + targetVer
		}
	} else if opts.BaseURL == "" {
		tag, err := resolveLatestReleaseTag(ctx, opts.HTTPClient, repo)
		if err != nil {
			return fmt.Errorf("resolving latest release: %w", err)
		}
		targetVer = tag
	} else {
		targetVer = "custom"
	}

	if opts.Check {
		fmt.Fprintf(opts.Stdout, "Current version: %s\nLatest version:  %s\n", currentVer, targetVer)
		if currentVer != "dev" && currentVer != "unknown" && currentVer == targetVer {
			fmt.Fprintln(opts.Stdout, "dermaga is up to date.")
		} else {
			fmt.Fprintf(opts.Stdout, "Update available: %s -> %s\nRun 'dermaga self-update' to install.\n", currentVer, targetVer)
		}
		return nil
	}

	if !opts.Force && currentVer != "dev" && currentVer != "unknown" && currentVer == targetVer {
		fmt.Fprintf(opts.Stdout, "dermaga is already up to date (%s). Use --force to reinstall.\n", currentVer)
		return nil
	}

	fmt.Fprintf(opts.Stdout, "Updating %s (%s -> %s [%s])...\n", name.Name, currentVer, targetVer, arch)

	var downloadURL, checksumURL string
	if opts.BaseURL != "" {
		downloadURL = strings.TrimSuffix(opts.BaseURL, "/") + "/" + binaryName
		checksumURL = strings.TrimSuffix(opts.BaseURL, "/") + "/checksums.txt"
	} else {
		downloadURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, targetVer, binaryName)
		checksumURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/checksums.txt", repo, targetVer)
	}

	destPath := opts.BinaryPath
	if destPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolving executable path: %w", err)
		}
		destPath, err = filepath.EvalSymlinks(exe)
		if err != nil {
			destPath = exe
		}
	}

	destDir := filepath.Dir(destPath)
	tempFile, err := os.CreateTemp(destDir, "dermaga-update-*")
	if err != nil {
		// If destDir is not writable, fail with clear permission instructions.
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("permission denied writing to %s: re-run with sudo (e.g. sudo dermaga self-update)", destDir)
		}
		return fmt.Errorf("creating temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		tempFile.Close()
		_ = os.Remove(tempPath)
	}()

	fmt.Fprintf(opts.Stdout, "Fetching checksums from %s...\n", checksumURL)
	expectedHash, err := fetchExpectedChecksum(ctx, opts.HTTPClient, checksumURL, binaryName)
	if err != nil {
		return fmt.Errorf("verifying release checksums: %w", err)
	}

	fmt.Fprintf(opts.Stdout, "Downloading %s from %s...\n", binaryName, downloadURL)
	hasher := sha256.New()
	mw := io.MultiWriter(tempFile, hasher)

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("dermaga-self-update/%s", currentVer))

	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", binaryName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s returned HTTP %d", binaryName, resp.StatusCode)
	}

	if _, err := io.Copy(mw, resp.Body); err != nil {
		return fmt.Errorf("saving binary: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("syncing binary: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("closing temp binary: %w", err)
	}

	computedHash := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(computedHash, expectedHash) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s (tampered or corrupt download)", expectedHash, computedHash)
	}
	fmt.Fprintf(opts.Stdout, "SHA-256 verified (%s).\n", computedHash)

	if err := os.Chmod(tempPath, 0o755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// Validate downloaded binary can execute
	validateCmd := exec.CommandContext(ctx, tempPath, "version")
	if out, err := validateCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("validating downloaded binary failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}

	// Atomic replacement with rollback
	backupPath := destPath + ".old"
	_ = os.Remove(backupPath)

	if err := os.Rename(destPath, backupPath); err != nil {
		return fmt.Errorf("backing up current binary: %w", err)
	}

	if err := os.Rename(tempPath, destPath); err != nil {
		// Rollback immediately
		_ = os.Rename(backupPath, destPath)
		return fmt.Errorf("replacing binary: %w (rolled back)", err)
	}
	_ = os.Remove(backupPath)

	fmt.Fprintf(opts.Stdout, "Successfully installed %s %s to %s.\n", name.Name, targetVer, destPath)

	// Post-update: sync relay binary if ~/.local/share/dermaga/relay/dermaga exists
	syncRelayBinary(destPath)

	// Restart systemd user unit if requested
	if !opts.SkipRestart {
		restartService(ctx, opts.Stdout)
	}

	return nil
}

func resolveLatestReleaseTag(ctx context.Context, client *http.Client, repo string) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "dermaga-self-update")

	resp, err := client.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var rel struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&rel); err == nil && rel.TagName != "" {
			return rel.TagName, nil
		}
	}
	if resp != nil {
		_ = resp.Body.Close()
	}

	// Fallback: check redirect location from https://github.com/<repo>/releases/latest
	htmlURL := fmt.Sprintf("https://github.com/%s/releases/latest", repo)
	noRedirectClient := &http.Client{
		Timeout:   10 * time.Second,
		Transport: client.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err = http.NewRequestWithContext(ctx, "GET", htmlURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "dermaga-self-update")
	resp, err = noRedirectClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", htmlURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		loc := resp.Header.Get("Location")
		if loc != "" {
			u, err := url.Parse(loc)
			if err == nil && strings.Contains(u.Path, "/releases/tag/") {
				parts := strings.Split(strings.Trim(u.Path, "/"), "/")
				if len(parts) >= 1 {
					tag := parts[len(parts)-1]
					if tag != "" && tag != "latest" && tag != "releases" {
						return tag, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("no releases found for repository %s (or repository is private)", repo)
}

func fetchExpectedChecksum(ctx context.Context, client *http.Client, checksumURL, filename string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", checksumURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "dermaga-self-update")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d fetching checksums", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return ParseChecksum(string(b), filename)
}

// ParseChecksum parses a sha256sum manifest and extracts the hash for filename.
func ParseChecksum(manifest, targetFilename string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(manifest))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := fields[0]
		fn := strings.TrimPrefix(fields[1], "*")
		fn = filepath.Base(fn)

		if fn == targetFilename {
			if len(hash) != 64 {
				return "", fmt.Errorf("invalid sha256 length %d for %s", len(hash), fn)
			}
			return strings.ToLower(hash), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("filename %q not found in checksum manifest", targetFilename)
}

func syncRelayBinary(newBinaryPath string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	relayPath := filepath.Join(home, ".local", "share", name.Name, "relay", name.Name)
	if _, err := os.Stat(filepath.Dir(relayPath)); err == nil {
		// Copy file atomically
		tmp := relayPath + ".tmp"
		src, err := os.Open(newBinaryPath)
		if err != nil {
			return
		}
		defer src.Close()

		dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return
		}
		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			_ = os.Remove(tmp)
			return
		}
		_ = dst.Sync()
		dst.Close()
		_ = os.Rename(tmp, relayPath)
	}
}

func restartService(ctx context.Context, out io.Writer) {
	var cmd *exec.Cmd
	if os.Geteuid() == 0 && os.Getenv("SUDO_USER") != "" {
		cmd = exec.CommandContext(ctx, "systemctl", "--user", "-M", os.Getenv("SUDO_USER")+"@", "restart", "dermaga")
	} else {
		cmd = exec.CommandContext(ctx, "systemctl", "--user", "restart", "dermaga")
	}

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(out, "Note: systemd user service restart returned: %v (run 'systemctl --user restart dermaga' if running)\n", err)
	} else {
		fmt.Fprintln(out, "Restarted dermaga.service user unit.")
	}
}
