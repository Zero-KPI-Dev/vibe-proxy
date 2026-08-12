//go:build windows || darwin

package wailsapp

import (
	"errors"
	"sync"

	"github.com/a448582655/vibe-proxy/desktop/app"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type nativeSystem struct {
	application *application.App
	clipboardMu sync.Mutex
}

var _ app.System = (*nativeSystem)(nil)

func (s *nativeSystem) CopyText(value string) error {
	s.clipboardMu.Lock()
	defer s.clipboardMu.Unlock()
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
