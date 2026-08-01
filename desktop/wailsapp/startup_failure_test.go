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

func TestCaptureNativeStartupFailureUsesFallbackLog(t *testing.T) {
	blockedParent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("occupied"), 0o600); err != nil {
		t.Fatalf("write blocked parent: %v", err)
	}
	fallback := filepath.Join(t.TempDir(), "vibe-proxy-startup.log")

	failure := captureNativeStartupFailure(
		app.Paths{LogPath: filepath.Join(blockedParent, "vibe-proxy.log")},
		fallback,
		errors.New("native fallback diagnostic"),
	)

	if failure.logPath != fallback {
		t.Fatalf("log path = %q, want fallback %q", failure.logPath, fallback)
	}
	if failure.logWriteErr == nil || !strings.Contains(failure.logWriteErr.Error(), "primary diagnostic path") {
		t.Fatalf("log write error = %v, want primary path failure", failure.logWriteErr)
	}
	data, err := os.ReadFile(fallback)
	if err != nil {
		t.Fatalf("read fallback log: %v", err)
	}
	if !strings.Contains(string(data), "native fallback diagnostic") {
		t.Fatalf("fallback log missing diagnostic: %q", data)
	}
}

func TestDisplayStartupErrorIsBounded(t *testing.T) {
	message := displayStartupError(errors.New(strings.Repeat("界", startupErrorDisplayLimit+10)))
	if got := len([]rune(message)); got != startupErrorDisplayLimit+1 {
		t.Fatalf("display length = %d, want %d", got, startupErrorDisplayLimit+1)
	}
	if !strings.HasSuffix(message, "…") {
		t.Fatalf("display message missing truncation marker: %q", message)
	}
}
