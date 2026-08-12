package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/auth"
	"github.com/a448582655/vibe-proxy/internal/config"
)

func (s *Server) adminAuthorize(w http.ResponseWriter, r *http.Request) bool {
	snap := s.current()
	if auth.AuthorizeAdmin(r, snap.AdminToken) {
		return true
	}
	if s.desktopSessions == nil || !s.desktopSessions.Authorize(r) {
		writeUnauthorized(w)
		return false
	}
	if isSafeMethod(r.Method) || sameOrigin(r) {
		return true
	}
	writeForbidden(w, "desktop session requires same-origin request")
	return false
}

func writeUnauthorized(w http.ResponseWriter) {
	http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
}

func writeForbidden(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusForbidden)
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func sameOrigin(r *http.Request) bool {
	origins := r.Header.Values("Origin")
	if len(origins) != 1 || origins[0] == "" || r.Host == "" {
		return false
	}
	rawOrigin := origins[0]
	origin, err := url.Parse(rawOrigin)
	if err != nil || !origin.IsAbs() || origin.Opaque != "" || origin.User != nil {
		return false
	}
	if !strings.EqualFold(origin.Scheme, "http") && !strings.EqualFold(origin.Scheme, "https") {
		return false
	}
	if origin.Host == "" || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	return strings.EqualFold(origin.Host, r.Host)
}

func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	s.writeJSONStatus(w, http.StatusOK, v)
}

func (s *Server) writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writeJSONError(w http.ResponseWriter, status int, message string) {
	s.writeJSONStatus(w, status, map[string]string{"error": message})
}

// ---- Provider Delete ----

func (s *Server) adminProviderDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, `{"error":"missing id"}`, http.StatusBadRequest)
		return
	}
	if s.cfgPath == "" {
		http.Error(w, `{"error":"config path not writable"}`, http.StatusBadRequest)
		return
	}
	next, err := config.DeleteProvider(s.cfgPath, body.ID)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.applyRuntimeConfig(next)
	s.writeJSON(w, map[string]bool{"ok": true})
}

// ---- Provider Update ----

func (s *Server) adminProviderUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.cfgPath == "" {
		http.Error(w, `{"error":"config path not writable"}`, http.StatusBadRequest)
		return
	}
	var input config.LocalProviderInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if input.ID == "" || input.BaseURL == "" {
		http.Error(w, `{"error":"id and base_url are required"}`, http.StatusBadRequest)
		return
	}
	next, err := config.UpdateProvider(s.cfgPath, input)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.applyRuntimeConfig(next)
	s.writeJSON(w, map[string]any{"ok": true, "loaded_at": s.current().LoadedAt, "issues": config.ValidateRuntime(next)})
}

func (s *Server) adminProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.adminSnapshot(w, r)
	case http.MethodPost:
		s.adminLocalConfigure(w, r)
	case http.MethodPut:
		s.adminProviderUpdate(w, r)
	case http.MethodDelete:
		s.adminProviderDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---- Aliases ----

func (s *Server) adminAliases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.adminAuthorize(w, r) {
			return
		}
		snap := s.current()
		aliases := map[string]string{}
		for name, a := range snap.Config.ModelResolver.Aliases {
			aliases[name] = a.Provider + "/" + a.Model
		}
		s.writeJSON(w, map[string]any{
			"default_model": snap.Config.ModelResolver.DefaultModel,
			"allow_raw":     snap.Config.ModelResolver.AllowRaw,
			"aliases":       aliases,
		})
	case http.MethodPost:
		s.adminAliasesCreate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) adminAliasesCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.cfgPath == "" {
		http.Error(w, `{"error":"config path not writable"}`, http.StatusBadRequest)
		return
	}
	var body struct {
		Alias  string `json:"alias"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Alias == "" || body.Target == "" {
		http.Error(w, `{"error":"alias and target are required"}`, http.StatusBadRequest)
		return
	}
	next, err := config.UpsertAlias(s.cfgPath, body.Alias, body.Target)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.applyRuntimeConfig(next)
	s.writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) adminAliasesDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.cfgPath == "" {
		http.Error(w, `{"error":"config path not writable"}`, http.StatusBadRequest)
		return
	}
	alias := strings.TrimPrefix(r.URL.Path, "/admin/aliases/")
	if alias == "" {
		http.Error(w, `{"error":"alias is required"}`, http.StatusBadRequest)
		return
	}
	next, err := config.DeleteAlias(s.cfgPath, alias)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.applyRuntimeConfig(next)
	s.writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) adminAliasesDefaults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.cfgPath == "" {
		http.Error(w, `{"error":"config path not writable"}`, http.StatusBadRequest)
		return
	}
	var body struct {
		DefaultModel string `json:"default_model"`
		AllowRaw     bool   `json:"allow_raw"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	next, err := config.UpdateAliasDefaults(s.cfgPath, body.DefaultModel, body.AllowRaw)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.applyRuntimeConfig(next)
	s.writeJSON(w, map[string]bool{"ok": true})
}

