package runtime

import (
	"net/http"
	"strings"

	"github.com/a448582655/vibe-proxy/internal/auth"
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
