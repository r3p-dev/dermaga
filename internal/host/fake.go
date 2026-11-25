package host

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// CommandCall records an invocation of RunCommand on FakeHost.
type CommandCall struct {
	Name string
	Args []string
	Env  []string
}

// FakeHost implements Host for unit testing.
type FakeHost struct {
	mu          sync.Mutex
	Files       map[string][]byte
	FileModes   map[string]os.FileMode
	Commands    []CommandCall
	CommandMock func(name string, args ...string) ([]byte, error)
	StreamMock  func(ctx context.Context, env []string, onLine func(string), name string, args ...string) error
}

// NewFakeHost initializes a memory-backed FakeHost.
func NewFakeHost() *FakeHost {
	return &FakeHost{
		Files:     make(map[string][]byte),
		FileModes: make(map[string]os.FileMode),
	}
}

// RunCommand records the call and executes the mock handler if configured.
func (f *FakeHost) RunCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return f.RunCommandWithEnv(ctx, nil, name, args...)
}

// RunCommandWithEnv records the call with env and executes the mock handler if configured.
func (f *FakeHost) RunCommandWithEnv(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Commands = append(f.Commands, CommandCall{Name: name, Args: args, Env: env})
	if f.CommandMock != nil {
		return f.CommandMock(name, args...)
	}
	return []byte("ok"), nil
}

// StreamCommandWithEnv records the call and streams mocked output lines.
func (f *FakeHost) StreamCommandWithEnv(ctx context.Context, env []string, onLine func(string), name string, args ...string) error {
	f.mu.Lock()
	f.Commands = append(f.Commands, CommandCall{Name: name, Args: args, Env: env})
	streamFn := f.StreamMock
	cmdFn := f.CommandMock
	f.mu.Unlock()

	if streamFn != nil {
		return streamFn(ctx, env, onLine, name, args...)
	}
	if cmdFn != nil {
		out, err := cmdFn(name, args...)
		if onLine != nil && len(out) > 0 {
			for _, line := range splitLines(string(out)) {
				onLine(line)
			}
		}
		return err
	}
	if onLine != nil {
		onLine("ok")
	}
	return nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// WriteFileAtomic stores file content in memory.
func (f *FakeHost) WriteFileAtomic(targetPath string, data []byte, perm os.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Files[targetPath] = append([]byte(nil), data...)
	f.FileModes[targetPath] = perm
	return nil
}

// ReadFile returns stored file content from memory.
func (f *FakeHost) ReadFile(path string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.Files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

// Stat returns mock file info.
func (f *FakeHost) Stat(path string) (os.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.Files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	perm := f.FileModes[path]
	return &fakeFileInfo{name: path, size: int64(len(data)), mode: perm}, nil
}

// Remove deletes a file from memory.
func (f *FakeHost) Remove(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.Files[path]; !ok {
		return os.ErrNotExist
	}
	delete(f.Files, path)
	delete(f.FileModes, path)
	return nil
}

// RemoveAll deletes path and any files under it from memory.
func (f *FakeHost) RemoveAll(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	clean := filepath.Clean(path)
	prefix := clean + string(filepath.Separator)
	for k := range f.Files {
		if k == clean || len(k) > len(prefix) && k[:len(prefix)] == prefix {
			delete(f.Files, k)
			delete(f.FileModes, k)
		}
	}
	return nil
}

// MkdirAll simulates creating directories in memory.
func (f *FakeHost) MkdirAll(path string, perm os.FileMode) error {
	return nil
}

// ReadDir returns all directory entries in memory under the given path.
func (f *FakeHost) ReadDir(path string) ([]os.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	cleanDir := filepath.Clean(path)
	seen := make(map[string]bool)
	var entries []os.DirEntry

	for p, data := range f.Files {
		dir := filepath.Clean(filepath.Dir(p))
		if dir == cleanDir {
			base := filepath.Base(p)
			if !seen[base] {
				seen[base] = true
				mode := f.FileModes[p]
				entries = append(entries, &fakeDirEntry{
					name: base,
					info: &fakeFileInfo{name: base, size: int64(len(data)), mode: mode},
				})
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	return entries, nil
}

type fakeDirEntry struct {
	name string
	info os.FileInfo
}

func (e *fakeDirEntry) Name() string               { return e.name }
func (e *fakeDirEntry) IsDir() bool                { return false }
func (e *fakeDirEntry) Type() os.FileMode          { return e.info.Mode().Type() }
func (e *fakeDirEntry) Info() (os.FileInfo, error) { return e.info, nil }

type fakeFileInfo struct {
	name string
	size int64
	mode os.FileMode
}

func (fi *fakeFileInfo) Name() string       { return fi.name }
func (fi *fakeFileInfo) Size() int64        { return fi.size }
func (fi *fakeFileInfo) Mode() os.FileMode  { return fi.mode }
func (fi *fakeFileInfo) ModTime() time.Time { return time.Now() }
func (fi *fakeFileInfo) IsDir() bool        { return false }
func (fi *fakeFileInfo) Sys() any           { return nil }
