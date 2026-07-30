package app

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/desktopbridge"
	"github.com/a448582655/vibe-proxy/internal/gatewayapp"
	"github.com/a448582655/vibe-proxy/internal/runtime"
)

func TestHostStartCreatesPrivateConfigAndBootstrapsRealGateway(t *testing.T) {
	fixture := newHostStartFixture(t)

	if err := fixture.host.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	info, err := os.Stat(fixture.paths.ConfigPath)
	if err != nil {
		t.Fatalf("Stat(config.yaml) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config.yaml permissions = %#o, want 0600", got)
	}
	cfg, err := config.LoadRuntime(fixture.paths.ConfigPath)
	if err != nil {
		t.Fatalf("LoadRuntime(config.yaml) error = %v", err)
	}
	if issues := config.ValidateRuntime(cfg); config.HasErrors(issues) {
		t.Fatalf("first-run config validation errors = %+v", issues)
	}
	if cfg.Server.Listen != "127.0.0.1:8080" {
		t.Fatalf("config listen = %q, want 127.0.0.1:8080", cfg.Server.Listen)
	}
	if cfg.Storage.SQLitePath != fixture.paths.DatabasePath {
		t.Fatalf("config sqlite path = %q, want %q", cfg.Storage.SQLitePath, fixture.paths.DatabasePath)
	}

	options, gateway := fixture.startCapture.values()
	rawToken, err := base64.RawURLEncoding.DecodeString(options.Runtime.AdminTokenOverride)
	if err != nil {
		t.Fatalf("admin token is not raw URL base64: %v", err)
	}
	if len(rawToken) != 32 {
		t.Fatalf("admin token bytes = %d, want 32", len(rawToken))
	}
	if options.Runtime.DesktopSessions == nil {
		t.Fatal("DesktopSessions = nil")
	}
	if options.Runtime.DesktopController == nil {
		t.Fatal("DesktopController = nil")
	}

	navigations := fixture.window.navigationTargets()
	if len(navigations) != 1 {
		t.Fatalf("Navigate calls = %v, want exactly one", navigations)
	}
	prefix := "http://" + gateway.Address() + "/desktop/bootstrap/"
	if !strings.HasPrefix(navigations[0], prefix) {
		t.Fatalf("navigation = %q, want prefix %q", navigations[0], prefix)
	}
	nonce := strings.TrimPrefix(navigations[0], prefix)
	if nonce == "" {
		t.Fatal("bootstrap nonce is empty")
	}
	if _, ok := options.Runtime.DesktopSessions.ConsumeBootstrap(nonce); !ok {
		t.Fatal("navigation nonce was not registered in desktop session store")
	}
	if _, reused := options.Runtime.DesktopSessions.ConsumeBootstrap(nonce); reused {
		t.Fatal("navigation nonce was reusable")
	}

	select {
	case <-gateway.Ready():
	default:
		t.Fatal("Host.Start returned before gateway readiness")
	}
	snapshot := options.Runtime.DesktopController.Snapshot()
	if !snapshot.Available || !snapshot.OwnsGateway {
		t.Fatalf("desktop snapshot = %+v, want available owned gateway", snapshot)
	}
	if snapshot.ListenAddress != gateway.Address() {
		t.Fatalf("snapshot listen = %q, want %q", snapshot.ListenAddress, gateway.Address())
	}
	if got := fixture.tray.statusValues(); len(got) != 2 || got[0] != "Starting" || got[1] != "Running on "+gateway.Address() {
		t.Fatalf("tray statuses = %v", got)
	}

	assertSecretsAbsentFromDesktopFiles(t, fixture.paths.DataDir, options.Runtime.AdminTokenOverride, nonce)
}

func TestHostStartNavigationFailureShutsDownOwnedGateway(t *testing.T) {
	fixture := newHostStartFixture(t)
	navigateErr := errors.New("webview navigation failed")
	fixture.window.navigateErr = navigateErr

	err := fixture.host.Start(context.Background())
	if !errors.Is(err, navigateErr) {
		t.Fatalf("Start() error = %v, want %v", err, navigateErr)
	}

	_, gateway := fixture.startCapture.values()
	select {
	case <-gateway.Done():
	case <-time.After(time.Second):
		t.Fatal("owned gateway was not cleaned up after navigation failure")
	}
	if got := fixture.tray.destroyCount(); got != 1 {
		t.Fatalf("Tray.Destroy calls = %d, want 1", got)
	}
	if got := fixture.tray.statusValues(); len(got) != 2 || got[0] != "Starting" || got[1] != "Error: "+navigateErr.Error() {
		t.Fatalf("tray statuses = %v", got)
	}
}

