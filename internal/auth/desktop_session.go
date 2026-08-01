package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

const DesktopSessionCookie = "vibe_desktop_session"

type tokenDigest [sha256.Size]byte

const desktopSessionTTL = 12 * time.Hour

// DesktopSessionStore keeps one-time bootstrap nonces and authorized desktop
// sessions in process memory. Only digests of nonce and session values are
// retained.
type DesktopSessionStore struct {
	mu       sync.Mutex
	now      func() time.Time
	nonceTTL time.Duration
	nonces   map[tokenDigest]time.Time
	sessions map[tokenDigest]time.Time
}

func NewDesktopSessionStore(now func() time.Time, nonceTTL time.Duration) *DesktopSessionStore {
	if now == nil {
		now = time.Now
	}
	return &DesktopSessionStore{
		now:      now,
		nonceTTL: nonceTTL,
		nonces:   make(map[tokenDigest]time.Time),
		sessions: make(map[tokenDigest]time.Time),
	}
}

func (s *DesktopSessionStore) NewBootstrapNonce() (string, error) {
	nonce, err := randomDesktopToken()
	if err != nil {
		return "", err
	}
	digest := digestDesktopToken(nonce)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.deleteExpiredNonces(now)
	s.nonces[digest] = now.Add(s.nonceTTL)
	return nonce, nil
}

func (s *DesktopSessionStore) ConsumeBootstrap(nonce string) (session string, ok bool) {
	digest := digestDesktopToken(nonce)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteExpiredNonces(s.now())
	storedDigest, found := findNonceDigest(s.nonces, digest)
	if !found {
		return "", false
	}
	delete(s.nonces, storedDigest)

	session, err := s.newSessionLocked()
	if err != nil {
		return "", false
	}
	return session, true
}

func (s *DesktopSessionStore) NewSession() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.newSessionLocked()
}

func (s *DesktopSessionStore) Authorize(r *http.Request) bool {
	cookie, err := r.Cookie(DesktopSessionCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	digest := digestDesktopToken(cookie.Value)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteExpiredSessions(s.now())
	return containsSessionDigest(s.sessions, digest)
}

func (s *DesktopSessionStore) Revoke(session string) {
	if session == "" {
		return
	}
	digest := digestDesktopToken(session)
	s.mu.Lock()
	defer s.mu.Unlock()
	for stored := range s.sessions {
		if subtle.ConstantTimeCompare(stored[:], digest[:]) == 1 {
			delete(s.sessions, stored)
			return
		}
	}
}

func (s *DesktopSessionStore) RevokeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.nonces)
	clear(s.sessions)
}

func (s *DesktopSessionStore) deleteExpiredNonces(now time.Time) {
	for digest, expiresAt := range s.nonces {
		if !expiresAt.After(now) {
			delete(s.nonces, digest)
		}
	}
}

func (s *DesktopSessionStore) newSessionLocked() (string, error) {
	session, err := randomDesktopToken()
	if err != nil {
		return "", err
	}
	now := s.now()
	s.deleteExpiredSessions(now)
	s.sessions[digestDesktopToken(session)] = now.Add(desktopSessionTTL)
	return session, nil
}

func (s *DesktopSessionStore) deleteExpiredSessions(now time.Time) {
	for digest, expiresAt := range s.sessions {
		if !expiresAt.After(now) {
			delete(s.sessions, digest)
		}
	}
}

func findNonceDigest(nonces map[tokenDigest]time.Time, candidate tokenDigest) (tokenDigest, bool) {
	for digest := range nonces {
		if subtle.ConstantTimeCompare(digest[:], candidate[:]) == 1 {
			return digest, true
		}
	}
	return tokenDigest{}, false
}

func containsSessionDigest(sessions map[tokenDigest]time.Time, candidate tokenDigest) bool {
	found := 0
	for digest := range sessions {
		found |= subtle.ConstantTimeCompare(digest[:], candidate[:])
	}
	return found == 1
}

func digestDesktopToken(token string) tokenDigest {
	return sha256.Sum256([]byte(token))
}

func randomDesktopToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
