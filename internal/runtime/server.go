package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/a448582655/vibe-proxy/internal/auth"
	clientanthropic "github.com/a448582655/vibe-proxy/internal/clientadapters/anthropic"
	clientopenai "github.com/a448582655/vibe-proxy/internal/clientadapters/openai"
	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/metrics"
	"github.com/a448582655/vibe-proxy/internal/modelcatalog"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/protocol"
	provideranthropic "github.com/a448582655/vibe-proxy/internal/provideradapters/anthropic"
	provideropenai "github.com/a448582655/vibe-proxy/internal/provideradapters/openai"
	"github.com/a448582655/vibe-proxy/internal/streamengine"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
	"github.com/a448582655/vibe-proxy/internal/types"
	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
)

type Snapshot struct {
	LoadedAt   time.Time
	Config     *config.RuntimeConfig
	Resolver   *modelresolver.Resolver
	AdminToken string
}

type Server struct {
	cfgPath          string
	snapshot         atomic.Value
	authenticator    *auth.Authenticator
	httpClient       *http.Client
	clientAdapters   []protocol.ClientAdapter
	providerAdapters map[string]protocol.ProviderAdapter
	metrics          *metrics.Prometheus
	sink             telemetry.EventSink
	recent           *telemetry.RecentStore
	catalog          *modelcatalog.Service
	semaphore        sync.Map
}

func New(cfgPath string, cfg *config.RuntimeConfig, sink telemetry.EventSink, prom *metrics.Prometheus) *Server {
	var recent *telemetry.RecentStore
	if r, ok := sink.(*telemetry.RecentStore); ok {
		recent = r
	}
	if ms, ok := sink.(interface{ RecentStore() *telemetry.RecentStore }); ok {
		recent = ms.RecentStore()
	}
	s := &Server{cfgPath: cfgPath, authenticator: auth.NewAuthenticator(), httpClient: &http.Client{Timeout: 0}, metrics: prom, sink: sink, recent: recent, catalog: modelcatalog.NewService(modelcatalog.Options{CachePath: modelCatalogCachePath(cfgPath, cfg)}), clientAdapters: []protocol.ClientAdapter{clientopenai.ChatAdapter{}, clientopenai.ResponsesAdapter{}, clientanthropic.MessagesAdapter{}}, providerAdapters: map[string]protocol.ProviderAdapter{"anthropic": provideranthropic.Provider{}, "openai-compatible": provideropenai.Provider{}}}
	s.snapshot.Store(s.buildSnapshot(cfg))
	return s
}

func (s *Server) SetHTTPClient(client *http.Client) {
	if client != nil {
		s.httpClient = client
	}
}

func (s *Server) SetCatalogHTTPClient(client *http.Client) {
	if s.catalog != nil {
		s.catalog.SetHTTPClient(client)
	}
}

func modelCatalogCachePath(cfgPath string, cfg *config.RuntimeConfig) string {
	if cfgPath == "" || cfg == nil || cfg.Storage.SQLitePath == "" || cfg.Storage.SQLitePath == ":memory:" {
		return ""
	}
	return cfg.Storage.SQLitePath + ".models-dev.json"
}

