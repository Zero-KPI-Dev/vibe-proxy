// Package wailsapp adapts the platform-neutral desktop host to Wails.
package wailsapp

const (
	mainWindowName   = "main"
	mainWindowTitle  = "Vibe Proxy"
	uniqueInstanceID = "io.vibeproxy.desktop"
)

// WindowContract describes the native main window without depending on Wails.
type WindowContract struct {
	Name      string
	Title     string
	Width     int
	Height    int
	MinWidth  int
	MinHeight int
}

// PlatformLifecycleContract describes platform-specific application policies.
type PlatformLifecycleContract struct {
	DisableQuitOnLastWindowClosed                   bool
	ApplicationShouldTerminateAfterLastWindowClosed bool
}

// CloseHookContract documents how the native close callback reaches the Host.
type CloseHookContract struct {
	Cancellable     bool
	DelegatesToHost bool
	Asynchronous    bool
}

// ApplicationLifecycleContract describes the process-wide native lifecycle.
type ApplicationLifecycleContract struct {
	UniqueInstanceID      string
	SecondInstanceActions []string
	CloseHook             CloseHookContract
	ShutdownSequence      []string
}

// TrayItem describes one native tray-menu item in visible order.
type TrayItem struct {
	Label     string
	Action    string
	Disabled  bool
	Separator bool
}

// TrayMenuContract describes the stable tray menu layout.
type TrayMenuContract struct {
	Items []TrayItem
}

func MainWindowContract() WindowContract {
	return WindowContract{
		Name: mainWindowName, Title: mainWindowTitle,
		Width: 1280, Height: 800, MinWidth: 960, MinHeight: 640,
	}
}

func PlatformContract(goos string) PlatformLifecycleContract {
	return PlatformLifecycleContract{
		DisableQuitOnLastWindowClosed:                   goos == "windows",
		ApplicationShouldTerminateAfterLastWindowClosed: false,
	}
}

func ApplicationContract() ApplicationLifecycleContract {
	return ApplicationLifecycleContract{
		UniqueInstanceID:      uniqueInstanceID,
		SecondInstanceActions: []string{"show", "focus"},
		CloseHook: CloseHookContract{
			Cancellable: true, DelegatesToHost: true, Asynchronous: true,
		},
		ShutdownSequence: []string{"gateway", "tray_destroy", "application_quit"},
	}
}

func TrayContract() TrayMenuContract {
	return TrayMenuContract{Items: []TrayItem{
		{Label: "Open Control Plane", Action: "open_control_plane"},
		{Label: "status", Disabled: true},
		{Separator: true},
		{Label: "Copy OpenAI Base URL", Action: "copy_openai_base_url"},
		{Label: "Copy Anthropic Base URL", Action: "copy_anthropic_base_url"},
		{Label: "Open Logs Folder", Action: "open_logs_folder"},
		{Label: "Open in Browser", Action: "open_in_browser"},
		{Separator: true},
		{Label: "Reset Close Behavior", Action: "reset_close_behavior"},
		{Label: "Exit vibe-proxy", Action: "request_quit"},
	}}
}
