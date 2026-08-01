//go:build windows || darwin

package wailsapp

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"sync"

	"github.com/a448582655/vibe-proxy/desktop/app"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type nativeWindow struct {
	window *application.WebviewWindow
}

var _ app.Window = (*nativeWindow)(nil)
var _ app.Application = (*nativeApplication)(nil)

func (w *nativeWindow) Show()                     { w.window.Show() }
func (w *nativeWindow) Hide()                     { w.window.Hide() }
func (w *nativeWindow) Focus()                    { w.window.Focus() }
func (w *nativeWindow) Navigate(url string) error { w.window.SetURL(url); return nil }

type nativeApplication struct {
	application *application.App
}

func (a *nativeApplication) Quit() { a.application.Quit() }

// Run creates the Wails shell and connects it to the platform-neutral Host.
func Run(ctx context.Context) error {
	paths, err := app.PlatformPaths()
	if err != nil {
		return fmt.Errorf("resolve desktop paths: %w", err)
	}
	preferences, err := app.LoadPreferences(paths.PreferencesPath)
	if err != nil {
		return fmt.Errorf("load desktop preferences: %w", err)
	}

	var mainWindow *application.WebviewWindow
	nativeApp := application.New(application.Options{
		Name:   "Vibe Proxy",
		Assets: application.AssetOptions{Handler: http.NotFoundHandler()},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: uniqueInstanceID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				mainWindow.Show()
				mainWindow.Focus()
			},
		},
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		Mac:     application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
	})

	windowContract := MainWindowContract()
	mainWindow = nativeApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: windowContract.Name, Title: windowContract.Title,
		Width: preferences.Window.Width, Height: preferences.Window.Height,
		MinWidth: windowContract.MinWidth, MinHeight: windowContract.MinHeight,
		Hidden: true,
	})
	var preferencesMu sync.Mutex
	persistWindowSize := func() {
		width, height := mainWindow.Size()
		if width <= 0 || height <= 0 {
			return
		}
		preferencesMu.Lock()
		defer preferencesMu.Unlock()
		next := preferences
		next.Window.Width = width
		next.Window.Height = height
		if err := app.SavePreferences(paths.PreferencesPath, next); err != nil {
			log.Printf("save desktop window size: %v", err)
			return
		}
		preferences = next
	}

	actions := &trayActions{}
	tray := newNativeTray(nativeApp, mainWindow, actions, trayIconBytes(), trayTemplateIconBytes())
	host, err := app.NewHost(app.HostOptions{
		Paths: paths, Preferences: preferences,
		Window:  &nativeWindow{window: mainWindow},
		Dialogs: &nativeDialogs{application: nativeApp, window: mainWindow},
		Tray:    tray, System: &nativeSystem{application: nativeApp},
		Application: &nativeApplication{application: nativeApp},
	})
	if err != nil {
		tray.Destroy()
		return fmt.Errorf("create desktop host: %w", err)
	}

	actions.openControlPlane = host.OpenControlPlane
	actions.copyOpenAI = host.CopyOpenAIBaseURL
	actions.copyAnthropic = host.CopyAnthropicBaseURL
	actions.openLogs = host.OpenLogsFolder
	actions.openBrowser = host.OpenControlPlaneInBrowser
	actions.resetClose = host.ResetCloseBehavior
	actions.requestQuit = func() { host.RequestQuit(context.Background()) }
	mainWindow.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) {
		// Persist outside the Wails event callback so the native event loop is not
		// blocked by filesystem I/O or a synchronous Size() query.
		go persistWindowSize()
	})

	mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		go host.HandleWindowClose(context.Background())
	})
	nativeApp.OnShutdown(func() {
		persistWindowSize()
		if err := host.Shutdown(context.Background()); err != nil {
			log.Printf("desktop shutdown: %v", err)
		}
		tray.Destroy()
	})
	nativeApp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			if err := host.Start(ctx); err != nil {
				fmt.Fprintln(os.Stderr, "vibe-proxy desktop startup:", err)
				if recoveryErr := host.RecoverStartup(ctx, err); recoveryErr != nil {
					fmt.Fprintln(os.Stderr, "vibe-proxy desktop recovery:", recoveryErr)
				}
				return
			}
			mainWindow.Show()
			mainWindow.Focus()
		}()
	})

	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		return fmt.Errorf("unsupported desktop platform %q", runtime.GOOS)
	}
	return nativeApp.Run()
}