func (s *Server) buildSnapshot(cfg *config.RuntimeConfig) *Snapshot {
	return &Snapshot{LoadedAt: time.Now(), Config: cfg, Resolver: modelresolver.New(cfg.ModelResolver), AdminToken: os.Getenv(cfg.Security.AdminBearerTokenEnv)}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Data-plane proxy endpoints
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/chat/completions", s.handle)
	mux.HandleFunc("/v1/responses", s.handle)
	mux.HandleFunc("/anthropic/v1/messages", s.handle)
	mux.HandleFunc("/v1/messages", s.handle)
	mux.Handle("/metrics", s.metrics.Handler())
	mux.HandleFunc("/healthz", s.healthz)

	// Admin API endpoints
	mux.HandleFunc("/admin/config/reload", s.reload)
	mux.HandleFunc("/admin/config/snapshot", s.adminSnapshot)
	mux.HandleFunc("/admin/config/validate", s.adminValidateConfig)
	mux.HandleFunc("/admin/config/raw", s.adminRawConfig)
	mux.HandleFunc("/admin/providers/test", s.adminProviderTest)
	mux.HandleFunc("/admin/providers/models", s.adminProviderModels)
	mux.HandleFunc("/admin/providers/health", s.adminProviderHealth)
	mux.HandleFunc("/admin/model-catalog/status", s.adminModelCatalogStatus)
	mux.HandleFunc("/admin/model-catalog/refresh", s.adminModelCatalogRefresh)
	mux.HandleFunc("/admin/model-catalog/lookup", s.adminModelCatalogLookup)
	mux.HandleFunc("/admin/providers", s.adminProviders) // GET (list), POST (create), PUT (update), DELETE (delete)
	mux.HandleFunc("/admin/local/configure", s.adminLocalConfigure)
	mux.HandleFunc("/admin/aliases/default", s.adminAliasesDefaults)
	mux.HandleFunc("/admin/aliases", s.adminAliases)               // GET (list), POST (create)
	mux.HandleFunc("/admin/aliases/", s.adminAliasesDelete)        // DELETE by alias name
	mux.HandleFunc("/admin/client-keys", s.adminClientKeys)        // GET (list), POST (create)
	mux.HandleFunc("/admin/client-keys/", s.adminClientKeysByName) // PUT (update), DELETE (delete)
	mux.HandleFunc("/admin/requests/recent", s.adminRecentRequests)
	mux.HandleFunc("/admin/metrics/summary", s.adminMetricsSummary)
	mux.HandleFunc("/admin/metrics/history", s.adminMetricsHistory)

	// SPA dashboard (must be last as catch-all)
	mux.Handle("/", spaFallback(spaHandler(), "index.html"))
	return limitBody(recordResponse(mux), 32<<20)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Method not allowed.", "type": "invalid_request_error", "code": "method_not_allowed"}})
		return
	}
	snap := s.current()
	if _, gerr := s.authenticator.AuthenticateDataPlane(r, snap.Config.ClientKeys); gerr != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(gerr.StatusCode)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": gerr.Message, "type": gerr.Type, "code": gerr.Code}})
		return
	}
	now := time.Now().Unix()
	seen := map[string]bool{}
	data := []map[string]any{}
	for alias := range snap.Config.ModelResolver.Aliases {
		if alias == "" || seen[alias] {
			continue
		}
		seen[alias] = true
		data = append(data, map[string]any{"id": alias, "object": "model", "created": now, "owned_by": "vibe-proxy", "vibe_type": "alias"})
	}
	for providerID, provider := range snap.Config.Providers {
		for _, model := range provider.Models {
			if model == "" || seen[model] {
				continue
			}
			seen[model] = true
			data = append(data, map[string]any{"id": model, "object": "model", "created": now, "owned_by": providerID, "vibe_type": "raw"})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	clientAdapter := s.detectClientAdapter(r)
	if clientAdapter == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		_ = clientAdapter.EncodeError(r.Context(), w, ir.GatewayError{StatusCode: 405, Kind: "invalid_request_error", Code: "method_not_allowed", Message: "Method not allowed."})
		return
	}
	client, gerr := s.authenticator.AuthenticateDataPlane(r, snap.Config.ClientKeys)
	if gerr != nil {
		_ = clientAdapter.EncodeError(r.Context(), w, toIRError(*gerr))
		return
	}
	creq, err := clientAdapter.ParseRequest(r.Context(), r)
	if err != nil {
		_ = clientAdapter.EncodeError(r.Context(), w, errorToIR(err))
		return
	}
	if !auth.ModelAllowed(client.AllowedModels, creq.RequestedModel) {
		_ = clientAdapter.EncodeError(r.Context(), w, ir.GatewayError{StatusCode: 403, Kind: "permission_error", Code: "model_not_allowed", Message: "This API key is not allowed to use the requested model."})
		return
	}
	target, ierr := snap.Resolver.Resolve(creq)
	if ierr != nil {
		_ = clientAdapter.EncodeError(r.Context(), w, *ierr)
		return
	}
	creq.ResolvedProvider = target.ProviderID
	creq.ResolvedModel = target.Model
	providerCfg, ok := snap.Config.Providers[target.ProviderID]
	if !ok {
		_ = clientAdapter.EncodeError(r.Context(), w, ir.GatewayError{StatusCode: 503, Kind: "config_error", Code: "provider_not_found", Message: "Resolved provider is not configured."})
		return
	}
	providerAdapter := s.providerAdapters[target.ProviderType]
	if providerAdapter == nil {
		_ = clientAdapter.EncodeError(r.Context(), w, ir.GatewayError{StatusCode: 501, Kind: "config_error", Code: "provider_adapter_not_found", Message: "Provider adapter is not available."})
		return
	}
	if !s.acquire(target.ProviderID, providerCfg.MaxConcurrency) {
		_ = clientAdapter.EncodeError(r.Context(), w, ir.GatewayError{StatusCode: 429, Kind: "rate_limit_error", Code: "provider_busy", Message: "Selected provider is busy.", RetryAfter: "1"})
		return
	}
	defer s.release(target.ProviderID)
	ctx, cancel := context.WithTimeout(r.Context(), providerCfg.Timeout.Duration)
	defer cancel()
	tracker := telemetry.NewTracker(telemetry.Event{RequestID: creq.ID, ClientName: client.Name, VirtualModel: creq.RequestedModel, UpstreamModel: target.Model, ChannelID: target.ProviderID, ProtocolIn: string(clientAdapter.Protocol()), ProtocolOut: string(providerAdapter.Protocol()), StartedAt: time.Now()}, s.sink)
	upReq, err := providerAdapter.BuildRequest(ctx, creq, target)
	if err != nil {
		ge := errorToIR(err)
		tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
		_ = clientAdapter.EncodeError(ctx, w, ge)
		return
	}
	if authErr := providerCfg.Auth.Apply(upReq); authErr != nil {
		tracker.Finish(authErr.StatusCode, types.Usage{}, authErr.Code)
		_ = clientAdapter.EncodeError(ctx, w, *authErr)
		return
	}
	resp, err := s.httpClient.Do(upReq)
	if err != nil {
		ge := ir.GatewayError{StatusCode: 502, Kind: "upstream_error", Code: "upstream_connection_failed", Message: "Could not connect to upstream provider."}
		tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
		_ = clientAdapter.EncodeError(ctx, w, ge)
		return
	}
	if creq.Stream {
		events, err := providerAdapter.ParseStream(ctx, resp)
		if err != nil {
			ge := errorToIR(err)
			tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
			_ = clientAdapter.EncodeError(ctx, w, ge)
			return
		}
		var streamStats streamengine.Stats
		tracked := streamengine.Track(ctx, events, func(stats streamengine.Stats) {
			streamStats = stats
			tracker.Event.Usage = toTelemetryUsage(stats.Usage)
		})
		if err := clientAdapter.EncodeStream(ctx, w, tracked); err != nil {
			ge := errorToIR(err)
			tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
			if !headersWritten(w) {
				_ = clientAdapter.EncodeError(ctx, w, ge)
			}
			return
		}
		tracker.Finish(http.StatusOK, toTelemetryUsage(streamStats.Usage), "")
		return
	}
	out, err := providerAdapter.ParseUnary(ctx, resp)
	if err != nil {
		ge := errorToIR(err)
		tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
		_ = clientAdapter.EncodeError(ctx, w, ge)
		return
	}
	if out.Model == "" {
		out.Model = creq.RequestedModel
	}
	if err := clientAdapter.EncodeUnary(ctx, w, out); err != nil {
		ge := errorToIR(err)
		tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
		return
	}
	tracker.Finish(http.StatusOK, toTelemetryUsage(out.Usage), "")
}

