package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/gatewayapp"
)

type RecoveryKind string

const (
	RecoveryInvalidConfig RecoveryKind = "invalid_config"
	RecoveryPortConflict  RecoveryKind = "port_conflict"
	RecoveryDatabase      RecoveryKind = "database"
	RecoveryWebView       RecoveryKind = "webview"
)

type RecoveryChoice string

const (
	RecoveryOpenExisting RecoveryChoice = "open_existing"
	RecoveryOpenData     RecoveryChoice = "open_data"
	RecoveryExit         RecoveryChoice = "exit"
)

type StartupPresentation struct {
	Kind    RecoveryKind
	Title   string
	Summary string
	Address string
}

type webViewStartupError struct {
	address string
	err     error
}

func (e *webViewStartupError) Error() string { return fmt.Sprintf("desktop webview: %v", e.err) }
func (e *webViewStartupError) Unwrap() error { return e.err }

type healthProbe func(context.Context, string) bool

// RecoverStartup presents a safe native recovery path. It never exposes the
// wrapped startup error in the dialog; full diagnostics remain in the local log.
func (h *Host) RecoverStartup(ctx context.Context, startupErr error) error {
	presentation := classifyStartupFailure(ctx, startupErr, probeVibeProxy)
	choice, err := h.dialogs.ShowStartupError(ctx, presentation)
	if err != nil {
		return err
	}

	switch choice {
	case RecoveryOpenExisting:
		if presentation.Kind == RecoveryWebView {
			return h.OpenControlPlaneInBrowser()
		}
		if presentation.Address == "" {
			return errors.New("existing control plane is unavailable")
		}
		return h.system.OpenBrowser("http://" + presentation.Address)
	case RecoveryOpenData:
		if presentation.Kind == RecoveryWebView {
			return h.system.OpenDirectory(h.paths.LogDir)
		}
		return h.system.OpenDirectory(h.paths.DataDir)
	case RecoveryExit:
		h.RequestQuit(ctx)
		return nil
	default:
		return nil
	}
}

func classifyStartupFailure(ctx context.Context, startupErr error, probe healthProbe) StartupPresentation {
	var webViewErr *webViewStartupError
	if errors.As(startupErr, &webViewErr) {
		return StartupPresentation{
			Kind:    RecoveryWebView,
			Title:   "Desktop window unavailable",
			Summary: "The proxy is still running. Open the control plane in your browser or inspect the logs.",
			Address: webViewErr.address,
		}
	}

	var gatewayErr *gatewayapp.StartupError
	if !errors.As(startupErr, &gatewayErr) {
		return StartupPresentation{
			Kind:    RecoveryInvalidConfig,
			Title:   "vibe-proxy could not start",
			Summary: "Open the application data folder to inspect the configuration and logs, then try again.",
		}
	}

	switch gatewayErr.Stage {
	case "database":
		return StartupPresentation{
			Kind:    RecoveryDatabase,
			Title:   "Database could not be opened",
			Summary: "Open the application data folder and inspect the log before trying again.",
		}
	case "listen":
		presentation := StartupPresentation{
			Kind:    RecoveryPortConflict,
			Title:   "The proxy address is already in use",
			Summary: "Another service is using the configured address. Open the data folder to change it or exit.",
		}
		if gatewayErr.Address != "" && probe(ctx, gatewayErr.Address) {
			presentation.Address = browserAddress(gatewayErr.Address)
			presentation.Summary = "Another vibe-proxy is already running. You can open its control plane or change this app's configuration."
		}
		return presentation
	default:
		return StartupPresentation{
			Kind:    RecoveryInvalidConfig,
			Title:   "Configuration could not be loaded",
			Summary: "Open the application data folder, correct config.yaml, and try again.",
		}
	}
}

func probeVibeProxy(ctx context.Context, address string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, "http://"+browserAddress(address)+"/healthz", nil)
	if err != nil {
		return false
	}
	response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return false
	}
	return jsonBool(body["ok"]) && len(body["started_at"]) > 0 && len(body["loaded_at"]) > 0
}

func jsonBool(raw json.RawMessage) bool {
	var value bool
	return json.Unmarshal(raw, &value) == nil && value
}

func browserAddress(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return strings.TrimPrefix(strings.TrimPrefix(address, "http://"), "https://")
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func (h *Host) writeStartupErrorLog(startupErr error) {
	stage, address := "desktop", ""
	var gatewayErr *gatewayapp.StartupError
	if errors.As(startupErr, &gatewayErr) {
		stage, address = gatewayErr.Stage, gatewayErr.Address
	}
	var webViewErr *webViewStartupError
	if errors.As(startupErr, &webViewErr) {
		stage, address = "webview", webViewErr.address
	}
	WriteStartupErrorLog(h.paths, stage, address, startupErr)
}

// WriteStartupErrorLog records a startup failure before the desktop shell has
// necessarily finished initialising. It is also used by the top-level native
// launcher, because an error returned by Wails itself happens before Host.Start
// can write its usual recovery log entry.
func WriteStartupErrorLog(paths Paths, stage, address string, startupErr error) {
	if startupErr == nil || paths.LogPath == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(paths.LogPath), 0o700)
	file, err := os.OpenFile(paths.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	// Keep one physical line per event so untrusted wrapped errors cannot forge
	// additional structured log entries.
	message := strings.NewReplacer("\r", " ", "\n", " ").Replace(startupErr.Error())
	_, _ = fmt.Fprintf(file, "%s stage=%q address=%q error=%q\n",
		time.Now().UTC().Format(time.RFC3339Nano), stage, address, message)
}
