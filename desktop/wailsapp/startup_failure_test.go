//go:build windows || darwin

package wailsapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a448582655/vibe-proxy/desktop/app"
)

func TestNativeErrorHandlerPersistsErrors(t *testing.T) {
	paths := app.Paths{LogPath: filepath.Join(t.TempDir(), "logs", "vibe-proxy.log")}

	NativeErrorHandler(paths)(errors.New("native startup diagnostic"))

	data, err := os.ReadFile(paths.LogPath)
	if err != nil {
		t.Fatalf("read startup log: %v", err)
	}
	if !strings.Contains(string(data), `stage="native"`) {
		t.Fatalf("startup log missing native stage: %q", data)
	}
	if !strings.Contains(string(data), `error="native startup diagnostic"`) {
		t.Fatalf("startup log missing error: %q", data)
	}
}