func (s *Server) detectClientAdapter(r *http.Request) protocol.ClientAdapter {
	for _, a := range s.clientAdapters {
		if a.Detect(r) {
			return a
		}
	}
	return nil
}
func (s *Server) current() *Snapshot { return s.snapshot.Load().(*Snapshot) }
func (s *Server) acquire(id string, max int) bool {
	if max <= 0 {
		max = 32
	}
	v, _ := s.semaphore.LoadOrStore(id, make(chan struct{}, max))
	select {
	case v.(chan struct{}) <- struct{}{}:
		return true
	default:
		return false
	}
}
func (s *Server) release(id string) {
	if v, ok := s.semaphore.Load(id); ok {
		select {
		case <-v.(chan struct{}):
		default:
		}
	}
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "loaded_at": s.current().LoadedAt})
}
func (s *Server) reload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	snap := s.current()
	if !auth.AuthorizeAdmin(r, snap.AdminToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	cfg, err := config.LoadRuntime(s.cfgPath)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.snapshot.Store(s.buildSnapshot(cfg))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"reloaded": true, "loaded_at": s.current().LoadedAt})
}
func (s *Server) adminSnapshot(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	if !auth.AuthorizeAdmin(r, snap.AdminToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	providers := []map[string]any{}
	for id, p := range snap.Config.Providers {
		authType, keySource, keyEnv := providerAuthMeta(p)
		providers = append(providers, map[string]any{"id": id, "type": p.Type, "base_url": p.BaseURL, "catalog_provider": p.CatalogProvider, "models": p.Models, "max_concurrency": p.MaxConcurrency, "auth_type": authType, "api_key_source": keySource, "api_key_env": keyEnv})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"loaded_at": snap.LoadedAt, "providers": providers, "model_resolver": snap.Config.ModelResolver})
}

