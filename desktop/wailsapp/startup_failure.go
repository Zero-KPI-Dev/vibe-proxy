//go:build windows || darwin

package wailsapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/a448582655/vibe-proxy/desktop/app"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const startupErrorDisplayLimit = 1600

type nativeStartupFailure struct {
	cause       error
	logPath     string
	logWriteErr error
}

// ReportStartupFailure is the last-resort error path for failures returned by
// the native Wails runtime itself. In particular, it runs when WebView2 is
// missing or cannot be initialised, before the in-app recovery UI exists.
//
// The desktop binary uses the Windows GUI subsystem, so stderr is not visible
// when the user launches it by double-clicking. Always persist a diagnostic
// first, then use the platform's visible fallback notification.
func ReportStartupFailure(startupErr error) {
	if startupErr == nil {
		return
	}

	paths, pathErr := app.PlatformPaths()
	failure := captureNativeStartupFailure(paths, fallbackStartupLogPath(), startupErr)
	if pathErr != nil {
		failure.logWriteErr = errors.Join(fmt.Errorf("resolve desktop paths: %w", pathErr), failure.logWriteErr)
	}
	reportNativeStartupFailure(failure)
}

// NativeErrorHandler returns the Wails error handler used while the native
// shell is being initialised. Wails reports some unrecoverable failures to
// Options.ErrorHandler and then calls os.Exit(1) internally, so they never
// reach the error returned by App.Run. A GUI application cannot rely on
// stderr in that situation: persist the error and show the same visible
// fallback that the top-level launcher uses for returned errors.
func NativeErrorHandler(paths app.Paths) func(error) {
	var mu sync.Mutex
	return func(err error) {
		if err == nil {
			return
		}

		mu.Lock()
		defer mu.Unlock()
		failure := captureNativeStartupFailure(paths, fallbackStartupLogPath(), err)

		var fatalErr *application.FatalError
		if errors.As(err, &fatalErr) {
			reportNativeStartupFailure(failure)
		}
	}
}

func captureNativeStartupFailure(paths app.Paths, fallbackLogPath string, startupErr error) nativeStartupFailure {
	failure := nativeStartupFailure{cause: startupErr}
	primaryErr := app.TryWriteStartupErrorLog(paths, "native", "", startupErr)
	if primaryErr == nil {
		failure.logPath = paths.LogPath
		return failure
	}

	fallbackPaths := app.Paths{LogPath: fallbackLogPath}
	fallbackErr := app.TryWriteStartupErrorLog(fallbackPaths, "native", "", startupErr)
	if fallbackErr == nil {
		failure.logPath = fallbackLogPath
		failure.logWriteErr = fmt.Errorf("primary diagnostic path %q failed: %w", paths.LogPath, primaryErr)
		return failure
	}

	failure.logWriteErr = errors.Join(
		fmt.Errorf("primary diagnostic path %q failed: %w", paths.LogPath, primaryErr),
		fmt.Errorf("fallback diagnostic path %q failed: %w", fallbackLogPath, fallbackErr),
	)
	return failure
}

func fallbackStartupLogPath() string {
	return filepath.Join(os.TempDir(), "vibe-proxy-startup.log")
}

func displayStartupError(startupErr error) string {
	if startupErr == nil {
		return "Unknown native startup error."
	}
	var fatalErr *application.FatalError
	if errors.As(startupErr, &fatalErr) {
		if cause := errors.Unwrap(fatalErr); cause != nil {
			startupErr = cause
		}
	}
	message := strings.TrimSpace(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(startupErr.Error()))
	runes := []rune(message)
	if len(runes) > startupErrorDisplayLimit {
		message = string(runes[:startupErrorDisplayLimit]) + "…"
	}
	return message
}
