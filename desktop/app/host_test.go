package app

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/auth"
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
	if cfg.Server.AdminListen != "127.0.0.1:8081" {
		t.Fatalf("config admin listen = %q, want 127.0.0.1:8081", cfg.Server.AdminListen)
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
	if options.Runtime.PasswordAuth == nil {
		t.Fatal("PasswordAuth = nil")
	}
	if options.Runtime.PasswordAuth.Initialized() {
		t.Fatal("fresh desktop management password store was already initialized")
	}
	if _, err := os.Stat(fixture.paths.AuthPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("auth file exists before first-run setup: %v", err)
	}
	if options.Runtime.DesktopController == nil {
		t.Fatal("DesktopController = nil")
	}

	navigations := fixture.window.navigationTargets()
	if len(navigations) != 1 {
		t.Fatalf("Navigate calls = %v, want exactly one", navigations)
	}
	prefix := "http://" + gateway.AdminAddress() + "/desktop/bootstrap/"
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
	if snapshot.DataAddress != gateway.Address() || snapshot.AdminAddress != gateway.AdminAddress() {
		t.Fatalf("desktop endpoint snapshot = %+v", snapshot)
	}
	if got := fixture.tray.statusValues(); len(got) != 2 || got[0] != "Starting" || got[1] != "Running on "+gateway.Address() {
		t.Fatalf("tray statuses = %v", got)
	}

	assertSecretsAbsentFromDesktopFiles(t, fixture.paths.DataDir, options.Runtime.AdminTokenOverride, nonce)
}

func TestHostStartUsesLoopbackURLsForWildcardListener(t *testing.T) {
	fixture := newHostStartFixture(t)
	fixture.startCapture.listenAddress = "0.0.0.0:0"

	if err := fixture.host.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	options, gateway := fixture.startCapture.values()
	wantDataAddress := browserAddress(gateway.Address())
	if wantDataAddress == gateway.Address() {
		t.Fatalf("browser address %q was not rewritten from wildcard listener", wantDataAddress)
	}
	response, err := (&http.Client{Timeout: time.Second}).Get("http://" + wantDataAddress + "/healthz")
	if err != nil {
		t.Fatalf("GET healthz through loopback address: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET healthz status = %d, want 200", response.StatusCode)
	}
	navigations := fixture.window.navigationTargets()
	if len(navigations) != 1 {
		t.Fatalf("Navigate calls = %v, want exactly one", navigations)
	}
	assertBrowserBootstrapTarget(t, navigations[0], gateway.AdminAddress(), options.Runtime.DesktopSessions)

	if err := fixture.host.CopyOpenAIBaseURL(); err != nil {
		t.Fatalf("CopyOpenAIBaseURL() error = %v", err)
	}
	if err := fixture.host.OpenControlPlaneInBrowser(); err != nil {
		t.Fatalf("OpenControlPlaneInBrowser() error = %v", err)
	}
	fixture.system.mu.Lock()
	defer fixture.system.mu.Unlock()
	if got := fixture.system.copied; len(got) != 1 || got[0] != "http://"+wantDataAddress+"/v1" {
		t.Fatalf("copied targets = %v, want loopback address", got)
	}
	if got := fixture.system.browsed; len(got) != 1 {
		t.Fatalf("browser targets = %v, want exactly one", got)
	} else {
		assertBrowserBootstrapTarget(t, got[0], gateway.AdminAddress(), options.Runtime.DesktopSessions)
	}
}

