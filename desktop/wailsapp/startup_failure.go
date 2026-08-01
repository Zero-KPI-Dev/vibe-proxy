//go:build windows || darwin

package wailsapp

import "github.com/a448582655/vibe-proxy/desktop/app"

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
