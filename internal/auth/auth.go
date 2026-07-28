package auth

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/types"
	"golang.org/x/crypto/bcrypt"
)

type Client struct {
	Name          string
	AllowedModels []string
	RPM           int
}

type Limiter struct {
	mu       sync.Mutex
	tokens   float64
	lastSeen time.Time
	rate     float64
	burst    float64
}

func NewLimiter(rpm int) *Limiter {
	if rpm <= 0 {
		rpm = 60
	}
	return &Limiter{tokens: float64(rpm), lastSeen: time.Now(), rate: float64(rpm) / 60.0, burst: float64(rpm)}
}

func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(l.lastSeen).Seconds()
	l.lastSeen = now
	l.tokens += elapsed * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}

type Authenticator struct {
	limiters sync.Map
}

func NewAuthenticator() *Authenticator { return &Authenticator{} }

func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

func (a *Authenticator) AuthenticateDataPlane(r *http.Request, keys []config.ClientKeyConfig) (Client, *types.GatewayError) {
	token := BearerToken(r)
	if token == "" {
		return Client{}, &types.GatewayError{StatusCode: http.StatusUnauthorized, Type: "authentication_error", Code: "missing_api_key", Message: "Missing bearer token."}
	}
	for _, k := range keys {
		if !k.Enabled {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(k.KeyHash), []byte(token)) == nil {
			// Include the key material and configured limit so deleting,
			// recreating, or editing a same-named key cannot accidentally
			// reuse a stale limiter.
			limiterID := k.Name + "\x00" + k.KeyHash + "\x00" + strconv.Itoa(k.RPM)
			limAny, _ := a.limiters.LoadOrStore(limiterID, NewLimiter(k.RPM))
			lim := limAny.(*Limiter)
			if !lim.Allow() {
				return Client{}, &types.GatewayError{StatusCode: http.StatusTooManyRequests, Type: "rate_limit_error", Code: "rate_limit_exceeded", Message: "Rate limit exceeded.", RetryAfter: "60"}
			}
			return Client{Name: k.Name, AllowedModels: k.AllowedModels, RPM: k.RPM}, nil
		}
	}
	return Client{}, &types.GatewayError{StatusCode: http.StatusUnauthorized, Type: "authentication_error", Code: "invalid_api_key", Message: "Invalid API key."}
}

func AuthorizeAdmin(r *http.Request, adminToken string) bool {
	if adminToken == "" {
		return false
	}
	token := BearerToken(r)
	return subtle.ConstantTimeCompare([]byte(token), []byte(adminToken)) == 1
}

func ModelAllowed(allowed []string, model string) bool {
	if len(allowed) == 0 {
		return false
	}
	for _, p := range allowed {
		if p == "*" || p == model || strings.HasSuffix(p, "*") && strings.HasPrefix(model, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}