func TestHostStartNavigationFailureKeepsGatewayForBrowserFallback(t *testing.T) {
	fixture := newHostStartFixture(t)
	navigateErr := errors.New("webview navigation failed")
	fixture.window.navigateErr = navigateErr

	err := fixture.host.Start(context.Background())
	if !errors.Is(err, navigateErr) {
		t.Fatalf("Start() error = %v, want %v", err, navigateErr)
	}

	options, gateway := fixture.startCapture.values()
	select {
	case <-gateway.Done():
		t.Fatal("owned gateway was stopped after WebView navigation failure")
	default:
	}
	if got := fixture.tray.destroyCount(); got != 0 {
		t.Fatalf("Tray.Destroy calls = %d, want 0 before terminal shutdown", got)
	}
	if got := fixture.tray.statusValues(); len(got) != 2 || got[0] != "Starting" || got[1] != "Running; desktop window unavailable" {
		t.Fatalf("tray statuses = %v", got)
	}
	if snapshot := fixture.host.controller.Snapshot(); !snapshot.OwnsGateway || snapshot.ListenAddress != gateway.Address() {
		t.Fatalf("desktop snapshot = %+v, want owned running gateway", snapshot)
	}
	if err := fixture.host.OpenControlPlaneInBrowser(); err != nil {
		t.Fatalf("OpenControlPlaneInBrowser() error = %v", err)
	}
	if err := options.Runtime.PasswordAuth.SetInitial("desktop browser password"); err != nil {
		t.Fatalf("SetInitial() error = %v", err)
	}
	if err := fixture.host.OpenControlPlaneInBrowser(); err != nil {
		t.Fatalf("OpenControlPlaneInBrowser() after password setup error = %v", err)
	}
	fixture.system.mu.Lock()
	defer fixture.system.mu.Unlock()
	base := "http://" + gateway.AdminAddress()
	if got := fixture.system.browsed; len(got) != 2 {
		t.Fatalf("browser targets = %v, want first-run bootstrap and password login targets", got)
	} else {
		assertBrowserBootstrapTarget(t, got[0], gateway.AdminAddress(), options.Runtime.DesktopSessions)
		if got[1] != base+"/" {
			t.Fatalf("post-setup browser target = %q, want %q", got[1], base+"/")
		}
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

func TestHostShutdownWaitsForNavigationTerminalPath(t *testing.T) {
	fixture := newHostStartFixture(t)
	navigateEntered := make(chan struct{})
	releaseNavigate := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseNavigate) }) }
	defer release()
	fixture.window.onNavigate = func(string) {
		close(navigateEntered)
		<-releaseNavigate
	}

	var orderMu sync.Mutex
	gatewayStoppedAtQuit := false
	fixture.application.onQuit = func() {
		_, gateway := fixture.startCapture.values()
		orderMu.Lock()
		defer orderMu.Unlock()
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
	case <-navigateEntered:
	case <-time.After(time.Second):
		t.Fatal("Navigate was not entered")
	}

	fixture.host.RequestQuit(context.Background())
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fixture.host.Shutdown(cancelled); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Shutdown() error = %v", err)
	}
	select {
	case <-fixture.host.shutdownDone:
		t.Error("shutdown completed while Navigate was still active")
	default:
	}
	select {
	case <-fixture.application.quit:
		t.Error("Application.Quit ran while Navigate was still active")
	default:
	}

	release()
	var startErr error
	select {
	case startErr = <-startResult:
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after Navigate was released")
	}
	if startErr == nil {
		t.Error("Start() error = nil after shutdown was requested during Navigate")
	}
	if err := fixture.host.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	fixture.application.waitForQuit(t)

	_, gateway := fixture.startCapture.values()
	if gateway == nil {
		t.Fatal("StartGateway returned nil gateway")
	}
	select {
	case <-gateway.Done():
	default:
		t.Error("gateway remained running after navigation/shutdown coordination")
	}
	if got := fixture.window.navigationTargets(); len(got) != 1 {
		t.Errorf("Navigate calls = %v, want only the pre-shutdown call", got)
	}
	if got := fixture.tray.statusValues(); len(got) != 1 || got[0] != "Starting" {
		t.Errorf("tray statuses = %v, want [Starting]", got)
	}
	if snapshot := fixture.host.controller.Snapshot(); snapshot.OwnsGateway || snapshot.ListenAddress != "" {
		t.Errorf("desktop snapshot after shutdown = %+v, want no ownership", snapshot)
	}
	orderMu.Lock()
	defer orderMu.Unlock()
	if !gatewayStoppedAtQuit {
		t.Error("Application.Quit ran before the published gateway stopped")
	}
}

