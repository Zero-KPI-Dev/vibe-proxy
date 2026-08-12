package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/desktopbridge"
)

func TestControllerImportValidConfigAtomicallyReplacesTarget(t *testing.T) {
	host, fakes := newCloseTestHost(t, desktopbridge.CloseAsk)
	host.paths.ConfigPath = filepath.Join(host.paths.DataDir, "config.yaml")
	original := []byte("version: vibeproxy.io/v1alpha1\nserver:\n  listen: 127.0.0.1:8080\nstorage:\n  sqlite_path: original.db\nproviders: {}\nmodels:\n  allow_raw: true\n")
	if err := os.WriteFile(host.paths.ConfigPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "selected-config.yaml")
	selected := []byte("version: vibeproxy.io/v1alpha1\nserver:\n  listen: 127.0.0.1:9090\nstorage:\n  sqlite_path: imported.db\nproviders: {}\nmodels:\n  allow_raw: true\n")
	if err := os.WriteFile(source, selected, 0o644); err != nil {
		t.Fatal(err)
	}
	fakes.dialogs.selectedPath = source
	fakes.dialogs.selected = true

	result, err := host.controller.ImportConfig(context.Background())
	if err != nil {
		t.Fatalf("ImportConfig() error = %v", err)
	}
	if result != (desktopbridge.ImportResult{Imported: true, Path: source}) {
		t.Fatalf("ImportConfig() = %+v", result)
	}
	got, err := os.ReadFile(host.paths.ConfigPath)
	if err != nil {
		t.Fatalf("ReadFile(imported config) error = %v", err)
	}
	if string(got) != string(selected) {
		t.Fatalf("imported bytes = %q, want %q", got, selected)
	}
	info, err := os.Stat(host.paths.ConfigPath)
	if err != nil {
		t.Fatalf("Stat(imported config) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("imported config permissions = %#o, want 0600", got)
	}
	if leftovers, err := filepath.Glob(host.paths.ConfigPath + ".tmp-*"); err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary config files = %v, glob error = %v", leftovers, err)
	}
}

func TestControllerImportInvalidYAMLPreservesOriginalWithoutLeakingSource(t *testing.T) {
	host, fakes := newCloseTestHost(t, desktopbridge.CloseAsk)
	host.paths.ConfigPath = filepath.Join(host.paths.DataDir, "config.yaml")
	original := []byte("original config bytes")
	if err := os.WriteFile(host.paths.ConfigPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	const sourceSecret = "TOP-SECRET-SOURCE-CONTENTS"
	source := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(source, []byte("providers: [\n"+sourceSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	fakes.dialogs.selectedPath = source
	fakes.dialogs.selected = true

	result, err := host.controller.ImportConfig(context.Background())
	if err == nil {
		t.Fatal("ImportConfig() error = nil, want invalid YAML error")
	}
	if result.Imported {
		t.Fatalf("ImportConfig() = %+v, want not imported", result)
	}
	if strings.Contains(err.Error(), sourceSecret) {
		t.Fatalf("ImportConfig() error leaked source contents: %v", err)
	}
	got, readErr := os.ReadFile(host.paths.ConfigPath)
	if readErr != nil {
		t.Fatalf("ReadFile(original config) error = %v", readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("original bytes = %q, want %q", got, original)
	}
}

func TestControllerImportPickerCancelReturnsNotImported(t *testing.T) {
	host, _ := newCloseTestHost(t, desktopbridge.CloseAsk)

	result, err := host.controller.ImportConfig(context.Background())
	if err != nil {
		t.Fatalf("ImportConfig() error = %v", err)
	}
	if result != (desktopbridge.ImportResult{Imported: false}) {
		t.Fatalf("ImportConfig() = %+v, want not imported", result)
	}
}

func TestControllerSetCloseBehaviorRejectsUnrecognizedValue(t *testing.T) {
	host, _ := newCloseTestHost(t, desktopbridge.CloseAsk)

	if err := host.controller.SetCloseBehavior("execute-arbitrary-command"); err == nil {
		t.Fatal("SetCloseBehavior() error = nil, want allow-list rejection")
	}
	if got := host.controller.Snapshot().CloseBehavior; got != desktopbridge.CloseAsk {
		t.Fatalf("CloseBehavior after rejection = %q, want %q", got, desktopbridge.CloseAsk)
	}
}

func TestControllerCopyTextUsesNativeSystem(t *testing.T) {
	host, fakes := newCloseTestHost(t, desktopbridge.CloseAsk)
	const value = "sk-native-clipboard-test"

	if err := host.controller.CopyText(value); err != nil {
		t.Fatalf("CopyText() error = %v", err)
	}
	fakes.system.mu.Lock()
	defer fakes.system.mu.Unlock()
	if len(fakes.system.copied) != 1 || fakes.system.copied[0] != value {
		t.Fatalf("native clipboard values = %q, want [%q]", fakes.system.copied, value)
	}
}
