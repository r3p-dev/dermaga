// Package config resolves dermaga's XDG paths and loads config.toml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/r3p-dev/dermaga/internal/name"
)

// Paths are the directories dermaga owns (BRIEF §3.3, ARCHITECTURE §4).
type Paths struct {
	ConfigDir  string // ~/.config/dermaga
	DataDir    string // ~/.local/share/dermaga
	RuntimeDir string // $XDG_RUNTIME_DIR/dermaga
	QuadletDir string // ~/.config/containers/systemd (generated dg-* units)
	TraefikDir string // ~/.config/traefik (traefik.yml, dynamic/)
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	run := os.Getenv("XDG_RUNTIME_DIR")
	if run == "" {
		return Paths{}, errors.New("XDG_RUNTIME_DIR is not set (log in as the user, e.g. over ssh, not via sudo -u)")
	}
	return Paths{
		ConfigDir:  filepath.Join(cfg, name.Name),
		DataDir:    filepath.Join(data, name.Name),
		RuntimeDir: filepath.Join(run, name.Name),
		QuadletDir: filepath.Join(cfg, "containers", "systemd"),
		TraefikDir: filepath.Join(cfg, "traefik"),
	}, nil
}

func (p Paths) ConfigFile() string { return filepath.Join(p.ConfigDir, "config.toml") }
func (p Paths) MasterKey() string  { return filepath.Join(p.ConfigDir, "master.key") }
func (p Paths) DB() string         { return filepath.Join(p.DataDir, name.Name+".db") }
func (p Paths) Socket() string     { return filepath.Join(p.RuntimeDir, name.Name+".sock") }
func (p Paths) DynamicDir() string { return filepath.Join(p.TraefikDir, "dynamic") }

// Config holds the non-secret settings that must be known before the database is open.
type Config struct {
	Panel struct {
		Port int `toml:"port"`
	} `toml:"panel"`
}

// Load returns the defaults overlaid with path, which may be missing.
// Unknown keys are rejected so a typo doesn't silently fall back to a default.
func Load(path string) (Config, error) {
	var c Config
	c.Panel.Port = 7070
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	d := toml.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if c.Panel.Port < 1 || c.Panel.Port > 65535 {
		return c, fmt.Errorf("%s: panel.port %d out of range", path, c.Panel.Port)
	}
	return c, nil
}
