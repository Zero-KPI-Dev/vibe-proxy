package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/a448582655/vibe-proxy/internal/auth"
	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/gatewayapp"
	runtimeoptions "github.com/a448582655/vibe-proxy/internal/runtime"
	"gopkg.in/yaml.v3"
)

var errHostShuttingDown = errors.New("desktop host is shutting down")

type HostOptions struct {
	Paths        Paths
	Preferences  Preferences
	Window       Window
	Dialogs      Dialogs
	Tray         Tray
	System       System
	Application  Application
	StartGateway func(context.Context, gatewayapp.Options) (*gatewayapp.App, error)
}

type Host struct {
	paths              Paths
	window             Window
	dialogs            Dialogs
	tray               Tray
	system             System
	application        Application
	startGateway       func(context.Context, gatewayapp.Options) (*gatewayapp.App, error)
	newShutdownContext func() (context.Context, context.CancelFunc)

	closeMu     sync.Mutex
	preferences Preferences
	dialogOpen  bool
	quitting    bool

	lifecycleMu sync.RWMutex
	gateway     *gatewayapp.App
	sessions    *auth.DesktopSessionStore
	ownsGateway bool
	controller  *desktopController
	startupDone chan struct{}

	shutdownRequested bool
	startupCleanupErr error

	shutdownOnce sync.Once
	shutdownDone chan struct{}
	shutdownErr  error
	quitOnce     sync.Once
}

func NewHost(options HostOptions) (*Host, error) {
	switch {
	case options.Window == nil:
		return nil, fmt.Errorf("window is required")
	case options.Dialogs == nil:
		return nil, fmt.Errorf("dialogs are required")
	case options.Tray == nil:
		return nil, fmt.Errorf("tray is required")
	case options.System == nil:
		return nil, fmt.Errorf("system is required")
	case options.Application == nil:
		return nil, fmt.Errorf("application is required")
	case options.Paths.PreferencesPath == "":
		return nil, fmt.Errorf("preferences path is required")
	}
	if options.StartGateway == nil {
		options.StartGateway = gatewayapp.Start
	}
	host := &Host{
		paths:        options.Paths,
		window:       options.Window,
		dialogs:      options.Dialogs,
		tray:         options.Tray,
		system:       options.System,
		application:  options.Application,
		startGateway: options.StartGateway,
		newShutdownContext: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), shutdownTimeout)
		},
		preferences:  normalizeLoadedPreferences(options.Preferences),
		shutdownDone: make(chan struct{}),
	}
	host.controller = &desktopController{host: host}
	return host, nil
}

func (h *Host) Start(ctx context.Context) error {
	startupDone, err := h.beginStartup()
	if err != nil {
		return err
	}
	defer h.finishStartup(startupDone)

	if !h.setStatusUnlessShuttingDown("Starting") {
		return errHostShuttingDown
	}
	if err := ensureFirstRunConfig(h.paths); err != nil {
		h.setStartupErrorStatus(err)
		return err
	}

	adminToken, err := randomHostToken()
	if err != nil {
		h.setStartupErrorStatus(err)
		return fmt.Errorf("generate desktop admin token: %w", err)
	}
	sessions := auth.NewDesktopSessionStore(time.Now, time.Minute)
	options := gatewayapp.Options{
		ConfigPath: h.paths.ConfigPath,
		Runtime: runtimeoptions.Options{
			AdminTokenOverride: adminToken,
			DesktopSessions:    sessions,
			DesktopController:  h.controller,
		},
	}
	gateway, err := h.startGateway(ctx, options)
	options.Runtime.AdminTokenOverride = ""
	adminToken = ""
	if err != nil {
		h.setStartupErrorStatus(err)
		return err
	}

	h.lifecycleMu.Lock()
	shutdownRequested := h.shutdownRequested
	if !shutdownRequested {
		h.gateway = gateway
		h.sessions = sessions
		h.ownsGateway = true
	}
	h.lifecycleMu.Unlock()
	if shutdownRequested {
		return h.cleanupStartupGateway(errHostShuttingDown, gateway, sessions)
	}

	select {
	case <-gateway.Ready():
	case <-ctx.Done():
		h.setStartupErrorStatus(ctx.Err())
		return h.cleanupStartupGateway(ctx.Err(), gateway, sessions)
	}
	if h.isShutdownRequested() {
		return h.cleanupStartupGateway(errHostShuttingDown, gateway, sessions)
	}

	nonce, err := sessions.NewBootstrapNonce()
	if err != nil {
		err = fmt.Errorf("generate desktop bootstrap nonce: %w", err)
		h.setStartupErrorStatus(err)
		return h.cleanupStartupGateway(err, gateway, sessions)
	}
	if h.isShutdownRequested() {
		return h.cleanupStartupGateway(errHostShuttingDown, gateway, sessions)
	}
	target := "http://" + gateway.Address() + "/desktop/bootstrap/" + nonce
	if err := h.window.Navigate(target); err != nil {
		if !h.isShutdownRequested() {
			h.setStartupErrorStatus(err)
			return h.cleanupStartupGateway(err, gateway, sessions)
		}
		return h.cleanupStartupGateway(errors.Join(errHostShuttingDown, err), gateway, sessions)
	}
	nonce = ""
	if !h.setStatusUnlessShuttingDown("Running on " + gateway.Address()) {
		return h.cleanupStartupGateway(errHostShuttingDown, gateway, sessions)
	}
	return nil
}

