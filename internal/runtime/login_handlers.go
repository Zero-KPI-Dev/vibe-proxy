package runtime

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/a448582655/vibe-proxy/internal/auth"
)

type authStatusResponse struct {
	Mode          string `json:"mode"`
	Initialized   bool   `json:"initialized"`
	Authenticated bool   `json:"authenticated"`
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.passwordAuth == nil || s.desktopSessions == nil {
		s.writeJSON(w, authStatusResponse{
			Mode:          "token",
			Initialized:   true,
			Authenticated: auth.AuthorizeAdmin(r, s.current().AdminToken),
		})
		return
	}
	s.writeJSON(w, authStatusResponse{
		Mode:          "desktop",
		Initialized:   s.passwordAuth.Initialized(),
		Authenticated: s.desktopSessions.Authorize(r),
	})
}

func (s *Server) authSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.passwordAuth == nil || s.desktopSessions == nil {
		http.NotFound(w, r)
		return
	}
	if !sameOrigin(r) {
		s.writeJSONError(w, http.StatusForbidden, "same-origin request required")
		return
	}
	if !s.desktopSessions.Authorize(r) {
		writeUnauthorized(w)
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.passwordAuth.SetInitial(input.Password); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, auth.ErrPasswordAlreadyInitialized) {
			status = http.StatusConflict
		}
		s.writeJSONError(w, status, err.Error())
		return
	}
	// First-run setup invalidates every bootstrap nonce and pre-setup session,
	// then gives only the completing native window a fresh session.
	s.desktopSessions.RevokeAll()
	session, err := s.desktopSessions.NewSession()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "create desktop session")
		return
	}
	setDesktopSessionCookie(w, session)
	s.writeJSON(w, authStatusResponse{Mode: "desktop", Initialized: true, Authenticated: true})
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.passwordAuth == nil || s.desktopSessions == nil {
		http.NotFound(w, r)
		return
	}
	if !sameOrigin(r) {
		s.writeJSONError(w, http.StatusForbidden, "same-origin request required")
		return
	}
	if !s.passwordAuth.Initialized() {
		s.writeJSONError(w, http.StatusConflict, "management password is not initialized")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !s.passwordAuth.Verify(input.Password) {
		// Keep failed attempts expensive enough to discourage rapid local
		// guessing without introducing a persistent account lockout.
		time.Sleep(150 * time.Millisecond)
		writeUnauthorized(w)
		return
	}
	session, err := s.desktopSessions.NewSession()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "create desktop session")
		return
	}
	setDesktopSessionCookie(w, session)
	s.writeJSON(w, authStatusResponse{Mode: "desktop", Initialized: true, Authenticated: true})
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.passwordAuth == nil || s.desktopSessions == nil {
		http.NotFound(w, r)
		return
	}
	if !sameOrigin(r) {
		s.writeJSONError(w, http.StatusForbidden, "same-origin request required")
		return
	}
	if cookie, err := r.Cookie(auth.DesktopSessionCookie); err == nil {
		s.desktopSessions.Revoke(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.DesktopSessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	s.writeJSON(w, authStatusResponse{Mode: "desktop", Initialized: s.passwordAuth.Initialized(), Authenticated: false})
}