func providerAuthMeta(p config.ProviderConfig) (authType string, keySource string, keyEnv string) {
	authType = p.Auth.Type
	if authType == "" {
		authType = "none"
	}
	var ref upstreamauth.SecretRef
	switch authType {
	case "bearer":
		ref = p.Auth.Token
	case "api_key_header":
		ref = p.Auth.Value
	}
	raw := string(ref)
	switch {
	case strings.HasPrefix(raw, "env:"):
		return authType, "env", strings.TrimPrefix(raw, "env:")
	case strings.HasPrefix(raw, "literal:"):
		return authType, "literal", ""
	case raw != "":
		return authType, "literal", ""
	default:
		return authType, "", ""
	}
}

func (s *Server) adminValidateConfig(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	if !auth.AuthorizeAdmin(r, snap.AdminToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	issues := config.ValidateRuntime(snap.Config)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"valid": !config.HasErrors(issues), "issues": issues})
}

func (s *Server) adminProviderTest(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	if !auth.AuthorizeAdmin(r, snap.AdminToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	providerID := r.URL.Query().Get("id")
	if providerID == "" {
		http.Error(w, "missing provider id", 400)
		return
	}
	p, ok := snap.Config.Providers[providerID]
	if !ok {
		http.Error(w, "provider not found", 404)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	testURL := providerProbeURL(p)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "provider": providerID, "error": "invalid_base_url"})
		return
	}
	if authErr := p.Auth.Apply(req); authErr != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "provider": providerID, "error": authErr.Code})
		return
	}
	started := time.Now()
	resp, err := s.httpClient.Do(req)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "provider": providerID, "latency_ms": latency, "error": "connection_failed"})
		return
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": resp.StatusCode >= 200 && resp.StatusCode < 400, "provider": providerID, "status": resp.StatusCode, "latency_ms": latency, "target": testURL})
}

func (s *Server) adminProviderModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap := s.current()
	if !auth.AuthorizeAdmin(r, snap.AdminToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var input config.LocalProviderInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if input.BaseURL == "" {
		http.Error(w, "base_url is required", http.StatusBadRequest)
		return
	}
	provider, err := config.BuildLocalProvider(input, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	target := providerProbeURL(provider)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, "invalid provider url", http.StatusBadRequest)
		return
	}
	if authErr := provider.Auth.Apply(req); authErr != nil {
		http.Error(w, authErr.Code, http.StatusBadRequest)
		return
	}
	started := time.Now()
	resp, err := s.httpClient.Do(req)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "models": []string{}, "latency_ms": latency, "target": target, "error": "connection_failed"})
		return
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 4<<20)
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		io.Copy(io.Discard, limited)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "models": []string{}, "status": resp.StatusCode, "latency_ms": latency, "target": target})
		return
	}
	models, err := parseProviderModels(limited)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "models": []string{}, "status": resp.StatusCode, "latency_ms": latency, "target": target, "error": "invalid_models_response"})
		return
	}
	sort.Strings(models)
	catalogErr := s.catalog.Ensure(r.Context(), 24*time.Hour)
	modelDetails := s.catalogMatches(input.CatalogProvider, input.ID, input.BaseURL, models)
	w.Header().Set("Content-Type", "application/json")
	result := map[string]any{"ok": true, "models": models, "model_details": modelDetails, "catalog": s.catalog.State(), "status": resp.StatusCode, "latency_ms": latency, "target": target}
	if catalogErr != nil {
		result["catalog_error"] = catalogErr.Error()
	}
	json.NewEncoder(w).Encode(result)
}