func TestHostStartFailureUpdatesTrayWithStructuredError(t *testing.T) {
	fixture := newHostStartFixture(t)
	startErr := &gatewayapp.StartupError{Stage: "listen", Address: "127.0.0.1:8080", Err: errors.New("address in use")}
	fixture.host.startGateway = func(context.Context, gatewayapp.Options) (*gatewayapp.App, error) {
		return nil, startErr
	}

	err := fixture.host.Start(context.Background())
	if !errors.Is(err, startErr) {
		t.Fatalf("Start() error = %v, want %v", err, startErr)
	}
	if got := fixture.tray.statusValues(); len(got) != 2 || got[0] != "Starting" || got[1] != "Error: "+startErr.Error() {
		t.Fatalf("tray statuses = %v", got)
	}
}

func TestHostShutdownCoordinatesBlockedStartup(t *testing.T) {
	fixture := newHostStartFixture(t)
	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	fixture.host.startGateway = func(ctx context.Context, options gatewayapp.Options) (*gatewayapp.App, error) {
		close(startEntered)
		<-releaseStart
		return fixture.startCapture.start(ctx, options)
	}

	var orderMu sync.Mutex
	gatewayExistedAtQuit := false
	gatewayStoppedAtQuit := false
	fixture.application.onQuit = func() {
		_, gateway := fixture.startCapture.values()
		orderMu.Lock()
		defer orderMu.Unlock()
		gatewayExistedAtQuit = gateway != nil
		if gateway != nil {
			select {
			case <-gateway.Done():
				gatewayStoppedAtQuit = true
			default:
			}
		}
	}

	startResult := make(chan error, 1)
	go func() {
		startResult <- fixture.host.Start(context.Background())
	}()
	select {
	case <-startEntered:
	case <-time.After(time.Second):
		t.Fatal("StartGateway was not entered")
	}

	fixture.host.RequestQuit(context.Background())
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fixture.host.Shutdown(cancelled); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Shutdown() error = %v", err)
	}
	close(releaseStart)
	if err := fixture.host.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	fixture.application.waitForQuit(t)

	var startErr error
	select {
	case startErr = <-startResult:
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after StartGateway was released")
	}
	_, gateway := fixture.startCapture.values()
	if gateway == nil {
		t.Fatal("StartGateway returned nil gateway")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = gateway.Shutdown(ctx)
	})

	if startErr == nil {
		t.Error("Start() error = nil after shutdown completed during startup")
	}
	select {
	case <-gateway.Done():
	default:
		t.Error("gateway remained running after shutdown raced with startup")
	}
	if got := fixture.window.navigationTargets(); len(got) != 0 {
		t.Errorf("Navigate calls = %v, want none", got)
	}
	if snapshot := fixture.host.controller.Snapshot(); snapshot.OwnsGateway || snapshot.ListenAddress != "" {
		t.Errorf("desktop snapshot after shutdown = %+v, want no ownership", snapshot)
	}
	orderMu.Lock()
	defer orderMu.Unlock()
	if !gatewayExistedAtQuit || !gatewayStoppedAtQuit {
		t.Errorf("Application.Quit ordering: gateway existed=%v stopped=%v, want true/true", gatewayExistedAtQuit, gatewayStoppedAtQuit)
	}
}