// ---- Client Keys ----

type clientKeyAdminView struct {
	Name          string   `json:"name"`
	KeyPrefix     string   `json:"key_prefix"`
	Recoverable   bool     `json:"recoverable"`
	Enabled       bool     `json:"enabled"`
	AllowedModels []string `json:"allowed_models"`
	RPM           int      `json:"rpm"`
	CreatedAt     string   `json:"created_at,omitempty"`
}

func newClientKeyAdminView(key config.ClientKeyConfig) clientKeyAdminView {
	return clientKeyAdminView{
		Name:          key.Name,
		KeyPrefix:     key.KeyPrefix,
		Recoverable:   key.RawKey != "",
		Enabled:       key.Enabled,
		AllowedModels: key.AllowedModels,
		RPM:           key.RPM,
	}
}

func findClientKey(keys []config.ClientKeyConfig, name string) (config.ClientKeyConfig, bool) {
	for _, key := range keys {
		if key.Name == name {
			return key, true
		}
	}
	return config.ClientKeyConfig{}, false
}

func (s *Server) adminClientKeysList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	snap := s.current()
	keys := make([]clientKeyAdminView, 0, len(snap.Config.ClientKeys))
	for _, k := range snap.Config.ClientKeys {
		keys = append(keys, newClientKeyAdminView(k))
	}
	s.writeJSON(w, map[string]any{"keys": keys})
}

func (s *Server) adminClientKeysCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.cfgPath == "" {
		http.Error(w, `{"error":"config path not writable"}`, http.StatusBadRequest)
		return
	}
	var input config.ClientKeyInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if input.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	next, rawKey, err := config.UpsertClientKey(s.cfgPath, input)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.applyRuntimeConfig(next)
	created, _ := findClientKey(next.ClientKeys, input.Name)
	w.Header().Set("Cache-Control", "no-store")
	s.writeJSON(w, map[string]any{
		"key":     newClientKeyAdminView(created),
		"raw_key": rawKey,
	})
}

func (s *Server) adminClientKeyReveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	name := strings.TrimPrefix(r.URL.Path, "/admin/client-keys/")
	if name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	for _, key := range s.current().Config.ClientKeys {
		if key.Name != name {
			continue
		}
		if key.RawKey == "" {
			s.writeJSONStatus(w, http.StatusConflict, map[string]string{
				"code":  "client_key_not_recoverable",
				"error": "client key was created before recoverable local storage was enabled",
			})
			return
		}
		s.writeJSON(w, map[string]string{"name": key.Name, "raw_key": key.RawKey})
		return
	}
	s.writeJSONError(w, http.StatusNotFound, "client key not found")
}

func (s *Server) adminClientKeysUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.cfgPath == "" {
		http.Error(w, `{"error":"config path not writable"}`, http.StatusBadRequest)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/admin/client-keys/")
	if name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	var update config.ClientKeyUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	next, err := config.UpdateClientKey(s.cfgPath, name, update)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.applyRuntimeConfig(next)
	s.writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) adminClientKeyRotate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.cfgPath == "" {
		http.Error(w, `{"error":"config path not writable"}`, http.StatusBadRequest)
		return
	}
	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/client-keys/"), "/rotate")
	if name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	next, rawKey, err := config.RotateClientKey(s.cfgPath, name)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.applyRuntimeConfig(next)
	rotated, _ := findClientKey(next.ClientKeys, name)
	w.Header().Set("Cache-Control", "no-store")
	s.writeJSON(w, map[string]any{
		"key":     newClientKeyAdminView(rotated),
		"raw_key": rawKey,
	})
}

