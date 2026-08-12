package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/auth"
	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/desktopbridge"
	"github.com/a448582655/vibe-proxy/internal/metrics"
)

func TestDesktopBootstrapSetsSessionCookieAndAuthorizesAdminRequests(t *testing.T) {
	server, sessions := newDesktopTestServer(t)
	nonce, err := sessions.NewBootstrapNonce()
	if err != nil {
		t.Fatal(err)
	}

	bootstrap := httptest.NewRequest(http.MethodGet, "/desktop/bootstrap/"+nonce, nil)
	bootstrapResponse := httptest.NewRecorder()
	server.Routes().ServeHTTP(bootstrapResponse, bootstrap)
	if bootstrapResponse.Code != http.StatusSeeOther {
		t.Fatalf("bootstrap status = %d, want %d: %s", bootstrapResponse.Code, http.StatusSeeOther, bootstrapResponse.Body.String())
	}
	if location := bootstrapResponse.Header().Get("Location"); location != "/" {
		t.Fatalf("bootstrap Location = %q, want /", location)
	}

	var sessionCookie *http.Cookie
	for _, cookie := range bootstrapResponse.Result().Cookies() {
		if cookie.Name == auth.DesktopSessionCookie {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("bootstrap response did not set %s", auth.DesktopSessionCookie)
	}
	if !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode || sessionCookie.Path != "/" {
		t.Fatalf("desktop session cookie flags are unsafe: %+v", sessionCookie)
	}
	if sessionCookie.MaxAge != 0 || !sessionCookie.Expires.IsZero() {
		t.Fatalf("desktop session cookie must be process/session scoped: %+v", sessionCookie)
	}

	snapshot := httptest.NewRequest(http.MethodGet, "/admin/config/snapshot", nil)
	snapshot.AddCookie(sessionCookie)
	snapshotResponse := httptest.NewRecorder()
	server.Routes().ServeHTTP(snapshotResponse, snapshot)
	if snapshotResponse.Code != http.StatusOK {
		t.Fatalf("cookie-authenticated snapshot status = %d, want 200: %s", snapshotResponse.Code, snapshotResponse.Body.String())
	}
}

func TestAdminCookieRequiresSameOriginForStateChangingRequests(t *testing.T) {
	server, sessions := newDesktopTestServer(t)
	session := consumeDesktopSession(t, sessions)

	tests := []struct {
		name             string
		origin           string
		additionalOrigin string
		want             int
	}{
		{name: "missing origin", want: http.StatusForbidden},
		{name: "opaque origin", origin: "null", want: http.StatusForbidden},
		{name: "malformed origin", origin: "http://%", want: http.StatusForbidden},
		{name: "user info", origin: "http://user@example.com", want: http.StatusForbidden},
		{name: "unsupported scheme", origin: "file://example.com", want: http.StatusForbidden},
		{name: "different host", origin: "http://other.example", want: http.StatusForbidden},
		{name: "multiple origins", origin: "http://example.com", additionalOrigin: "http://other.example", want: http.StatusForbidden},
		{name: "same origin", origin: "http://example.com", want: http.StatusOK},
		{name: "same origin case insensitive", origin: "HTTPS://EXAMPLE.COM", want: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/admin/config/reload", nil)
			req.AddCookie(&http.Cookie{Name: auth.DesktopSessionCookie, Value: session})
			if test.origin != "" {
				req.Header.Set("Origin", test.origin)
			}
			if test.additionalOrigin != "" {
				req.Header.Add("Origin", test.additionalOrigin)
			}
			response := httptest.NewRecorder()
			server.Routes().ServeHTTP(response, req)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestAdminCookieDoesNotRestrictBearerStateChangingRequests(t *testing.T) {
	server, _ := newDesktopTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/admin/config/reload", nil)
	req.Header.Set("Authorization", "Bearer override-admin-token")
	response := httptest.NewRecorder()

	server.Routes().ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("Bearer-authenticated reload without Origin status = %d, want 200: %s", response.Code, response.Body.String())
	}
}

func TestDesktopBootstrapRouteIsNotExposedInCLIMode(t *testing.T) {
	path := writeAdminTestConfig(t)
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	server := NewWithOptions(path, cfg, nil, testProm, Options{})
	for _, target := range []string{"/desktop/bootstrap", "/desktop/bootstrap/not-a-nonce"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, req)
		if response.Code != http.StatusNotFound {
			t.Fatalf("CLI bootstrap status for %s = %d, want 404: %s", target, response.Code, response.Body.String())
		}
	}
}

func newDesktopTestServer(t *testing.T) (*Server, *auth.DesktopSessionStore) {
	t.Helper()
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "environment-admin-token")
	path := writeAdminTestConfig(t)
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewDesktopSessionStore(time.Now, time.Minute)
	testPromOnce.Do(func() { testProm = metrics.New() })
	server := NewWithOptions(path, cfg, nil, testProm, Options{
		AdminTokenOverride: "override-admin-token",
		DesktopSessions:    sessions,
	})
	return server, sessions
}

func consumeDesktopSession(t *testing.T, sessions *auth.DesktopSessionStore) string {
	t.Helper()
	nonce, err := sessions.NewBootstrapNonce()
	if err != nil {
		t.Fatal(err)
	}
	session, ok := sessions.ConsumeBootstrap(nonce)
	if !ok {
		t.Fatal("fresh bootstrap nonce was not consumed")
	}
	return session
}

type fakeDesktopController struct {
	snapshot desktopbridge.Snapshot

	setCloseBehaviorErr error
	copyTextErr         error
	openDataDirErr      error
	importConfigErr     error
	importConfig        func(context.Context) (desktopbridge.ImportResult, error)

	setCloseBehaviors []desktopbridge.CloseBehavior
	copiedTexts       []string
	openDataDirCalls  int
	importConfigCalls int
}

func (f *fakeDesktopController) Snapshot() desktopbridge.Snapshot { return f.snapshot }

func (f *fakeDesktopController) SetCloseBehavior(behavior desktopbridge.CloseBehavior) error {
	f.setCloseBehaviors = append(f.setCloseBehaviors, behavior)
	return f.setCloseBehaviorErr
}

func (f *fakeDesktopController) CopyText(value string) error {
	f.copiedTexts = append(f.copiedTexts, value)
	return f.copyTextErr
}

func (f *fakeDesktopController) OpenDataDir() error {
	f.openDataDirCalls++
	return f.openDataDirErr
}

func (f *fakeDesktopController) ImportConfig(ctx context.Context) (desktopbridge.ImportResult, error) {
	f.importConfigCalls++
	if f.importConfig != nil {
		return f.importConfig(ctx)
	}
	return desktopbridge.ImportResult{Imported: true, Path: "/safe/imported.yaml"}, f.importConfigErr
}

func TestDesktopControllerCLISnapshotIsUnavailableAndNativeActionsConflict(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	path := writeAdminTestConfig(t)
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	server := New(path, cfg, nil, testProm)

	for _, test := range []struct {
		method string
		target string
		body   string
		want   int
	}{
		{method: http.MethodGet, target: "/admin/desktop", want: http.StatusOK},
		{method: http.MethodPut, target: "/admin/desktop/preferences", body: `{"close_behavior":"tray"}`, want: http.StatusConflict},
		{method: http.MethodPost, target: "/admin/desktop/clipboard", body: `{"text":"hello"}`, want: http.StatusConflict},
		{method: http.MethodPost, target: "/admin/desktop/open-data-dir", want: http.StatusConflict},
		{method: http.MethodPost, target: "/admin/desktop/import-config", want: http.StatusConflict},
	} {
		t.Run(test.target, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.Routes().ServeHTTP(response, adminJSONRequest(test.method, test.target, test.body))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.want, response.Body.String())
			}
			if test.target == "/admin/desktop" && response.Body.String() != "{\"available\":false}\n" {
				t.Fatalf("CLI snapshot = %s, want {\\\"available\\\":false}", response.Body.String())
			}
		})
	}
}

