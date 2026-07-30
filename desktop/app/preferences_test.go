package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/desktopbridge"
)

func TestPreferencesDefaults(t *testing.T) {
	preferences := DefaultPreferences()
	if preferences.CloseBehavior != desktopbridge.CloseAsk {
		t.Fatalf("CloseBehavior = %q, want %q", preferences.CloseBehavior, desktopbridge.CloseAsk)
	}
	if preferences.Window != (WindowPreferences{Width: 1280, Height: 800}) {
		t.Fatalf("Window = %+v, want 1280x800", preferences.Window)
	}
}

func TestSavePreferencesCreatesPrivateAtomicFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "desktop.json")
	preferences := Preferences{CloseBehavior: desktopbridge.CloseTray, Window: WindowPreferences{Width: 1000, Height: 700}}
	if err := SavePreferences(path, preferences); err != nil {
		t.Fatalf("SavePreferences() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(saved preferences) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("saved file permissions = %#o, want 0600", got)
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat(parent) error = %v", err)
	}
	if got := parent.Mode().Perm(); got != 0o700 {
		t.Fatalf("parent permissions = %#o, want 0700", got)
	}
	if leftovers, err := filepath.Glob(path + ".tmp-*"); err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary files = %v, glob error = %v", leftovers, err)
	}
}

func TestLoadPreferencesIgnoresUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.json")
	if err := os.WriteFile(path, []byte(`{"closeBehavior":"tray","window":{"width":1100,"height":700},"future":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	preferences, err := LoadPreferences(path)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	if preferences != (Preferences{CloseBehavior: desktopbridge.CloseTray, Window: WindowPreferences{Width: 1100, Height: 700}}) {
		t.Fatalf("preferences = %+v", preferences)
	}
}

func TestLoadPreferencesReplacesInvalidValuesWithDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.json")
	if err := os.WriteFile(path, []byte(`{"closeBehavior":"invalid","window":{"width":1,"height":2}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	preferences, err := LoadPreferences(path)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	if preferences != DefaultPreferences() {
		t.Fatalf("preferences = %+v, want defaults %+v", preferences, DefaultPreferences())
	}
}

func TestSavePreferencesValidatesAndClampsValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.json")
	if err := SavePreferences(path, Preferences{CloseBehavior: "invalid", Window: WindowPreferences{Width: 1, Height: 2}}); err != nil {
		t.Fatalf("SavePreferences() error = %v", err)
	}
	preferences, err := LoadPreferences(path)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	if preferences != (Preferences{CloseBehavior: desktopbridge.CloseAsk, Window: WindowPreferences{Width: 960, Height: 640}}) {
		t.Fatalf("preferences = %+v, want validated defaults", preferences)
	}
}

func TestLoadPreferencesBacksUpCorruptJSONWithInjectedClock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.json")
	contents := []byte(`{"closeBehavior":`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 30, 5, 4, 3, 123456789, time.FixedZone("UTC+8", 8*60*60))
	preferences, err := loadPreferences(path, func() time.Time { return now })
	if err != nil {
		t.Fatalf("loadPreferences() error = %v", err)
	}
	if preferences != DefaultPreferences() {
		t.Fatalf("preferences = %+v, want defaults", preferences)
	}
	backup := path + ".corrupt-20260729T210403.123456789Z"
	got, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("ReadFile(backup) error = %v", err)
	}
	if string(got) != string(contents) {
		t.Fatalf("backup contents = %q, want %q", got, contents)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt source stat error = %v, want not exist", err)
	}
}

func TestLoadPreferencesMissingFileReturnsDefaults(t *testing.T) {
	preferences, err := LoadPreferences(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	if preferences != DefaultPreferences() {
		t.Fatalf("preferences = %+v, want defaults", preferences)
	}
}