func (h *Host) beginStartup() (chan struct{}, error) {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.shutdownRequested {
		return nil, errHostShuttingDown
	}
	startupDone := make(chan struct{})
	h.startupDone = startupDone
	return startupDone, nil
}

func (h *Host) finishStartup(startupDone chan struct{}) {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.startupDone == startupDone {
		h.startupDone = nil
		close(startupDone)
	}
}

func (h *Host) isShutdownRequested() bool {
	h.lifecycleMu.RLock()
	defer h.lifecycleMu.RUnlock()
	return h.shutdownRequested
}

func (h *Host) cleanupStartupGateway(startErr error, gateway *gatewayapp.App, sessions *auth.DesktopSessionStore) error {
	cleanupCtx, cancel := h.newShutdownContext()
	cleanupErr := gateway.Shutdown(cleanupCtx)
	cancel()
	sessions.RevokeAll()

	h.lifecycleMu.Lock()
	if cleanupErr == nil {
		h.clearGatewayLocked(gateway)
	} else {
		if h.gateway == nil {
			h.gateway = gateway
			h.sessions = sessions
			h.ownsGateway = true
		}
		if h.shutdownRequested {
			h.startupCleanupErr = cleanupErr
		}
	}
	h.lifecycleMu.Unlock()
	if cleanupErr != nil {
		return errors.Join(startErr, cleanupErr)
	}
	return startErr
}

func (h *Host) clearGatewayLocked(gateway *gatewayapp.App) {
	if h.gateway == gateway {
		h.gateway = nil
		h.sessions = nil
		h.ownsGateway = false
	}
}

func (h *Host) reconcileGatewayDone() {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.gateway == nil {
		return
	}
	select {
	case <-h.gateway.Done():
		h.clearGatewayLocked(h.gateway)
	default:
	}
}

func (h *Host) OpenControlPlane() {
	h.window.Show()
	h.window.Focus()
}

func (h *Host) CopyOpenAIBaseURL() error {
	base, err := h.gatewayBaseURL()
	if err != nil {
		return err
	}
	return h.system.CopyText(base + "/v1")
}

func (h *Host) CopyAnthropicBaseURL() error {
	base, err := h.gatewayBaseURL()
	if err != nil {
		return err
	}
	return h.system.CopyText(base + "/anthropic")
}

func (h *Host) gatewayBaseURL() (string, error) {
	h.lifecycleMu.RLock()
	defer h.lifecycleMu.RUnlock()
	if h.gateway == nil {
		return "", errors.New("gateway is not running")
	}
	return "http://" + h.gateway.Address(), nil
}

func (h *Host) setStartupErrorStatus(err error) {
	h.setStatusUnlessShuttingDown("Error: " + err.Error())
}

func (h *Host) setStatusUnlessShuttingDown(status string) bool {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.shutdownRequested {
		return false
	}
	h.tray.SetStatus(status)
	return true
}

func ensureFirstRunConfig(paths Paths) error {
	if _, err := os.Stat(paths.ConfigPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect desktop config: %w", err)
	}
	if paths.ConfigPath == "" || paths.DatabasePath == "" {
		return errors.New("desktop config and database paths are required")
	}

	bootstrap := config.SimpleConfig{
		Version:    "vibeproxy.io/v1alpha1",
		Server:     config.ServerConfig{Listen: "127.0.0.1:8080"},
		Storage:    config.StorageConfig{SQLitePath: paths.DatabasePath, RetentionDays: 14},
		ClientKeys: []config.ClientKeyConfig{},
		Providers:  map[string]config.ProviderConfig{},
		Models: config.ModelsConfig{
			Default:  "",
			AllowRaw: true,
			Aliases:  map[string]string{},
		},
		AgentProfiles: map[string]config.AgentProfileConfig{},
		Routes:        []config.AdvancedRouteConfig{},
	}
	data, err := yaml.Marshal(bootstrap)
	if err != nil {
		return fmt.Errorf("marshal first-run config: %w", err)
	}
	return writePrivateFile(paths.ConfigPath, data)
}

func writePrivateFile(path string, data []byte) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create desktop data directory: %w", err)
	}
	temporary, err := os.CreateTemp(parent, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary desktop config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("set temporary desktop config permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary desktop config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary desktop config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary desktop config: %w", err)
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("install desktop config: %w", err)
	}
	return nil
}

func randomHostToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
