package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Paths identifies desktop-owned data files. Resolving paths does not create
// any directories.
type Paths struct {
	DataDir         string
	ConfigPath      string
	DatabasePath    string
	AuthPath        string
	PreferencesPath string
	LogDir          string
	LogPath         string
}

// ResolvePaths returns the application-data layout for a supported desktop
// platform. Its dependencies are injected to keep the resolution deterministic.
func ResolvePaths(goos string, getenv func(string) string, userHome func() (string, error)) (Paths, error) {
	var dataDir string
	switch goos {
	case "windows":
		localAppData := strings.TrimRight(getenv("LOCALAPPDATA"), `\\/`)
		if localAppData == "" {
			return Paths{}, fmt.Errorf("LOCALAPPDATA is required for Windows desktop paths")
		}
		dataDir = localAppData + `\vibe-proxy`
	case "darwin":
		home, err := userHome()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve macOS home directory: %w", err)
		}
		if home == "" {
			return Paths{}, fmt.Errorf("macOS home directory is empty")
		}
		dataDir = filepath.Join(home, "Library", "Application Support", "vibe-proxy")
	default:
		return Paths{}, fmt.Errorf("unsupported desktop platform: %s", goos)
	}

	return pathsForDataDir(dataDir, goos == "windows"), nil
}

// PlatformPaths resolves paths for the current process platform.
func PlatformPaths() (Paths, error) {
	return platformPaths(runtime.GOOS, os.Getenv, os.UserHomeDir)
}

func pathsForDataDir(dataDir string, windows bool) Paths {
	if windows {
		logDir := dataDir + `\logs`
		return Paths{
			DataDir:         dataDir,
			ConfigPath:      dataDir + `\config.yaml`,
			DatabasePath:    dataDir + `\vibe-proxy.db`,
			AuthPath:        dataDir + `\auth.json`,
			PreferencesPath: dataDir + `\desktop.json`,
			LogDir:          logDir,
			LogPath:         logDir + `\vibe-proxy.log`,
		}
	}

	logDir := filepath.Join(dataDir, "logs")
	return Paths{
		DataDir:         dataDir,
		ConfigPath:      filepath.Join(dataDir, "config.yaml"),
		DatabasePath:    filepath.Join(dataDir, "vibe-proxy.db"),
		AuthPath:        filepath.Join(dataDir, "auth.json"),
		PreferencesPath: filepath.Join(dataDir, "desktop.json"),
		LogDir:          logDir,
		LogPath:         filepath.Join(logDir, "vibe-proxy.log"),
	}
}