func TestHostQuitDoesNotExitOnBlockedStartupTimeout(t *testing.T) {
	fixture := newHostStartFixture(t)
	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseStart) }) }
	defer release()
	fixture.host.startGateway = func(ctx context.Context, options gatewayapp.Options) (*gatewayapp.App, error) {
		close(startEntered)
		<-releaseStart
		return fixture.startCapture.start(ctx, options)
	}

	shutdownDeadline := newManualDeadlineContext()
	shutdownContextCreated := make(chan struct{})
	var contextMu sync.Mutex
	contextCalls := 0
	fixture.host.newShutdownContext = func() (context.Context, context.CancelFunc) {
		contextMu.Lock()
		defer contextMu.Unlock()
		contextCalls++
		if contextCalls == 1 {
			close(shutdownContextCreated)
			return shutdownDeadline, func() {}
		}
		return context.WithTimeout(context.Background(), time.Second)
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
	select {
	case <-shutdownContextCreated:
	case <-time.After(time.Second):
		t.Fatal("shutdown context was not created")
	}
	shutdownDeadline.expire()
	select {
	case <-fixture.host.shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after its deadline")
	}

	firstErr := fixture.host.shutdownErr
	if !errors.Is(firstErr, context.DeadlineExceeded) {
		t.Fatalf("shutdown error before late startup cleanup = %v, want deadline exceeded", firstErr)
	}
	if err := fixture.host.Shutdown(context.Background()); err != firstErr {
		t.Fatalf("repeated Shutdown() error = %v, want stable %v", err, firstErr)
	}
	select {
	case <-fixture.application.quit:
		t.Fatal("Application.Quit ran after ownership-safe shutdown timed out")
	default:
	}
	if got := fixture.tray.destroyCount(); got != 0 {
		t.Fatalf("Tray.Destroy calls after shutdown timeout = %d, want 0", got)
	}

	release()
	var startErr error
	select {
	case startErr = <-startResult:
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after StartGateway was released")
	}
	if startErr == nil {
		t.Error("Start() error = nil after shutdown timed out during startup")
	}
	_, gateway := fixture.startCapture.values()
	if gateway == nil {
		t.Fatal("StartGateway returned nil gateway")
	}
	select {
	case <-gateway.Done():
	default:
		t.Error("late gateway remained running after startup was released")
	}
	if got := fixture.window.navigationTargets(); len(got) != 0 {
		t.Errorf("Navigate calls = %v, want none", got)
	}
	if snapshot := fixture.host.controller.Snapshot(); snapshot.OwnsGateway || snapshot.ListenAddress != "" {
		t.Errorf("desktop snapshot after late cleanup = %+v, want no ownership", snapshot)
	}
	select {
	case <-fixture.application.quit:
		t.Error("Application.Quit ran after late gateway cleanup with a finalized shutdown error")
	default:
	}
	if got := fixture.host.shutdownErr; got != firstErr {
		t.Errorf("shutdown error changed after Done closed: before=%v after=%v", firstErr, got)
	}
}

