//go:build windows

package wailsapp

import (
	"github.com/a448582655/vibe-proxy/internal/buildinfo"
	"golang.org/x/sys/windows"
)

func reportNativeStartupFailure(failure nativeStartupFailure) {
	message := "Vibe Proxy could not open its desktop window.\r\n\r\n"
	message += "Actual error:\r\n" + displayStartupError(failure.cause) + "\r\n\r\n"
	if failure.logPath != "" {
		message += "A diagnostic was saved to:\r\n" + failure.logPath + "\r\n\r\n"
	}
	if failure.logWriteErr != nil {
		message += "Diagnostic note:\r\n" + displayStartupError(failure.logWriteErr) + "\r\n\r\n"
	}
	message += "Vibe Proxy requires Microsoft Edge WebView2 Runtime, but this message does not by itself mean that WebView2 is missing. Use the actual error above to diagnose the failure.\r\n\r\n"
	message += buildinfo.String()

	text, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	title, err := windows.UTF16PtrFromString("Vibe Proxy")
	if err != nil {
		return
	}
	_, _ = windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR|windows.MB_SYSTEMMODAL)
}