func TestDesktopControllerRoutesAreAuthenticatedAndMethodExact(t *testing.T) {
	controller := &fakeDesktopController{snapshot: desktopbridge.Snapshot{Available: true}}
	server := newDesktopControllerTestServer(t, controller)

	for _, test := range []struct {
		name   string
		method string
		target string
		want   int
	}{
		{name: "snapshot auth", method: http.MethodGet, target: "/admin/desktop", want: http.StatusUnauthorized},
		{name: "preferences auth", method: http.MethodPut, target: "/admin/desktop/preferences", want: http.StatusUnauthorized},
		{name: "clipboard auth", method: http.MethodPost, target: "/admin/desktop/clipboard", want: http.StatusUnauthorized},
		{name: "open auth", method: http.MethodPost, target: "/admin/desktop/open-data-dir", want: http.StatusUnauthorized},
		{name: "import auth", method: http.MethodPost, target: "/admin/desktop/import-config", want: http.StatusUnauthorized},
		{name: "snapshot method", method: http.MethodPost, target: "/admin/desktop", want: http.StatusMethodNotAllowed},
		{name: "preferences method", method: http.MethodPost, target: "/admin/desktop/preferences", want: http.StatusMethodNotAllowed},
		{name: "clipboard method", method: http.MethodGet, target: "/admin/desktop/clipboard", want: http.StatusMethodNotAllowed},
		{name: "open method", method: http.MethodGet, target: "/admin/desktop/open-data-dir", want: http.StatusMethodNotAllowed},
		{name: "import method", method: http.MethodGet, target: "/admin/desktop/import-config", want: http.StatusMethodNotAllowed},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.target, nil)
			if test.want == http.StatusMethodNotAllowed {
				req.Header.Set("Authorization", "Bearer admin-token")
			}
			response := httptest.NewRecorder()
			server.Routes().ServeHTTP(response, req)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.want, response.Body.String())
			}
			if !json.Valid(response.Body.Bytes()) {
				t.Fatalf("response is not JSON: %s", response.Body.String())
			}
		})
	}
}