func TestHostLateStartupCleanupFailurePreventsQuitAndRetainsOwnership(t *testing.T) {
	fixture := newHostStartFixture(t)
	gatewayStarted := make(chan struct{})
	releaseStart := make(chan struct{})
	var releaseStartOnce sync.Once
	releaseStartup := func() { releaseStartOnce.Do(func() { close(releaseStart) }) }
	defer releaseStartup()
	fixture.host.startGateway = func(ctx context.Context, options gatewayapp.Options) (*gatewayapp.App, error) {
		gateway, err := fixture.startCapture.start(ctx, options)
		close(gatewayStarted)
		<-releaseStart
		return gateway, err
	}

	startResult := make(chan error, 1)
	go func() {
		startResult <- fixture.host.Start(context.Background())
	}()
	select {
	case <-gatewayStarted:
	case <-time.After(time.Second):
		t.Fatal("real gateway was not started")
	}
	options, gateway := fixture.startCapture.values()
	releaseRequest := startBlockingOpenDataRequest(t, fixture, gateway, options.Runtime.AdminTokenOverride)
	defer releaseRequest()

	executeCtx, cancelExecute := context.WithCancel(context.Background())
	defer cancelExecute()
	cleanupDeadline := newManualDeadlineContext()
	cleanupDeadline.expire()
	executeContextCreated := make(chan struct{})
	cleanupContextCreated := make(chan struct{})
	var contextMu sync.Mutex
	contextCalls := 0
	fixture.host.newShutdownContext = func() (context.Context, context.CancelFunc) {
		contextMu.Lock()
		defer contextMu.Unlock()
		contextCalls++
		switch contextCalls {
		case 1:
			close(executeContextCreated)
			return executeCtx, cancelExecute
		case 2:
			close(cleanupContextCreated)
			return cleanupDeadline, func() {}
		default:
			return context.WithTimeout(context.Background(), time.Second)
		}
	}

	fixture.host.RequestQuit(context.Background())
	select {
	case <-executeContextCreated:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not begin")
	}
	releaseStartup()
	select {
	case <-cleanupContextCreated:
	case <-time.After(time.Second):
		t.Fatal("late startup cleanup did not begin")
	}

	var startErr error
	select {
	case startErr = <-startResult:
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after failed cleanup")
	}
	if !errors.Is(startErr, context.DeadlineExceeded) {
		t.Fatalf("Start() error = %v, want cleanup deadline exceeded", startErr)
	}
	select {
	case <-fixture.host.shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not publish the startup cleanup failure")
	}
	firstErr := fixture.host.shutdownErr
	if !errors.Is(firstErr, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want cleanup deadline exceeded", firstErr)
	}
	if got := fixture.tray.statusValues(); len(got) != 2 || got[0] != "Starting" || got[1] != "Error: "+firstErr.Error() {
		t.Fatalf("tray statuses after late cleanup failure = %v, want Starting then observable shutdown error", got)
	}
	select {
	case <-fixture.application.quit:
		t.Fatal("Application.Quit ran after late startup cleanup failed")
	default:
	}
	select {
	case <-gateway.Done():
		t.Fatal("gateway reached Done before the blocked handler was released")
	default:
	}
	if snapshot := fixture.host.controller.Snapshot(); !snapshot.OwnsGateway || snapshot.ListenAddress != gateway.Address() {
		t.Fatalf("desktop snapshot after cleanup failure = %+v, want retained gateway ownership", snapshot)
	}
	if err := fixture.host.Shutdown(context.Background()); err != firstErr {
		t.Fatalf("repeated Shutdown() error = %v, want stable %v", err, firstErr)
	}

	releaseRequest()
	waitForGatewayDone(t, gateway)
	if err := fixture.host.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error after gateway Done = %v, want nil", err)
	}
	if snapshot := fixture.host.controller.Snapshot(); snapshot.OwnsGateway || snapshot.ListenAddress != "" {
		t.Fatalf("desktop snapshot after gateway Done = %+v, want released ownership", snapshot)
	}
	if got := fixture.tray.destroyCount(); got != 1 {
		t.Fatalf("Tray.Destroy calls after confirmed cleanup = %d, want 1", got)
	}
	select {
	case <-fixture.application.quit:
		t.Error("Application.Quit ran after a failed cleanup later reached Done")
	default:
	}
}

func TestHostGatewayShutdownFailureHasStableObservableTerminalState(t *testing.T) {
	fixture := newHostStartFixture(t)
	if err := fixture.host.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	options, gateway := fixture.startCapture.values()
	releaseRequest := startBlockingOpenDataRequest(t, fixture, gateway, options.Runtime.AdminTokenOverride)
	defer releaseRequest()

	shutdownDeadline := newManualDeadlineContext()
	shutdownContextCreated := make(chan struct{})
	var contextOnce sync.Once
	fixture.host.newShutdownContext = func() (context.Context, context.CancelFunc) {
		contextOnce.Do(func() { close(shutdownContextCreated) })
		return shutdownDeadline, func() {}
	}

	fixture.host.RequestQuit(context.Background())
	select {
	case <-shutdownContextCreated:
	case <-time.After(time.Second):
		t.Fatal("shutdown context was not created")
	}
	shutdownDeadline.expire()
	select {
	case <-fixture.host.shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after its deadline")
	}

	firstErr := fixture.host.shutdownErr
	if !errors.Is(firstErr, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want deadline exceeded", firstErr)
	}
	wantRunning := "Running on " + gateway.Address()
	if got := fixture.tray.statusValues(); len(got) != 3 || got[0] != "Starting" || got[1] != wantRunning || got[2] != "Error: "+firstErr.Error() {
		t.Fatalf("tray statuses after shutdown failure = %v, want Starting, %q, then observable shutdown error", got, wantRunning)
	}
	select {
	case <-fixture.application.quit:
		t.Fatal("Application.Quit ran after gateway shutdown failed")
	default:
	}
	select {
	case <-gateway.Done():
		t.Fatal("gateway reached Done before the blocked handler was released")
	default:
	}
	if snapshot := fixture.host.controller.Snapshot(); !snapshot.OwnsGateway || snapshot.ListenAddress != gateway.Address() {
		t.Fatalf("desktop snapshot while failed gateway is not Done = %+v, want retained ownership", snapshot)
	}

	const callers = 16
	results := make(chan error, callers)
	for range callers {
		go func() {
			results <- fixture.host.Shutdown(context.Background())
		}()
	}
	for range callers {
		if err := <-results; err != firstErr {
			t.Fatalf("concurrent Shutdown() error = %v, want stable %v", err, firstErr)
		}
	}

	releaseRequest()
	waitForGatewayDone(t, gateway)
	if err := fixture.host.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error after gateway Done = %v, want nil", err)
	}
	if snapshot := fixture.host.controller.Snapshot(); snapshot.OwnsGateway || snapshot.ListenAddress != "" {
		t.Fatalf("desktop snapshot after failed gateway reached Done = %+v, want released ownership", snapshot)
	}
	if got := fixture.tray.destroyCount(); got != 1 {
		t.Fatalf("Tray.Destroy calls after confirmed shutdown = %d, want 1", got)
	}
	select {
	case <-fixture.application.quit:
		t.Error("Application.Quit ran after failed gateway shutdown")
	default:
	}
}

