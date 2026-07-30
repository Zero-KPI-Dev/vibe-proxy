package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDesktopSessionBootstrapNonceIsURLSafeSingleUseAndExpiring(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	store := NewDesktopSessionStore(func() time.Time { return now }, time.Minute)

	nonce, err := store.NewBootstrapNonce()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil {
		t.Fatalf("bootstrap nonce is not raw URL-safe base64: %q: %v", nonce, err)
	}
	if len(decoded) != 32 {
		t.Fatalf("bootstrap nonce decoded to %d bytes, want 32", len(decoded))
	}

	session, ok := store.ConsumeBootstrap(nonce)
	if !ok || session == "" {
		t.Fatal("fresh bootstrap nonce was not consumed")
	}
	if _, reused := store.ConsumeBootstrap(nonce); reused {
		t.Fatal("bootstrap nonce was accepted more than once")
	}

	req := httptest.NewRequest(http.MethodGet, "http://desktop.local/admin/config/snapshot", nil)
	req.AddCookie(&http.Cookie{Name: DesktopSessionCookie, Value: session})
	if !store.Authorize(req) {
		t.Fatal("issued desktop session cookie was not authorized")
	}
	store.Revoke(session)
	if store.Authorize(req) {
		t.Fatal("individually revoked desktop session cookie was still authorized")
	}
	session, err = store.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "http://desktop.local/admin/config/snapshot", nil)
	req.AddCookie(&http.Cookie{Name: DesktopSessionCookie, Value: session})
	store.RevokeAll()
	if store.Authorize(req) {
		t.Fatal("revoked desktop session cookie was still authorized")
	}

	expiringNonce, err := store.NewBootstrapNonce()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute + time.Nanosecond)
	if _, accepted := store.ConsumeBootstrap(expiringNonce); accepted {
		t.Fatal("expired bootstrap nonce was accepted")
	}

	session, err = store.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "http://desktop.local/admin/config/snapshot", nil)
	req.AddCookie(&http.Cookie{Name: DesktopSessionCookie, Value: session})
	now = now.Add(desktopSessionTTL + time.Nanosecond)
	if store.Authorize(req) {
		t.Fatal("expired desktop session cookie was authorized")
	}
}
