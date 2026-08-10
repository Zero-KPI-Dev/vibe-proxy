package wailsapp

import (
	"bytes"
	"image"
	_ "image/png"
	"testing"
)

func TestEmbeddedTrayAssetsAreValid64PixelPNGs(t *testing.T) {
	for name, data := range map[string][]byte{
		"color":    trayIconBytes(),
		"template": trayTemplateIconBytes(),
	} {
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("decode %s tray icon: %v", name, err)
		}
		if format != "png" || config.Width != 64 || config.Height != 64 {
			t.Errorf("%s tray icon = %s %dx%d, want png 64x64", name, format, config.Width, config.Height)
		}
	}
}