func TestHostRequestQuitRetriesAfterGatewayShutdownFailure(t *testing.T) {
	fixture := newHostStartFixture(t)
	if err := fixture.host.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	options, gateway := fixture.startCapture.values()
	releaseRequest := startBlockingOpenDataRequest(t, fixture, gateway, options.Runtime.AdminTokenOverride)
	defer releaseRequest()

	shutdownDeadline := newManualDeadlineContext()
	shutdownContextCreated := make(chan struct{})
	var shutdownContextOnce sync.Once
	fixture.host.newShutdownContext = func() (context.Context, context.CancelFunc) {
		shutdownContextOnce.Do(func() { close(shutdownContextCreated) })
		return shutdownDeadline, func() {}
	}
	fixture.host.RequestQuit(context.Background())
	select {
	case <-shutdownContextCreated:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not start")
	}
	shutdownDeadline.expire()
	select {
	case <-fixture.host.shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("failed shutdown did not finish")
	}
	select {
	case <-fixture.application.quit:
		t.Fatal("Application.Quit ran after failed shutdown")
	default:
	}

	releaseRequest()
	waitForGatewayDone(t, gateway)
	fixture.host.RequestQuit(context.Background())
	fixture.application.waitForQuit(t)
	if got := fixture.tray.destroyCount(); got != 1 {
		t.Fatalf("Tray.Destroy calls after retry = %d, want 1", got)
	}
}

