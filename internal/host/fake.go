package host

import (
	"context"
	"os"
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

// MkdirAll simulates creating directories in memory.
func (f *FakeHost) MkdirAll(path string, perm os.FileMode) error {
	return nil
}

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
