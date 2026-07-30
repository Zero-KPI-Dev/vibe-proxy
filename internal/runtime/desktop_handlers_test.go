package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/auth"
	"github.com/a448582655/vibe-proxy/internal/config"
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
