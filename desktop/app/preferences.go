package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/a448582655/vibe-proxy/internal/desktopbridge"
)

const (
	minimumWindowWidth  = 960
	minimumWindowHeight = 640
)

// WindowPreferences holds the persisted desktop window dimensions.
type WindowPreferences struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Preferences holds the non-secret desktop host preferences.
type Preferences struct {
	CloseBehavior desktopbridge.CloseBehavior `json:"closeBehavior"`
	Window        WindowPreferences           `json:"window"`
}

// DefaultPreferences returns the safe first-run desktop preferences.
func DefaultPreferences() Preferences {
	return Preferences{
		CloseBehavior: desktopbridge.CloseAsk,
		Window: WindowPreferences{
			Width:  1280,
			Height: 800,
		},
	}
}

// LoadPreferences loads saved preferences, returning defaults for a missing or
// corrupt preferences file. Corrupt files are retained as a timestamped backup.
func LoadPreferences(path string) (Preferences, error) {
	return loadPreferences(path, time.Now)
}

func loadPreferences(path string, now func() time.Time) (Preferences, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultPreferences(), nil
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("open preferences: %w", err)
	}
	var preferences Preferences
	decoder := json.NewDecoder(file)
	decodeErr := decoder.Decode(&preferences)
	if decodeErr == nil {
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			if err == nil {
				decodeErr = errors.New("multiple JSON values")
			} else {
				decodeErr = err
			}
		}
	}
	if err := file.Close(); err != nil {
		return Preferences{}, fmt.Errorf("close preferences: %w", err)
	}
	if decodeErr != nil {
		return backupCorruptPreferences(path, now)
	}

	return normalizeLoadedPreferences(preferences), nil
}

// SavePreferences validates values and atomically replaces the preferences
// file. The parent and completed file are restricted to the owning user.
func SavePreferences(path string, preferences Preferences) error {
	return savePreferences(path, preferences, replacePreferenceFile)
}

func savePreferences(path string, preferences Preferences, replace func(string, string) error) error {
	preferences = normalizeSavedPreferences(preferences)
	contents, err := json.Marshal(preferences)
	if err != nil {
		return fmt.Errorf("marshal preferences: %w", err)
	}

	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create preferences directory: %w", err)
	}

	temporary, err := os.CreateTemp(parent, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary preferences file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("set temporary preferences permissions: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary preferences: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary preferences: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary preferences: %w", err)
	}
	if err := replace(temporaryPath, path); err != nil {
		return fmt.Errorf("replace preferences: %w", err)
	}
	return nil
}

func backupCorruptPreferences(path string, now func() time.Time) (Preferences, error) {
	backupPath := path + ".corrupt-" + now().UTC().Format("20060102T150405.000000000Z")
	if _, err := os.Lstat(backupPath); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(path, backupPath); err != nil {
			return Preferences{}, fmt.Errorf("backup corrupt preferences: %w", err)
		}
	} else if err != nil {
		return Preferences{}, fmt.Errorf("inspect corrupt preferences backup: %w", err)
	}
	return DefaultPreferences(), nil
}

func normalizeLoadedPreferences(preferences Preferences) Preferences {
	defaults := DefaultPreferences()
	if !validCloseBehavior(preferences.CloseBehavior) {
		preferences.CloseBehavior = defaults.CloseBehavior
	}
	if preferences.Window.Width < minimumWindowWidth || preferences.Window.Height < minimumWindowHeight {
		preferences.Window = defaults.Window
	}
	return preferences
}

func normalizeSavedPreferences(preferences Preferences) Preferences {
	if !validCloseBehavior(preferences.CloseBehavior) {
		preferences.CloseBehavior = desktopbridge.CloseAsk
	}
	if preferences.Window.Width < minimumWindowWidth {
		preferences.Window.Width = minimumWindowWidth
	}
	if preferences.Window.Height < minimumWindowHeight {
		preferences.Window.Height = minimumWindowHeight
	}
	return preferences
}

func validCloseBehavior(behavior desktopbridge.CloseBehavior) bool {
	switch behavior {
	case desktopbridge.CloseAsk, desktopbridge.CloseTray, desktopbridge.CloseQuit:
		return true
	default:
		return false
	}
}
