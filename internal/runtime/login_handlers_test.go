package runtime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/auth"
	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/metrics"
)

const testManagementPassword = "local management password"

func TestDesktopManagementPasswordSetupLoginAndLogout(t *testing.T) {
	server, sessions, passwordPath := newPasswordAuthTestServer(t)
	nativeSession, err := sessions.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	nativeCookie := &http.Cookie{Name: auth.DesktopSessionCookie, Value: nativeSession}

	status := serveAuthRequest(t, server, http.MethodGet, "/auth/status", "", nil, "")
	assertAuthStatus(t, status, http.StatusOK, authStatusResponse{
		Mode:          "desktop",
		Initialized:   false,
		Authenticated: false,
	})

	missingOrigin := serveAuthRequest(
		t,
		server,
		http.MethodPost,
		"/auth/setup",
		`{"password":"`+testManagementPassword+`"}`,
		nativeCookie,
		"",
	)
	if missingOrigin.Code != http.StatusForbidden {
		t.Fatalf("setup without Origin status = %d, want 403: %s", missingOrigin.Code, missingOrigin.Body.String())
	}

	noNativeSession := serveAuthRequest(
		t,
		server,
		http.MethodPost,
		"/auth/setup",
		`{"password":"`+testManagementPassword+`"}`,
		nil,
		"http://desktop.local",
	)
	if noNativeSession.Code != http.StatusUnauthorized {
		t.Fatalf("setup without native session status = %d, want 401: %s", noNativeSession.Code, noNativeSession.Body.String())
	}

	setup := serveAuthRequest(
		t,
		server,
		http.MethodPost,
		"/auth/setup",
		`{"password":"`+testManagementPassword+`"}`,
		nativeCookie,
		"http://desktop.local",
	)
	assertAuthStatus(t, setup, http.StatusOK, authStatusResponse{
		Mode:          "desktop",
		Initialized:   true,
		Authenticated: true,
	})
	setupCookie := responseCookie(t, setup, auth.DesktopSessionCookie)
	oldNativeSession := serveAuthRequest(t, server, http.MethodGet, "/admin/config/snapshot", "", nativeCookie, "")
	if oldNativeSession.Code != http.StatusUnauthorized {
		t.Fatalf("pre-setup native session status = %d, want 401: %s", oldNativeSession.Code, oldNativeSession.Body.String())
	}

	stored, err := os.ReadFile(passwordPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), testManagementPassword) {
		t.Fatal("management password was persisted in plaintext")
	}

	repeatedSetup := serveAuthRequest(
		t,
		server,
		http.MethodPost,
		"/auth/setup",
		`{"password":"another valid password"}`,
		setupCookie,
		"http://desktop.local",
	)
	if repeatedSetup.Code != http.StatusConflict {
		t.Fatalf("repeated setup status = %d, want 409: %s", repeatedSetup.Code, repeatedSetup.Body.String())
	}

	wrongLogin := serveAuthRequest(
		t,
		server,
		http.MethodPost,
		"/auth/login",
		`{"password":"incorrect password"}`,
		nil,
		"http://desktop.local",
	)
	if wrongLogin.Code != http.StatusUnauthorized {
		t.Fatalf("wrong login status = %d, want 401: %s", wrongLogin.Code, wrongLogin.Body.String())
	}

	login := serveAuthRequest(
		t,
		server,
		http.MethodPost,
		"/auth/login",
		`{"password":"`+testManagementPassword+`"}`,
		nil,
		"http://desktop.local",
	)
	assertAuthStatus(t, login, http.StatusOK, authStatusResponse{
		Mode:          "desktop",
		Initialized:   true,
		Authenticated: true,
	})
	browserCookie := responseCookie(t, login, auth.DesktopSessionCookie)
	if !browserCookie.HttpOnly || browserCookie.SameSite != http.SameSiteStrictMode || browserCookie.Path != "/" {
		t.Fatalf("login cookie flags are unsafe: %+v", browserCookie)
	}

	snapshot := serveAuthRequest(t, server, http.MethodGet, "/admin/config/snapshot", "", browserCookie, "")
	if snapshot.Code != http.StatusOK {
		t.Fatalf("logged-in snapshot status = %d, want 200: %s", snapshot.Code, snapshot.Body.String())
	}
	reload := serveAuthRequest(t, server, http.MethodPost, "/admin/config/reload", "", browserCookie, "http://desktop.local")
	if reload.Code != http.StatusOK {
		t.Fatalf("logged-in same-origin reload status = %d, want 200: %s", reload.Code, reload.Body.String())
	}

	logout := serveAuthRequest(t, server, http.MethodPost, "/auth/logout", "", browserCookie, "http://desktop.local")
	assertAuthStatus(t, logout, http.StatusOK, authStatusResponse{
		Mode:          "desktop",
		Initialized:   true,
		Authenticated: false,
	})
	clearCookie := responseCookie(t, logout, auth.DesktopSessionCookie)
	if clearCookie.MaxAge != -1 {
		t.Fatalf("logout cookie MaxAge = %d, want -1", clearCookie.MaxAge)
	}
	afterLogout := serveAuthRequest(t, server, http.MethodGet, "/admin/config/snapshot", "", browserCookie, "")
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("revoked browser session status = %d, want 401: %s", afterLogout.Code, afterLogout.Body.String())
	}
}

