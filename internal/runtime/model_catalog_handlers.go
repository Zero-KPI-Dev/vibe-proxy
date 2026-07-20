package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/a448582655/vibe-proxy/internal/modelcatalog"
)

func (s *Server) adminModelCatalogStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	s.writeJSON(w, map[string]any{"catalog": s.catalog.State()})
}

func (s *Server) adminModelCatalogRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := s.catalog.Refresh(ctx); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]any{"error": "catalog_refresh_failed", "message": err.Error(), "catalog": s.catalog.State()})
		return
	}
	s.writeJSON(w, map[string]any{"ok": true, "catalog": s.catalog.State()})
}

type modelCatalogLookupInput struct {
	ProviderID      string   `json:"provider_id"`
	CatalogProvider string   `json:"catalog_provider"`
	BaseURL         string   `json:"base_url"`
	Models          []string `json:"models"`
	Refresh         bool     `json:"refresh"`
}

func (s *Server) adminModelCatalogLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	var input modelCatalogLookupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	var refreshErr error
	if input.Refresh || s.catalog.State().Models == 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		refreshErr = s.catalog.Ensure(ctx, 24*time.Hour)
		cancel()
	}
	result := map[string]any{
		"matches": s.catalogMatches(input.CatalogProvider, input.ProviderID, input.BaseURL, input.Models),
		"catalog": s.catalog.State(),
	}
	if refreshErr != nil {
		result["catalog_error"] = refreshErr.Error()
	}
	s.writeJSON(w, result)
}

func (s *Server) catalogMatches(catalogProvider, providerID, baseURL string, models []string) map[string]modelcatalog.Match {
	result := make(map[string]modelcatalog.Match, len(models))
	for _, modelID := range models {
		if modelID == "" {
			continue
		}
		result[modelID] = s.catalog.Lookup(catalogProvider, providerID, baseURL, modelID)
	}
	return result
}