func TestDesktopControllerSnapshotReturnsFakeSnapshot(t *testing.T) {
	want := desktopbridge.Snapshot{
		Available:     true,
		Platform:      "darwin",
		CloseBehavior: desktopbridge.CloseTray,
		ListenAddress: "127.0.0.1:8080",
		DataDir:       "/safe/data",
		LogDir:        "/safe/logs",
		OwnsGateway:   true,
	}
	server := newDesktopControllerTestServer(t, &fakeDesktopController{snapshot: want})

	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, adminJSONRequest(http.MethodGet, "/admin/desktop", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var got desktopbridge.Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
}

func TestDesktopControllerOpenDataDirInvokesControllerOnce(t *testing.T) {
	controller := &fakeDesktopController{snapshot: desktopbridge.Snapshot{Available: true}}
	server := newDesktopControllerTestServer(t, controller)

	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, adminJSONRequest(http.MethodPost, "/admin/desktop/open-data-dir", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("open status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if controller.openDataDirCalls != 1 {
		t.Fatalf("open calls = %d, want 1", controller.openDataDirCalls)
	}
}

func TestDesktopClipboardCopiesWithoutEchoingValue(t *testing.T) {
	controller := &fakeDesktopController{snapshot: desktopbridge.Snapshot{Available: true}}
	server := newDesktopControllerTestServer(t, controller)
	const value = "sk-local-secret-value"

	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, adminJSONRequest(http.MethodPost, "/admin/desktop/clipboard", `{"text":"`+value+`"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("clipboard status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if len(controller.copiedTexts) != 1 || controller.copiedTexts[0] != value {
		t.Fatalf("copied texts = %q, want one exact value", controller.copiedTexts)
	}
	if strings.Contains(response.Body.String(), value) || response.Body.String() != "{\"copied\":true}\n" {
		t.Fatalf("clipboard response leaked value or changed contract: %s", response.Body.String())
	}
}

func TestDesktopClipboardRejectsInvalidInput(t *testing.T) {
	controller := &fakeDesktopController{snapshot: desktopbridge.Snapshot{Available: true}}
	server := newDesktopControllerTestServer(t, controller)

	for _, body := range []string{`{}`, `{"text":""}`, `{"text":"ok","extra":true}`, `{"text":"ok"}{"text":"extra"}`} {
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, adminJSONRequest(http.MethodPost, "/admin/desktop/clipboard", body))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400: %s", body, response.Code, response.Body.String())
		}
	}
	if len(controller.copiedTexts) != 0 {
		t.Fatalf("invalid input reached clipboard controller: %q", controller.copiedTexts)
	}
}

func TestDesktopPreferencesAllowsOnlyKnownCloseBehaviors(t *testing.T) {
	controller := &fakeDesktopController{snapshot: desktopbridge.Snapshot{Available: true}}
	server := newDesktopControllerTestServer(t, controller)

	invalid := httptest.NewRecorder()
	server.Routes().ServeHTTP(invalid, adminJSONRequest(http.MethodPut, "/admin/desktop/preferences", `{"close_behavior":"delete-files"}`))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid behavior status = %d, want 400: %s", invalid.Code, invalid.Body.String())
	}
	if len(controller.setCloseBehaviors) != 0 {
		t.Fatalf("invalid behavior invoked controller: %v", controller.setCloseBehaviors)
	}

	for _, behavior := range []desktopbridge.CloseBehavior{desktopbridge.CloseAsk, desktopbridge.CloseTray, desktopbridge.CloseQuit} {
		valid := httptest.NewRecorder()
		server.Routes().ServeHTTP(valid, adminJSONRequest(http.MethodPut, "/admin/desktop/preferences", `{"close_behavior":"`+string(behavior)+`"}`))
		if valid.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200: %s", behavior, valid.Code, valid.Body.String())
		}
	}
	if got := controller.setCloseBehaviors; len(got) != 3 || got[0] != desktopbridge.CloseAsk || got[1] != desktopbridge.CloseTray || got[2] != desktopbridge.CloseQuit {
		t.Fatalf("persisted close behaviors = %v, want [ask tray quit]", got)
	}
}

func TestDesktopImportReloadsValidatedRuntimeAndReturnsLoadedAt(t *testing.T) {
	path := writeAdminTestConfig(t)
	controller := &fakeDesktopController{snapshot: desktopbridge.Snapshot{Available: true}}
	controller.importConfig = func(context.Context) (desktopbridge.ImportResult, error) {
		content, err := os.ReadFile(path)
		if err != nil {
			return desktopbridge.ImportResult{}, err
		}
		content = []byte(strings.Replace(string(content), "https://mock.openai/v1", "https://imported.openai/v1", 1))
		return desktopbridge.ImportResult{Imported: true, Path: "/safe/imported.yaml"}, os.WriteFile(path, content, 0600)
	}
	server := newDesktopControllerTestServerAtPath(t, path, controller)
	before := server.current().LoadedAt

	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, adminJSONRequest(http.MethodPost, "/admin/desktop/import-config", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("import status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if controller.importConfigCalls != 1 {
		t.Fatalf("import calls = %d, want 1", controller.importConfigCalls)
	}
	var payload struct {
		Imported bool      `json:"imported"`
		Path     string    `json:"path"`
		LoadedAt time.Time `json:"loaded_at"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	// Windows can expose a coarser wall-clock resolution than the time between
	// the initial snapshot and this immediate reload. A reload is still valid
	// when both snapshots serialize to the same timestamp; the changed provider
	// configuration below is the authoritative assertion that it happened.
	if !payload.Imported || payload.Path != "/safe/imported.yaml" || payload.LoadedAt.Before(before) {
		t.Fatalf("unexpected import response: %+v (before %s)", payload, before)
	}
	if got := server.current().Config.Providers["mockai"].BaseURL; got != "https://imported.openai/v1" {
		t.Fatalf("import did not reload validated config, base URL = %q", got)
	}
}

func TestDesktopControllerErrorsReturnGenericJSON(t *testing.T) {
	secret := "/Users/twn/private/import-config.yaml: credentials"
	controller := &fakeDesktopController{
		snapshot:            desktopbridge.Snapshot{Available: true},
		setCloseBehaviorErr: errors.New(secret),
		copyTextErr:         errors.New(secret),
		openDataDirErr:      errors.New(secret),
		importConfigErr:     errors.New(secret),
	}
	server := newDesktopControllerTestServer(t, controller)

	for _, test := range []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "preferences", method: http.MethodPut, target: "/admin/desktop/preferences", body: `{"close_behavior":"ask"}`},
		{name: "clipboard", method: http.MethodPost, target: "/admin/desktop/clipboard", body: `{"text":"secret"}`},
		{name: "open", method: http.MethodPost, target: "/admin/desktop/open-data-dir"},
		{name: "import", method: http.MethodPost, target: "/admin/desktop/import-config"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.Routes().ServeHTTP(response, adminJSONRequest(test.method, test.target, test.body))
			if response.Code != http.StatusInternalServerError || !json.Valid(response.Body.Bytes()) {
				t.Fatalf("error status/body = %d %s", response.Code, response.Body.String())
			}
			if response.Body.String() == "" || strings.Contains(response.Body.String(), secret) {
				t.Fatalf("controller error leaked: %s", response.Body.String())
			}
		})
	}
}

func newDesktopControllerTestServer(t *testing.T, controller desktopbridge.Controller) *Server {
	t.Helper()
	return newDesktopControllerTestServerAtPath(t, writeAdminTestConfig(t), controller)
}

func newDesktopControllerTestServerAtPath(t *testing.T, path string, controller desktopbridge.Controller) *Server {
	t.Helper()
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "environment-admin-token")
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	return NewWithOptions(path, cfg, nil, testProm, Options{
		AdminTokenOverride: "admin-token",
		DesktopController:  controller,
	})
}
