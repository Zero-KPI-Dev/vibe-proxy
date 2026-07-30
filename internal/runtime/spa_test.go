package runtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSPAServesIndexAndFallsBackForClientRoutes(t *testing.T) {
	h := spaFallback(spaHandler(), "index.html")

	for _, path := range []string{"/", "/providers/new"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "vibe-proxy") && !strings.Contains(w.Body.String(), "/assets/") {
			t.Fatalf("%s did not look like the SPA index: %s", path, w.Body.String())
		}
	}
}