func parseProviderModels(r io.Reader) ([]string, error) {
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []string `json:"models"`
	}
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	models := []string{}
	for _, m := range body.Models {
		if m != "" && !seen[m] {
			seen[m] = true
			models = append(models, m)
		}
	}
	for _, item := range body.Data {
		if item.ID != "" && !seen[item.ID] {
			seen[item.ID] = true
			models = append(models, item.ID)
		}
	}
	return models, nil
}

func providerProbeURL(p config.ProviderConfig) string {
	base := strings.TrimRight(p.BaseURL, "/")
	switch p.Type {
	case "openai-compatible":
		return base + "/models"
	case "anthropic":
		return base + "/v1/models"
	default:
		return base
	}
}

func (s *Server) adminLocalConfigure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	snap := s.current()
	if !auth.AuthorizeAdmin(r, snap.AdminToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	if s.cfgPath == "" {
		http.Error(w, "config path is not writable", 400)
		return
	}
	var input config.LocalProviderInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	if input.ID == "" || input.BaseURL == "" {
		http.Error(w, "id and base_url are required", 400)
		return
	}
	next, err := config.UpsertLocalProvider(s.cfgPath, input)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.snapshot.Store(s.buildSnapshot(next))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "loaded_at": s.current().LoadedAt, "issues": config.ValidateRuntime(next)})
}

func (s *Server) adminRecentRequests(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	if !auth.AuthorizeAdmin(r, snap.AdminToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	limit := 50
	if s.recent == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"active": []telemetry.Event{}, "recent": []telemetry.Event{}})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"active": s.recent.Active(), "recent": s.recent.Recent(limit)})
}

func (s *Server) notImplemented(message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adapter := clientopenai.ChatAdapter{}
		_ = adapter.EncodeError(r.Context(), w, ir.GatewayError{StatusCode: 501, Kind: "invalid_request_error", Code: "protocol_not_implemented", Message: message})
	}
}

type recorder struct {
	http.ResponseWriter
	written bool
}

func (r *recorder) WriteHeader(code int) {
	if !r.written {
		r.written = true
		r.ResponseWriter.WriteHeader(code)
	}
}
func (r *recorder) Write(b []byte) (int, error) {
	if !r.written {
		r.written = true
	}
	return r.ResponseWriter.Write(b)
}
func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func recordResponse(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.ServeHTTP(&recorder{ResponseWriter: w}, r) })
}
func headersWritten(w http.ResponseWriter) bool {
	if rr, ok := w.(*recorder); ok {
		return rr.written
	}
	return false
}
func limitBody(next http.Handler, max int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, max)
		next.ServeHTTP(w, r)
	})
}

func errorToIR(err error) ir.GatewayError {
	if e, ok := err.(ir.GatewayError); ok {
		return e
	}
	if e, ok := err.(*ir.GatewayError); ok {
		return *e
	}
	return ir.GatewayError{StatusCode: 500, Kind: "server_error", Code: "internal_error", Message: "Internal server error."}
}
func toIRError(e types.GatewayError) ir.GatewayError {
	return ir.GatewayError{StatusCode: e.StatusCode, Kind: e.Type, Code: e.Code, Message: e.Message, RetryAfter: e.RetryAfter}
}
func toTelemetryUsage(u ir.Usage) types.Usage {
	return types.Usage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, TotalTokens: u.TotalTokens, CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens, CacheHitRatio: u.CacheHitRatio}
}

var _ = upstreamauth.Profile{}