func (s *Server) adminClientKeysDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.cfgPath == "" {
		http.Error(w, `{"error":"config path not writable"}`, http.StatusBadRequest)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/admin/client-keys/")
	if name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	next, err := config.DeleteClientKey(s.cfgPath, name)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.applyRuntimeConfig(next)
	s.writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) adminClientKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.adminClientKeysList(w, r)
	case http.MethodPost:
		s.adminClientKeysCreate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) adminClientKeysByName(w http.ResponseWriter, r *http.Request) {
	relativePath := strings.TrimPrefix(r.URL.Path, "/admin/client-keys/")
	if r.Method == http.MethodPost && strings.HasSuffix(relativePath, "/rotate") && strings.TrimSuffix(relativePath, "/rotate") != "" {
		s.adminClientKeyRotate(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.adminClientKeyReveal(w, r)
	case http.MethodPut:
		s.adminClientKeysUpdate(w, r)
	case http.MethodDelete:
		s.adminClientKeysDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---- Metrics ----

func (s *Server) adminMetricsSummary(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorize(w, r) {
		return
	}
	snap := s.current()
	totalProviders := len(snap.Config.Providers)
	totalModels := 0
	for _, p := range snap.Config.Providers {
		totalModels += len(p.Models)
	}
	var totalRequests int64
	var promptTokens int64
	var completionTokens int64
	var totalTokens int64
	var cacheReadTokens int64
	var cacheWriteTokens int64
	var todayRequests int64
	var cacheReportedRequests int64
	var cacheEligiblePromptTokens int64
	var cacheWeightedHitRatio float64
	var cacheReportingCoverage float64
	if s.observability != nil {
		now := time.Now().UTC()
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		summary, err := s.observability.MetricsSummary(todayStart)
		if err != nil {
			http.Error(w, `{"error":"metrics_summary_failed"}`, http.StatusInternalServerError)
			return
		}
		totalRequests = summary.TotalRequests
		todayRequests = summary.TodayRequests
		promptTokens = summary.TodayTokens.Prompt
		completionTokens = summary.TodayTokens.Completion
		totalTokens = summary.TodayTokens.Total
		cacheReadTokens = summary.TodayTokens.CacheRead
		cacheWriteTokens = summary.TodayTokens.CacheWrite
		cacheReportedRequests = summary.PromptCache.ReportedRequests
		cacheEligiblePromptTokens = summary.PromptCache.EligiblePromptTokens
		cacheWeightedHitRatio = summary.PromptCache.WeightedHitRatio
		cacheReportingCoverage = summary.PromptCache.ReportingCoverage
	}
	s.writeJSON(w, map[string]any{
		"total_requests":   totalRequests,
		"today_requests":   todayRequests,
		"active_providers": totalProviders,
		"total_models":     totalModels,
		"today_tokens": map[string]int64{
			"prompt":      promptTokens,
			"completion":  completionTokens,
			"total":       totalTokens,
			"cache_read":  cacheReadTokens,
			"cache_write": cacheWriteTokens,
		},
		"prompt_cache": map[string]any{
			"reported_requests":      cacheReportedRequests,
			"eligible_prompt_tokens": cacheEligiblePromptTokens,
			"weighted_hit_ratio":     cacheWeightedHitRatio,
			"reporting_coverage":     cacheReportingCoverage,
		},
	})
}

func (s *Server) adminMetricsHistory(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorize(w, r) {
		return
	}
	rangeName := r.URL.Query().Get("range")
	window, bucket, ok := metricsRange(rangeName)
	if !ok {
		http.Error(w, `{"error":"invalid_metrics_range"}`, http.StatusBadRequest)
		return
	}
	points := []any{}
	if s.observability != nil {
		history, err := s.observability.MetricsHistory(time.Now().UTC().Add(-window), bucket)
		if err != nil {
			http.Error(w, `{"error":"metrics_history_failed"}`, http.StatusInternalServerError)
			return
		}
		points = make([]any, len(history))
		for i := range history {
			points[i] = history[i]
		}
	}
	s.writeJSON(w, map[string]any{
		"range":  rangeName,
		"points": points,
	})
}

func metricsRange(value string) (window time.Duration, bucket time.Duration, ok bool) {
	switch value {
	case "", "1h":
		return time.Hour, 5 * time.Minute, true
	case "6h":
		return 6 * time.Hour, 30 * time.Minute, true
	case "24h":
		return 24 * time.Hour, time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, 6 * time.Hour, true
	default:
		return 0, 0, false
	}
}

func (s *Server) adminProviderHealth(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorize(w, r) {
		return
	}
	snap := s.current()
	type provHealth struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		BaseURL   string `json:"base_url"`
		Healthy   bool   `json:"healthy"`
		LatencyMs int64  `json:"latency_ms,omitempty"`
	}
	healthList := make([]provHealth, 0, len(snap.Config.Providers))
	for id, p := range snap.Config.Providers {
		h := provHealth{
			ID:      id,
			Type:    p.Type,
			BaseURL: p.BaseURL,
			Healthy: false,
		}
		h.Healthy, h.LatencyMs = s.probeProviderHealth(r.Context(), p)
		healthList = append(healthList, h)
	}
	s.writeJSON(w, map[string]any{"providers": healthList})
}

func (s *Server) probeProviderHealth(parent context.Context, provider config.ProviderConfig) (bool, int64) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, providerProbeURL(provider), nil)
	if err != nil {
		return false, 0
	}
	if authErr := provider.Auth.Apply(req); authErr != nil {
		return false, 0
	}
	started := time.Now()
	resp, err := s.httpClient.Do(req)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		return false, latency
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	return resp.StatusCode >= 200 && resp.StatusCode < 400, latency
}

// ---- Raw Config ----

func (s *Server) adminRawConfig(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.cfgPath == "" {
		http.Error(w, `{"error":"config path not available"}`, http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		yamlContent, err := config.RawConfig(s.cfgPath)
		if err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, map[string]string{"yaml": yamlContent})
	case http.MethodPut:
		var body struct {
			YAML string `json:"yaml"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		next, err := config.SaveRawConfig(s.cfgPath, body.YAML)
		if err != nil {
			s.writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.applyRuntimeConfig(next)
		s.writeJSON(w, map[string]any{"ok": true, "issues": config.ValidateRuntime(next), "loaded_at": s.current().LoadedAt})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
