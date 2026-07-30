package runtime

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/auth"
	"github.com/a448582655/vibe-proxy/internal/desktopbridge"
)

const desktopBootstrapPrefix = "/desktop/bootstrap/"

func (s *Server) desktopBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	nonce := strings.TrimPrefix(r.URL.Path, desktopBootstrapPrefix)
	if nonce == "" || strings.Contains(nonce, "/") {
		http.NotFound(w, r)
		return
	}
	session, ok := s.desktopSessions.ConsumeBootstrap(nonce)
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.DesktopSessionCookie,
		Value:    session,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) spaRoutes() http.Handler {
	spa := spaFallback(spaHandler(), "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == strings.TrimSuffix(desktopBootstrapPrefix, "/") ||
			strings.HasPrefix(r.URL.Path, desktopBootstrapPrefix) {
			http.NotFound(w, r)
			return
		}
		spa.ServeHTTP(w, r)
	})
}

func (s *Server) desktopSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.desktopController == nil {
		s.writeJSON(w, struct {
			Available bool `json:"available"`
		}{Available: false})
		return
	}
	s.writeJSON(w, s.desktopController.Snapshot())
}

func (s *Server) desktopPreferences(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		s.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.desktopController == nil {
		s.writeJSONError(w, http.StatusConflict, "desktop controls are unavailable")
		return
	}
	var input struct {
		CloseBehavior desktopbridge.CloseBehavior `json:"close_behavior"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || !isCloseBehavior(input.CloseBehavior) {
		s.writeJSONError(w, http.StatusBadRequest, "invalid close_behavior")
		return
	}
	if err := s.desktopController.SetCloseBehavior(input.CloseBehavior); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "desktop controller operation failed")
		return
	}
	s.writeJSON(w, struct {
		CloseBehavior desktopbridge.CloseBehavior `json:"close_behavior"`
	}{CloseBehavior: input.CloseBehavior})
}

func (s *Server) desktopOpenDataDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.desktopController == nil {
		s.writeJSONError(w, http.StatusConflict, "desktop controls are unavailable")
		return
	}
	if err := s.desktopController.OpenDataDir(); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "desktop controller operation failed")
		return
	}
	s.writeJSON(w, map[string]bool{"opened": true})
}

func (s *Server) desktopImportConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.desktopController == nil {
		s.writeJSONError(w, http.StatusConflict, "desktop controls are unavailable")
		return
	}
	result, err := s.desktopController.ImportConfig(r.Context())
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "desktop controller operation failed")
		return
	}
	if !result.Imported {
		s.writeJSON(w, result)
		return
	}
	if !s.reloadRuntimeConfig(w) {
		return
	}
	s.writeJSON(w, struct {
		desktopbridge.ImportResult
		LoadedAt time.Time `json:"loaded_at"`
	}{ImportResult: result, LoadedAt: s.current().LoadedAt})
}

func isCloseBehavior(behavior desktopbridge.CloseBehavior) bool {
	switch behavior {
	case desktopbridge.CloseAsk, desktopbridge.CloseTray, desktopbridge.CloseQuit:
		return true
	default:
		return false
	}
}