func TestHostLateStartupCleanupFailureIsPublishedAfterBarrierTimeout(t *testing.T) {
	fixture := newHostStartFixture(t)
	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	var releaseStartOnce sync.Once
	releaseStartup := func() { releaseStartOnce.Do(func() { close(releaseStart) }) }
	defer releaseStartup()
	fixture.host.startGateway = func(ctx context.Context, options gatewayapp.Options) (*gatewayapp.App, error) {
		gateway, err := fixture.startCapture.start(ctx, options)
		close(startEntered)
		<-releaseStart
		return gateway, err
	}

	startResult := make(chan error, 1)
	go func() {
		startResult <- fixture.host.Start(context.Background())
	}()
	select {
	case <-startEntered:
	case <-time.After(time.Second):
		t.Fatal("gateway did not start")
	}
	options, gateway := fixture.startCapture.values()
	releaseRequest := startBlockingOpenDataRequest(t, fixture, gateway, options.Runtime.AdminTokenOverride)
	defer releaseRequest()

	barrierErr := errors.New("startup barrier timeout")
	cleanupErr := errors.New("late startup cleanup failure")
	barrierContext := newManualErrorContext(barrierErr)
	cleanupContext := newManualErrorContext(cleanupErr)
	barrierContextCreated := make(chan struct{})
	cleanupContextCreated := make(chan struct{})
	var contextMu sync.Mutex
	contextCalls := 0
	fixture.host.newShutdownContext = func() (context.Context, context.CancelFunc) {
		contextMu.Lock()
		defer contextMu.Unlock()
		contextCalls++
		switch contextCalls {
		case 1:
			close(barrierContextCreated)
			return barrierContext, func() {}
		case 2:
			close(cleanupContextCreated)
			return cleanupContext, func() {}
		default:
			return context.WithTimeout(context.Background(), time.Second)
		}
	}

	fixture.host.RequestQuit(context.Background())
	select {
	case <-barrierContextCreated:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not start")
	}
	barrierContext.expire()
	select {
	case <-fixture.host.shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not publish its barrier timeout")
	}
	if !errors.Is(fixture.host.shutdownErr, barrierErr) {
		t.Fatalf("published shutdown error = %v, want %v", fixture.host.shutdownErr, barrierErr)
	}

	releaseStartup()
	select {
	case <-cleanupContextCreated:
	case <-time.After(time.Second):
		t.Fatal("late startup cleanup did not begin")
	}
	cleanupContext.expire()
	select {
	case err := <-startResult:
		if !errors.Is(err, cleanupErr) {
			t.Fatalf("Start() error = %v, want late cleanup error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after late cleanup failure")
	}
	if got := fixture.tray.statusValues(); len(got) != 3 || got[2] != "Error: "+cleanupErr.Error() {
		t.Fatalf("tray statuses after late cleanup failure = %v, want final error %q", got, "Error: "+cleanupErr.Error())
	}
	if !errors.Is(fixture.host.shutdownErr, barrierErr) {
		t.Fatalf("published shutdown error changed after late cleanup: %v", fixture.host.shutdownErr)
	}

	releaseRequest()
	waitForGatewayDone(t, gateway)
	fixture.host.RequestQuit(context.Background())
	fixture.application.waitForQuit(t)
}

func TestHostLateStartupCleanupFailureDuringShutdownPublicationIsNotLost(t *testing.T) {
	fixture := newHostStartFixture(t)
	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	var releaseStartOnce sync.Once
	releaseStartup := func() { releaseStartOnce.Do(func() { close(releaseStart) }) }
	defer releaseStartup()
	fixture.host.startGateway = func(ctx context.Context, options gatewayapp.Options) (*gatewayapp.App, error) {
		gateway, err := fixture.startCapture.start(ctx, options)
		close(startEntered)
		<-releaseStart
		return gateway, err
	}

	startResult := make(chan error, 1)
	go func() {
		startResult <- fixture.host.Start(context.Background())
	}()
	select {
	case <-startEntered:
	case <-time.After(time.Second):
		t.Fatal("gateway did not start")
	}
	options, gateway := fixture.startCapture.values()
	releaseRequest := startBlockingOpenDataRequest(t, fixture, gateway, options.Runtime.AdminTokenOverride)
	defer releaseRequest()

	barrierErr := errors.New("startup barrier timeout")
	cleanupErr := errors.New("late startup cleanup failure")
	barrierContext := newManualErrorContext(barrierErr)
	cleanupContext := newManualErrorContext(cleanupErr)
	barrierContextCreated := make(chan struct{})
	cleanupContextCreated := make(chan struct{})
	var contextMu sync.Mutex
	contextCalls := 0
	fixture.host.newShutdownContext = func() (context.Context, context.CancelFunc) {
		contextMu.Lock()
		defer contextMu.Unlock()
		contextCalls++
		switch contextCalls {
		case 1:
			close(barrierContextCreated)
			return barrierContext, func() {}
		case 2:
			close(cleanupContextCreated)
			return cleanupContext, func() {}
		default:
			return context.WithTimeout(context.Background(), time.Second)
		}
	}

	statusEntered := make(chan struct{})
	statusRelease := make(chan struct{})
	var releaseStatusOnce sync.Once
	releaseStatus := func() { releaseStatusOnce.Do(func() { close(statusRelease) }) }
	defer releaseStatus()
	fixture.host.tray = &blockingStatusTray{
		tray:    fixture.tray,
		status:  "Error: " + barrierErr.Error(),
		entered: statusEntered,
		release: statusRelease,
	}

	fixture.host.RequestQuit(context.Background())
	select {
	case <-barrierContextCreated:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not start")
	}
	barrierContext.expire()
	select {
	case <-statusEntered:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not reach barrier error publication")
	}

	fixture.host.lifecycleMu.RLock()
	attempt := fixture.host.shutdownAttempt
	fixture.host.lifecycleMu.RUnlock()
	if attempt == nil {
		t.Fatal("shutdown attempt was not registered")
	}

	releaseStartup()
	select {
	case <-cleanupContextCreated:
	case <-time.After(time.Second):
		t.Fatal("late startup cleanup did not begin")
	}
	cleanupContext.expire()
	select {
	case err := <-startResult:
		if !errors.Is(err, cleanupErr) {
			t.Fatalf("Start() error = %v, want late cleanup error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("late startup cleanup did not finish inside the publication window")
	}

	releaseStatus()
	select {
	case <-attempt.done:
	case <-time.After(time.Second):
		t.Fatal("shutdown attempt did not finish")
	}
	publishedErr := attempt.err
	if !errors.Is(publishedErr, barrierErr) || !errors.Is(publishedErr, cleanupErr) {
		t.Fatalf("published shutdown error = %v, want barrier and late cleanup errors", publishedErr)
	}
	if fixture.host.shutdownErr != publishedErr {
		t.Fatalf("host shutdown error = %v, want immutable attempt error %v", fixture.host.shutdownErr, publishedErr)
	}
	statuses := fixture.tray.statusValues()
	if len(statuses) < 3 || !strings.Contains(statuses[len(statuses)-1], cleanupErr.Error()) {
		t.Fatalf("tray statuses after publication-window cleanup failure = %v, want final late cleanup error", statuses)
	}

	releaseRequest()
	waitForGatewayDone(t, gateway)
	fixture.host.RequestQuit(context.Background())
	fixture.application.waitForQuit(t)
	if attempt.err != publishedErr {
		t.Fatalf("published attempt error changed after retry: before=%v after=%v", publishedErr, attempt.err)
	}
}

func TestHostControllerAndTrayOperationsUseAllowListedTargets(t *testing.T) {
	fixture := newHostStartFixture(t)
	if err := fixture.host.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	options, gateway := fixture.startCapture.values()

	fixture.host.OpenControlPlane()
	if err := fixture.host.CopyOpenAIBaseURL(); err != nil {
		t.Fatalf("CopyOpenAIBaseURL() error = %v", err)
	}
	if err := fixture.host.CopyAnthropicBaseURL(); err != nil {
		t.Fatalf("CopyAnthropicBaseURL() error = %v", err)
	}
	if err := fixture.host.OpenLogsFolder(); err != nil {
		t.Fatalf("OpenLogsFolder() error = %v", err)
	}
	if err := fixture.host.OpenControlPlaneInBrowser(); err != nil {
		t.Fatalf("OpenControlPlaneInBrowser() error = %v", err)
	}
	if err := fixture.host.controller.OpenDataDir(); err != nil {
		t.Fatalf("OpenDataDir() error = %v", err)
	}
	if shows, focuses := fixture.window.activationCounts(); shows != 1 || focuses != 1 {
		t.Fatalf("window activation = show:%d focus:%d, want 1 each", shows, focuses)
	}

	fixture.system.mu.Lock()
	defer fixture.system.mu.Unlock()
	dataBase := "http://" + gateway.Address()
	if got := fixture.system.browsed; len(got) != 1 {
		t.Fatalf("browser targets = %v, want exactly one", got)
	}
	assertBrowserBootstrapTarget(t, fixture.system.browsed[0], gateway.AdminAddress(), options.Runtime.DesktopSessions)
	if got := fixture.system.copied; len(got) != 2 || got[0] != dataBase+"/v1" || got[1] != dataBase+"/anthropic" {
		t.Fatalf("copied targets = %v", got)
	}
	if got := fixture.system.directories; len(got) != 2 || got[0] != fixture.paths.LogDir || got[1] != fixture.paths.DataDir {
		t.Fatalf("opened directories = %v, want [%q %q]", got, fixture.paths.LogDir, fixture.paths.DataDir)
	}
}

type manualDeadlineContext struct {
	done chan struct{}
	once sync.Once
}

type blockingStatusTray struct {
	tray    *fakeTray
	status  string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (t *blockingStatusTray) SetStatus(status string) {
	t.tray.SetStatus(status)
	if status == t.status {
		t.once.Do(func() { close(t.entered) })
		<-t.release
	}
}

func (t *blockingStatusTray) Destroy() {
	t.tray.Destroy()
}

type manualErrorContext struct {
	done chan struct{}
	err  error
	once sync.Once
}

func newManualErrorContext(err error) *manualErrorContext {
	return &manualErrorContext{done: make(chan struct{}), err: err}
}

func (c *manualErrorContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *manualErrorContext) Done() <-chan struct{}       { return c.done }
func (c *manualErrorContext) Err() error {
	select {
	case <-c.done:
		return c.err
	default:
		return nil
	}
}
func (c *manualErrorContext) Value(any) any { return nil }
func (c *manualErrorContext) expire()       { c.once.Do(func() { close(c.done) }) }

func newManualDeadlineContext() *manualDeadlineContext {
	return &manualDeadlineContext{done: make(chan struct{})}
}

func (c *manualDeadlineContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (c *manualDeadlineContext) Done() <-chan struct{} {
	return c.done
}

func (c *manualDeadlineContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (c *manualDeadlineContext) Value(any) any {
	return nil
}

func (c *manualDeadlineContext) expire() {
	c.once.Do(func() { close(c.done) })
}

func startBlockingOpenDataRequest(t *testing.T, fixture *hostStartFixture, gateway *gatewayapp.App, adminToken string) func() {
	t.Helper()
	entered := make(chan struct{})
	release := make(chan struct{})
	fixture.system.mu.Lock()
	fixture.system.openEntered = entered
	fixture.system.openRelease = release
	fixture.system.mu.Unlock()

	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		request, err := http.NewRequest(
			http.MethodPost,
			"http://"+gateway.AdminAddress()+"/admin/desktop/open-data-dir",
			nil,
		)
		if err != nil {
			return
		}
		request.Header.Set("Authorization", "Bearer "+adminToken)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("gateway request did not enter the blocking desktop operation")
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			close(release)
			select {
			case <-requestDone:
			case <-time.After(time.Second):
				t.Error("blocking gateway request did not return")
			}
		})
	}
}

func waitForGatewayDone(t *testing.T, gateway *gatewayapp.App) {
	t.Helper()
	select {
	case <-gateway.Done():
	case <-time.After(time.Second):
		t.Fatal("gateway did not reach Done after its blocked handler returned")
	}
}

func assertBrowserBootstrapTarget(t *testing.T, target, address string, sessions *auth.DesktopSessionStore) {
	t.Helper()
	if sessions == nil {
		t.Fatal("DesktopSessions = nil")
	}
	prefix := "http://" + address + "/desktop/bootstrap/"
	if !strings.HasPrefix(target, prefix) {
		t.Fatalf("browser target = %q, want prefix %q", target, prefix)
	}
	nonce := strings.TrimPrefix(target, prefix)
	if nonce == "" || strings.Contains(nonce, "/") {
		t.Fatalf("browser bootstrap nonce = %q, want one non-empty path segment", nonce)
	}
	if _, ok := sessions.ConsumeBootstrap(nonce); !ok {
		t.Fatal("browser bootstrap nonce was not registered in desktop session store")
	}
	if _, reused := sessions.ConsumeBootstrap(nonce); reused {
		t.Fatal("browser bootstrap nonce was reusable")
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
		AuthPath:        filepath.Join(dataDir, "auth.json"),
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
	mu            sync.Mutex
	options       gatewayapp.Options
	app           *gatewayapp.App
	listenAddress string
}

func (c *gatewayStartCapture) start(ctx context.Context, options gatewayapp.Options) (*gatewayapp.App, error) {
	c.mu.Lock()
	c.options = cloneGatewayOptions(options)
	c.mu.Unlock()
	dataListenAddress := c.listenAddress
	if dataListenAddress == "" {
		dataListenAddress = "127.0.0.1:0"
	}
	listenCall := 0
	options.Listen = func(_, _ string) (net.Listener, error) {
		listenCall++
		if listenCall == 1 {
			return net.Listen("tcp", dataListenAddress)
		}
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
			PasswordAuth:       options.Runtime.PasswordAuth,
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
