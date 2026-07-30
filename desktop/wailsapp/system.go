//go:build windows || darwin

package wailsapp

import (
	"errors"

	"github.com/a448582655/vibe-proxy/desktop/app"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type nativeSystem struct {
	application *application.App
}

var _ app.System = (*nativeSystem)(nil)

func (s *nativeSystem) CopyText(value string) error {
	if ok := s.application.Clipboard.SetText(value); !ok {
		return errors.New("set native clipboard text")
	}
	return nil
}

func (s *nativeSystem) OpenBrowser(target string) error {
	return s.application.Browser.OpenURL(target)
}

func (s *nativeSystem) OpenDirectory(path string) error {
	return s.application.Env.OpenFileManager(path, false)
}
