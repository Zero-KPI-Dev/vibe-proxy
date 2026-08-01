package wailsapp

import (
	"reflect"
	"testing"
)

func TestAdapterMainWindowContract(t *testing.T) {
	window := MainWindowContract()
	if window.Name != "main" || window.Title != "Vibe Proxy" {
		t.Fatalf("window identity = %#v, want main/Vibe Proxy", window)
	}
	if window.Width != 1280 || window.Height != 800 {
		t.Fatalf("window size = %dx%d, want 1280x800", window.Width, window.Height)
	}
	if window.MinWidth != 960 || window.MinHeight != 640 {
		t.Fatalf("window minimum = %dx%d, want 960x640", window.MinWidth, window.MinHeight)
	}
}

func TestAdapterPlatformLifecycleContract(t *testing.T) {
	if !PlatformContract("windows").DisableQuitOnLastWindowClosed {
		t.Fatal("Windows must disable quit on last window closed")
	}
	if PlatformContract("darwin").ApplicationShouldTerminateAfterLastWindowClosed {
		t.Fatal("macOS must not terminate after its last window closes")
	}
	if got, want := ApplicationContract().UniqueInstanceID, "io.vibeproxy.desktop"; got != want {
		t.Fatalf("unique instance ID = %q, want %q", got, want)
	}
	if got, want := ApplicationContract().SecondInstanceActions, []string{"show", "focus"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second instance actions = %v, want %v", got, want)
	}
}

func TestAdapterCloseAndShutdownContract(t *testing.T) {
	closeHook := ApplicationContract().CloseHook
	if !closeHook.Cancellable || !closeHook.DelegatesToHost || !closeHook.Asynchronous {
		t.Fatalf("close hook = %#v, want cancellable asynchronous host delegation", closeHook)
	}
	if got, want := ApplicationContract().ShutdownSequence, []string{"gateway", "tray_destroy", "application_quit"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("shutdown sequence = %v, want %v", got, want)
	}
}

func TestAdapterTrayContract(t *testing.T) {
	tray := TrayContract()
	if got, want := tray.Items, []TrayItem{
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
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tray items = %#v, want %#v", got, want)
	}
}
