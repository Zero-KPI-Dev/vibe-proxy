package auth

import (
	"net/http"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func TestAuthenticateDataPlaneRefreshesLimiterWhenRPMChanges(t *testing.T) {
	const rawKey = "sk-test-rate-limit-key"
	hash, err := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	request := func() *http.Request {
		req, err := http.NewRequest(http.MethodGet, "http://vibe-proxy.local/v1/models", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+rawKey)
		return req
	}
	authenticator := NewAuthenticator()
	key := config.ClientKeyConfig{Name: "agent", KeyHash: string(hash), Enabled: true, AllowedModels: []string{"*"}, RPM: 1}

	if _, gatewayErr := authenticator.AuthenticateDataPlane(request(), []config.ClientKeyConfig{key}); gatewayErr != nil {
		t.Fatalf("first request rejected: %+v", gatewayErr)
	}
	if _, gatewayErr := authenticator.AuthenticateDataPlane(request(), []config.ClientKeyConfig{key}); gatewayErr == nil || gatewayErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected original limiter to be exhausted: %+v", gatewayErr)
	}

	key.RPM = 2
	for i := 0; i < 2; i++ {
		if _, gatewayErr := authenticator.AuthenticateDataPlane(request(), []config.ClientKeyConfig{key}); gatewayErr != nil {
			t.Fatalf("updated limiter request %d rejected: %+v", i+1, gatewayErr)
		}
	}
	if _, gatewayErr := authenticator.AuthenticateDataPlane(request(), []config.ClientKeyConfig{key}); gatewayErr == nil || gatewayErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected updated limiter to enforce the new RPM: %+v", gatewayErr)
	}
}
