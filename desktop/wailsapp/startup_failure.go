//go:build windows || darwin

package wailsapp

import (
	"errors"
	"sync"

	"github.com/a448582655/vibe-proxy/desktop/app"
	"github.com/wailsapp/wails/v3/pkg/application"
)

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

	logPath := ""
	if paths, err := app.PlatformPaths(); err == nil {
		logPath = paths.LogPath
		app.WriteStartupErrorLog(paths, "native", "", startupErr)
	}
	reportNativeStartupFailure(logPath)
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
		app.WriteStartupErrorLog(paths, "native", "", err)

		var fatalErr *application.FatalError
		if errors.As(err, &fatalErr) {
			reportNativeStartupFailure(paths.LogPath)
		}
	}
}
