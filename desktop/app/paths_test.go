package app

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPathsWindowsUsesLocalAppData(t *testing.T) {
	paths, err := ResolvePaths("windows", func(key string) string {
		if key == "LOCALAPPDATA" {
			return `C:\Users\A\AppData\Local`
		}
		return ""
	}, func() (string, error) {
		return "", nil
	})
	if err != nil {
		t.Fatalf("ResolvePaths() error = %v", err)
	}
	if want := `C:\Users\A\AppData\Local\vibe-proxy`; paths.DataDir != want {
		t.Fatalf("DataDir = %q, want %q", paths.DataDir, want)
	}
	if paths.ConfigPath != `C:\Users\A\AppData\Local\vibe-proxy\config.yaml` {
		t.Fatalf("ConfigPath = %q", paths.ConfigPath)
	}
	if paths.DatabasePath != `C:\Users\A\AppData\Local\vibe-proxy\vibe-proxy.db` {
		t.Fatalf("DatabasePath = %q", paths.DatabasePath)
	}
	if paths.AuthPath != `C:\Users\A\AppData\Local\vibe-proxy\auth.json` {
		t.Fatalf("AuthPath = %q", paths.AuthPath)
	}
	if paths.PreferencesPath != `C:\Users\A\AppData\Local\vibe-proxy\desktop.json` {
		t.Fatalf("PreferencesPath = %q", paths.PreferencesPath)
	}
	if paths.LogDir != `C:\Users\A\AppData\Local\vibe-proxy\logs` || paths.LogPath != `C:\Users\A\AppData\Local\vibe-proxy\logs\vibe-proxy.log` {
		t.Fatalf("unexpected log paths: %+v", paths)
	}
}

func TestPathsWindowsTrimsTrailingSeparators(t *testing.T) {
	paths, err := ResolvePaths("windows", func(string) string { return `C:\Local////` }, func() (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("ResolvePaths() error = %v", err)
	}
	if want := `C:\Local\vibe-proxy`; paths.DataDir != want {
		t.Fatalf("DataDir = %q, want %q", paths.DataDir, want)
	}
}

func TestPathsDarwinUsesApplicationSupport(t *testing.T) {
	paths, err := ResolvePaths("darwin", func(string) string { return "" }, func() (string, error) { return "/Users/a", nil })
	if err != nil {
		t.Fatalf("ResolvePaths() error = %v", err)
	}
	if want := filepath.Join("/Users/a", "Library", "Application Support", "vibe-proxy"); paths.DataDir != want {
		t.Fatalf("DataDir = %q, want %q", paths.DataDir, want)
	}
	if paths.ConfigPath != filepath.Join(paths.DataDir, "config.yaml") || paths.DatabasePath != filepath.Join(paths.DataDir, "vibe-proxy.db") || paths.AuthPath != filepath.Join(paths.DataDir, "auth.json") || paths.PreferencesPath != filepath.Join(paths.DataDir, "desktop.json") || paths.LogDir != filepath.Join(paths.DataDir, "logs") || paths.LogPath != filepath.Join(paths.DataDir, "logs", "vibe-proxy.log") {
		t.Fatalf("unexpected darwin paths: %+v", paths)
	}
}

func TestPathsWindowsRequiresLocalAppData(t *testing.T) {
	_, err := ResolvePaths("windows", func(string) string { return "" }, func() (string, error) { return "", nil })
	if err == nil || !strings.Contains(err.Error(), "LOCALAPPDATA") {
		t.Fatalf("ResolvePaths() error = %v, want LOCALAPPDATA error", err)
	}
}

func TestPathsRejectsUnsupportedDesktopPlatform(t *testing.T) {
	_, err := ResolvePaths("linux", func(string) string { return "" }, func() (string, error) { return "", nil })
	if err == nil || !strings.Contains(err.Error(), "unsupported desktop platform") {
		t.Fatalf("ResolvePaths() error = %v, want unsupported platform error", err)
	}
}
