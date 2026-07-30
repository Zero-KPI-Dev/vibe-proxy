//go:build windows || darwin

package wailsapp

import (
	"log"
	"runtime"
	"sync"

	"github.com/a448582655/vibe-proxy/desktop/app"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type trayActions struct {
	openControlPlane func()
	copyOpenAI       func() error
	copyAnthropic    func() error
	openLogs         func() error
	openBrowser      func() error
	resetClose       func() error
	requestQuit      func()
}

type nativeTray struct {
	tray        *application.SystemTray
	menu        *application.Menu
	status      *application.MenuItem
	actions     *trayActions
	destroyOnce sync.Once
}

var _ app.Tray = (*nativeTray)(nil)

func newNativeTray(nativeApp *application.App, mainWindow *application.WebviewWindow, actions *trayActions, icon, templateIcon []byte) *nativeTray {
	menu := nativeApp.NewMenu()
	menu.Add("Open Control Plane").OnClick(func(*application.Context) { invoke(actions.openControlPlane) })
	status := menu.Add("Starting").SetEnabled(false)
	menu.AddSeparator()
	menu.Add("Copy OpenAI Base URL").OnClick(func(*application.Context) { invokeError(actions.copyOpenAI) })
	menu.Add("Copy Anthropic Base URL").OnClick(func(*application.Context) { invokeError(actions.copyAnthropic) })
	menu.Add("Open Logs Folder").OnClick(func(*application.Context) { invokeError(actions.openLogs) })
	menu.Add("Open in Browser").OnClick(func(*application.Context) { invokeError(actions.openBrowser) })
	menu.AddSeparator()
	menu.Add("Reset Close Behavior").OnClick(func(*application.Context) { invokeError(actions.resetClose) })
	menu.Add("Exit vibe-proxy").OnClick(func(*application.Context) { invoke(actions.requestQuit) })

	tray := nativeApp.SystemTray.New().SetMenu(menu).OnClick(func() {
		mainWindow.Show()
		mainWindow.Focus()
	})
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(templateIcon)
	} else {
		tray.SetIcon(icon)
	}
	return &nativeTray{tray: tray, menu: menu, status: status, actions: actions}
}

func (t *nativeTray) SetStatus(status string) {
	t.status.SetLabel(status)
	t.menu.Update()
}

func (t *nativeTray) Destroy() {
	t.destroyOnce.Do(func() { t.tray.Destroy() })
}

func invoke(action func()) {
	if action != nil {
		action()
	}
}

func invokeError(action func() error) {
	if action == nil {
		return
	}
	if err := action(); err != nil {
		log.Printf("desktop tray action: %v", err)
	}
}
