// Package host abstracts interactions with the host operating system, including
// command execution without shell interpolation and atomic file operations.
package host

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Host abstracts host-level interactions for dependency injection and testability.
type Host interface {
	RunCommand(ctx context.Context, name string, args ...string) ([]byte, error)
	RunCommandWithEnv(ctx context.Context, env []string, name string, args ...string) ([]byte, error)
	WriteFileAtomic(path string, data []byte, perm os.FileMode) error
	ReadFile(path string) ([]byte, error)
	Stat(path string) (os.FileInfo, error)
	Remove(path string) error
	MkdirAll(path string, perm os.FileMode) error
	ReadDir(path string) ([]os.DirEntry, error)
}

// RealHost implements Host using real OS system calls.
type RealHost struct{}

// NewRealHost returns a Host implementation backed by the actual host OS.
func NewRealHost() *RealHost {
	return &RealHost{}
}

// RunCommand executes a command using an argument slice without shell interpolation (SEC-16).
func (h *RealHost) RunCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return h.RunCommandWithEnv(ctx, nil, name, args...)
}

// RunCommandWithEnv executes a command with specified environment variables without shell interpolation (SEC-16).
func (h *RealHost) RunCommandWithEnv(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("command %s %v failed: %w: %s", name, args, err, string(out))
	}
	return out, nil
}

// WriteFileAtomic writes data to a temporary file in the same directory, fsyncs it,
// renames it to the target path, and fsyncs the parent directory.
func (h *RealHost) WriteFileAtomic(targetPath string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating parent directory %q: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, ".dermaga-atomic-*")
	if err != nil {
		return fmt.Errorf("creating temp file in %q: %w", dir, err)
	}
	tmpPath := tmpFile.Name()

	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := tmpFile.Chmod(perm); err != nil {
		return fmt.Errorf("setting permissions on %q: %w", tmpPath, err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("writing data to %q: %w", tmpPath, err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("fsyncing file %q: %w", tmpPath, err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temp file %q: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("renaming %q to %q: %w", tmpPath, targetPath, err)
	}

	// Fsync parent directory
	dirFile, err := os.Open(dir)
	if err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

	return nil
}

// ReadFile reads the named file and returns the contents.
func (h *RealHost) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// Stat returns a FileInfo describing the named file.
func (h *RealHost) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// Remove removes the named file or empty directory.
func (h *RealHost) Remove(path string) error {
	return os.Remove(path)
}

// MkdirAll creates a directory named path, along with any necessary parents.
func (h *RealHost) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// ReadDir reads the named directory, returning all its directory entries sorted by filename.
func (h *RealHost) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}