func TestDesktopAuthStatusReflectsNativeBootstrapSession(t *testing.T) {
	server, sessions, _ := newPasswordAuthTestServer(t)
	nonce, err := sessions.NewBootstrapNonce()
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := serveAuthRequest(t, server, http.MethodGet, "/desktop/bootstrap/"+nonce, "", nil, "")
	if bootstrap.Code != http.StatusSeeOther {
		t.Fatalf("bootstrap status = %d, want 303: %s", bootstrap.Code, bootstrap.Body.String())
	}
	nativeCookie := responseCookie(t, bootstrap, auth.DesktopSessionCookie)
	status := serveAuthRequest(t, server, http.MethodGet, "/auth/status", "", nativeCookie, "")
	assertAuthStatus(t, status, http.StatusOK, authStatusResponse{
		Mode:          "desktop",
		Initialized:   false,
		Authenticated: true,
	})
}

func TestTokenModeAuthStatusAndPasswordRoutes(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	path := writeAdminTestConfig(t)
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	server := New(path, cfg, nil, testProm)

	unauthenticated := serveAuthRequest(t, server, http.MethodGet, "/auth/status", "", nil, "")
	assertAuthStatus(t, unauthenticated, http.StatusOK, authStatusResponse{
		Mode:          "token",
		Initialized:   true,
		Authenticated: false,
	})

	req := httptest.NewRequest(http.MethodGet, "http://desktop.local/auth/status", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	authenticated := httptest.NewRecorder()
	server.Routes().ServeHTTP(authenticated, req)
	assertAuthStatus(t, authenticated, http.StatusOK, authStatusResponse{
		Mode:          "token",
		Initialized:   true,
		Authenticated: true,
	})

	for _, target := range []string{"/auth/setup", "/auth/login", "/auth/logout"} {
		response := serveAuthRequest(t, server, http.MethodPost, target, `{}`, nil, "http://desktop.local")
		if response.Code != http.StatusNotFound {
			t.Fatalf("token-mode %s status = %d, want 404: %s", target, response.Code, response.Body.String())
		}
	}
}

func newPasswordAuthTestServer(t *testing.T) (*Server, *auth.DesktopSessionStore, string) {
	t.Helper()
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "environment-admin-token")
	path := writeAdminTestConfig(t)
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	passwordPath := filepath.Join(t.TempDir(), "auth.json")
	passwordAuth, err := auth.NewPasswordAuth(passwordPath)
	if err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewDesktopSessionStore(time.Now, time.Minute)
	testPromOnce.Do(func() { testProm = metrics.New() })
	return NewWithOptions(path, cfg, nil, testProm, Options{
		AdminTokenOverride: "hidden-admin-token",
		DesktopSessions:    sessions,
		PasswordAuth:       passwordAuth,
	}), sessions, passwordPath
}

func serveAuthRequest(
	t *testing.T,
	server *Server,
	method string,
	target string,
	body string,
	cookie *http.Cookie,
	origin string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://desktop.local"+target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, req)
	return response
}

func assertAuthStatus(t *testing.T, response *httptest.ResponseRecorder, wantCode int, want authStatusResponse) {
	t.Helper()
	if response.Code != wantCode {
		t.Fatalf("status = %d, want %d: %s", response.Code, wantCode, response.Body.String())
	}
	var got authStatusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode auth status: %v: %s", err, response.Body.String())
	}
	if got != want {
		t.Fatalf("auth status = %+v, want %+v", got, want)
	}
}

func responseCookie(t *testing.T, response *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response did not set cookie %q", name)
	return nil
}
