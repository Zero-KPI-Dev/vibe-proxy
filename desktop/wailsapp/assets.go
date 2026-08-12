package wailsapp

import _ "embed"

// Tray images live beside the package so Go can embed them directly into the
// desktop binary. The color asset is used on Windows; macOS receives the
// monochrome template and applies the current menu-bar tint automatically.
var (
	//go:embed assets/tray.png
	trayIcon []byte

	//go:embed assets/tray-template.png
	trayTemplateIcon []byte
)

func trayIconBytes() []byte {
	return append([]byte(nil), trayIcon...)
}

func trayTemplateIconBytes() []byte {
	return append([]byte(nil), trayTemplateIcon...)
}