func TestHostControllerAndTrayOperationsUseAllowListedTargets(t *testing.T) {
	fixture := newHostStartFixture(t)
	if err := fixture.host.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	_, gateway := fixture.startCapture.values()

	fixture.host.OpenControlPlane()
	if err := fixture.host.CopyOpenAIBaseURL(); err != nil {
		t.Fatalf("CopyOpenAIBaseURL() error = %v", err)
	}
	if err := fixture.host.CopyAnthropicBaseURL(); err != nil {
		t.Fatalf("CopyAnthropicBaseURL() error = %v", err)
	}
	if err := fixture.host.controller.OpenDataDir(); err != nil {
		t.Fatalf("OpenDataDir() error = %v", err)
	}
	if shows, focuses := fixture.window.activationCounts(); shows != 1 || focuses != 1 {
		t.Fatalf("window activation = show:%d focus:%d, want 1 each", shows, focuses)
	}

	fixture.system.mu.Lock()
	defer fixture.system.mu.Unlock()
	base := "http://" + gateway.Address()
	if got := fixture.system.browsed; len(got) != 0 {
		t.Fatalf("browser targets = %v, want none", got)
	}
	if got := fixture.system.copied; len(got) != 2 || got[0] != base+"/v1" || got[1] != base+"/anthropic" {
		t.Fatalf("copied targets = %v", got)
	}
	if got := fixture.system.directories; len(got) != 1 || got[0] != fixture.paths.DataDir {
		t.Fatalf("opened directories = %v, want [%q]", got, fixture.paths.DataDir)
	}
}

type hostStartFixture struct {
	host         *Host
	paths        Paths
	window       *fakeWindow
	dialogs      *fakeDialogs
	tray         *fakeTray
	system       *fakeSystem
	application  *fakeApplication
	startCapture *gatewayStartCapture
}

func newHostStartFixture(t *testing.T) *hostStartFixture {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "desktop-state")
	paths := Paths{
		DataDir:         dataDir,
		ConfigPath:      filepath.Join(dataDir, "config.yaml"),
		DatabasePath:    filepath.Join(dataDir, "vibe-proxy.db"),
		PreferencesPath: filepath.Join(dataDir, "desktop.json"),
		LogDir:          filepath.Join(dataDir, "logs"),
		LogPath:         filepath.Join(dataDir, "logs", "vibe-proxy.log"),
	}
	preferences := DefaultPreferences()
	if err := SavePreferences(paths.PreferencesPath, preferences); err != nil {
		t.Fatalf("SavePreferences() error = %v", err)
	}
	fixture := &hostStartFixture{
		paths:        paths,
		window:       &fakeWindow{},
		dialogs:      &fakeDialogs{},
		tray:         &fakeTray{},
		system:       &fakeSystem{},
		application:  newFakeApplication(),
		startCapture: &gatewayStartCapture{},
	}
	host, err := NewHost(HostOptions{
		Paths:        paths,
		Preferences:  preferences,
		Window:       fixture.window,
		Dialogs:      fixture.dialogs,
		Tray:         fixture.tray,
		System:       fixture.system,
		Application:  fixture.application,
		StartGateway: fixture.startCapture.start,
	})
	if err != nil {
		t.Fatalf("NewHost() error = %v", err)
	}
	fixture.host = host
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = host.Shutdown(ctx)
	})
	return fixture
}

type gatewayStartCapture struct {
	mu      sync.Mutex
	options gatewayapp.Options
	app     *gatewayapp.App
}

func (c *gatewayStartCapture) start(ctx context.Context, options gatewayapp.Options) (*gatewayapp.App, error) {
	c.mu.Lock()
	c.options = cloneGatewayOptions(options)
	c.mu.Unlock()
	options.Listen = func(_, _ string) (net.Listener, error) {
		return net.Listen("tcp", "127.0.0.1:0")
	}
	app, err := gatewayapp.Start(ctx, options)
	c.mu.Lock()
	c.app = app
	c.mu.Unlock()
	return app, err
}

func (c *gatewayStartCapture) values() (gatewayapp.Options, *gatewayapp.App) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneGatewayOptions(c.options), c.app
}

func cloneGatewayOptions(options gatewayapp.Options) gatewayapp.Options {
	return gatewayapp.Options{
		ConfigPath: options.ConfigPath,
		Runtime: runtime.Options{
			AdminTokenOverride: options.Runtime.AdminTokenOverride,
			DesktopSessions:    options.Runtime.DesktopSessions,
			DesktopController:  options.Runtime.DesktopController,
		},
		Listen: options.Listen,
	}
}

func assertSecretsAbsentFromDesktopFiles(t *testing.T, dataDir string, secrets ...string) {
	t.Helper()
	err := filepath.WalkDir(dataDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".json", ".yaml", ".log":
		default:
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, secret := range secrets {
			if secret != "" && strings.Contains(string(contents), secret) {
				t.Errorf("%s contains generated secret", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%q) error = %v", dataDir, err)
	}
}

var _ desktopbridge.Controller = (*desktopController)(nil)
