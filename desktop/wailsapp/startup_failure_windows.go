//go:build windows

package wailsapp

import (
	"github.com/a448582655/vibe-proxy/internal/buildinfo"
	"golang.org/x/sys/windows"
)

func reportNativeStartupFailure(logPath string) {
	message := "Vibe Proxy could not open its desktop window.\r\n\r\n"
	if logPath != "" {
		message += "A diagnostic was saved to:\r\n" + logPath + "\r\n\r\n"
	}
	message += "If this is a new Windows installation, install the Microsoft Edge WebView2 Runtime and try again.\r\n\r\n"
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
