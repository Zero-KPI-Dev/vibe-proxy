package runtime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/auth"
	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/types"
)

type rejectedDataPlaneAuth struct{ failure types.GatewayError }

func (a rejectedDataPlaneAuth) AuthenticateDataPlane(*http.Request, []config.ClientKeyConfig) (auth.Client, *types.GatewayError) {
	return auth.Client{}, &a.failure
}

func TestDataEndpointsPreserveAuthenticationRetryAfter(t *testing.T) {
	// Inject the authentication result, not the HTTP response. This exercises
	// each real router/error encoder without depending on bcrypt scheduling.
	for _, failure := range []types.GatewayError{
		{StatusCode: 503, Type: "server_error", Code: "authentication_busy", Message: "Authentication is busy.", RetryAfter: "1"},
		{StatusCode: 429, Type: "rate_limit_error", Code: "rate_limit_exceeded", Message: "Rate limit exceeded.", RetryAfter: "60"},
		{StatusCode: 401, Type: "authentication_error", Code: "invalid_api_key", Message: "Invalid API key."},
	} {
		for _, endpoint := range []struct{ method, path string }{
			{http.MethodGet, "/v1/models"},
			{http.MethodPost, "/v1/chat/completions"},
			{http.MethodPost, "/v1/responses"},
			{http.MethodPost, "/anthropic/v1/messages"},
		} {
			t.Run(failure.Code+endpoint.path, func(t *testing.T) {
				s := newTestServer(t, nil)
				s.authenticator = rejectedDataPlaneAuth{failure: failure}
				req := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(`{}`))
				req.Header.Set("Authorization", "Bearer test-only-key")
				w := httptest.NewRecorder()
				s.DataRoutes().ServeHTTP(w, req)
				response := w.Result()
				defer response.Body.Close()
				if response.StatusCode != failure.StatusCode || response.Header.Get("Retry-After") != failure.RetryAfter {
					t.Fatalf("status=%d Retry-After=%q; want status=%d Retry-After=%q", response.StatusCode, response.Header.Get("Retry-After"), failure.StatusCode, failure.RetryAfter)
				}
				if response.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("unexpected content type: %s", response.Header.Get("Content-Type"))
				}
				var body struct {
					Error struct {
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Error.Code != failure.Code || body.Error.Message != failure.Message {
					t.Fatalf("authentication error changed: %+v", body.Error)
				}
			})
		}
	}
}
