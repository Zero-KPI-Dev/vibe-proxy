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
	paths        Paths
	window       Window
	dialogs      Dialogs
	tray         Tray
	system       System
	application  Application
	startGateway func(context.Context, gatewayapp.Options) (*gatewayapp.App, error)

	closeMu     sync.Mutex
	preferences Preferences
	dialogOpen  bool
	quitting    bool

	lifecycleMu sync.RWMutex
	gateway     *gatewayapp.App
	sessions    *auth.DesktopSessionStore
	ownsGateway bool
	controller  *desktopController

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
		preferences:  normalizeLoadedPreferences(options.Preferences),
		shutdownDone: make(chan struct{}),
	}
	host.controller = &desktopController{host: host}
	return host, nil
}

func (h *Host) Start(ctx context.Context) error {
	h.tray.SetStatus("Starting")
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
	h.gateway = gateway
	h.sessions = sessions
	h.ownsGateway = true
	h.lifecycleMu.Unlock()

	select {
	case <-gateway.Ready():
	case <-ctx.Done():
		h.setStartupErrorStatus(ctx.Err())
		_ = h.Shutdown(context.Background())
		return ctx.Err()
	}

	nonce, err := sessions.NewBootstrapNonce()
	if err != nil {
		err = fmt.Errorf("generate desktop bootstrap nonce: %w", err)
		h.setStartupErrorStatus(err)
		_ = h.Shutdown(context.Background())
		return err
	}
	target := "http://" + gateway.Address() + "/desktop/bootstrap/" + nonce
	if err := h.window.Navigate(target); err != nil {
		h.setStartupErrorStatus(err)
		_ = h.Shutdown(context.Background())
		return err
	}
	nonce = ""
	h.tray.SetStatus("Running on " + gateway.Address())
	return nil
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
	h.tray.SetStatus("Error: " + err.Error())
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
