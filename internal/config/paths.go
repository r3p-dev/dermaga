package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultPaths returns standard XDG paths for dermaga files.
type Paths struct {
	DBPath     string
	KeyPath    string
	SocketDir  string
	DaemonSock string
	HookSock   string
}

// ResolvePaths resolves all standard directories according to XDG specifications.
func ResolvePaths() Paths {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/home/server"
	}

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}

	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}

	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
		if _, err := os.Stat(runtimeDir); err != nil {
			runtimeDir = filepath.Join(home, ".local", "run")
		}
	}

	socketDir := filepath.Join(runtimeDir, "dermaga")

	return Paths{
		DBPath:     filepath.Join(dataHome, "dermaga", "dermaga.db"),
		KeyPath:    filepath.Join(configHome, "dermaga", "master.key"),
		SocketDir:  socketDir,
		DaemonSock: filepath.Join(socketDir, "daemon.sock"),
		HookSock:   filepath.Join(socketDir, "hooks.sock"),
	}
}
