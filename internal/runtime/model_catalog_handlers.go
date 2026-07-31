package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
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
	s.writeJSON(w, map[string]any{
		"catalog": s.catalog.State(),
		"proxy":   modelCatalogProxyView(s.current().Config.ModelCatalog.ProxyURL),
	})
}

func (s *Server) adminModelCatalogSettings(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorize(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.writeJSON(w, map[string]any{
			"proxy": modelCatalogProxyView(s.current().Config.ModelCatalog.ProxyURL),
		})
	case http.MethodPut:
		if s.cfgPath == "" {
			s.writeJSONError(w, http.StatusBadRequest, "config path not writable")
			return
		}
		var input struct {
			ProxyURL string `json:"proxy_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			s.writeJSONError(w, http.StatusBadRequest, "invalid json")
			return
		}
		next, err := config.UpdateModelCatalogProxy(s.cfgPath, input.ProxyURL)
		if err != nil {
			s.writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.applyRuntimeConfig(next)
		s.writeJSON(w, map[string]any{
			"ok":    true,
			"proxy": modelCatalogProxyView(next.ModelCatalog.ProxyURL),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func modelCatalogProxyView(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{"configured": false}
	}
	display := raw
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		parsed.User = nil
		parsed.Path = ""
		parsed.RawPath = ""
		parsed.RawQuery = ""
		parsed.Fragment = ""
		display = parsed.String()
	}
	return map[string]any{"configured": true, "display_url": display}
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

func (s *Server) adminModelCatalogImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, modelcatalog.MaxCatalogBytes+(1<<20))
	var reader io.Reader = r.Body
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if strings.EqualFold(mediaType, "multipart/form-data") {
		if err := r.ParseMultipartForm(modelcatalog.MaxCatalogBytes + (1 << 20)); err != nil {
			s.modelCatalogImportError(w, fmt.Errorf("read uploaded catalog: %w", err))
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		file, _, err := r.FormFile("catalog")
		if err != nil {
			s.modelCatalogImportError(w, fmt.Errorf("catalog file is required: %w", err))
			return
		}
		defer file.Close()
		reader = file
	}

	if err := s.catalog.Import(reader); err != nil {
		s.modelCatalogImportError(w, err)
		return
	}
	s.writeJSON(w, map[string]any{"ok": true, "catalog": s.catalog.State()})
}

func (s *Server) modelCatalogImportError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":   "catalog_import_failed",
		"message": err.Error(),
		"catalog": s.catalog.State(),
	})
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
