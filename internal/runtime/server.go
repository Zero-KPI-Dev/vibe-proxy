package runtime

import (
	"bytes"
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

	"github.com/google/uuid"

	"github.com/a448582655/vibe-proxy/internal/auth"
	clientanthropic "github.com/a448582655/vibe-proxy/internal/clientadapters/anthropic"
	clientopenai "github.com/a448582655/vibe-proxy/internal/clientadapters/openai"
	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/desktopbridge"
	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/metrics"
	"github.com/a448582655/vibe-proxy/internal/modelcatalog"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/multimodal"
	"github.com/a448582655/vibe-proxy/internal/ocr"
	"github.com/a448582655/vibe-proxy/internal/preprocess"
	"github.com/a448582655/vibe-proxy/internal/protocol"
	provideranthropic "github.com/a448582655/vibe-proxy/internal/provideradapters/anthropic"
	provideropenai "github.com/a448582655/vibe-proxy/internal/provideradapters/openai"
	"github.com/a448582655/vibe-proxy/internal/streamengine"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
	"github.com/a448582655/vibe-proxy/internal/types"
	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
)

type Snapshot struct {
	LoadedAt         time.Time
	Config           *config.RuntimeConfig
	Resolver         *modelresolver.Resolver
	Preprocessors    *preprocess.Pipeline
	AgentProfiles    []telemetry.AgentProfile
	CapturePolicy    telemetry.CapturePolicy
	ContentRetention time.Duration
	AdminToken       string
}

type Server struct {
	cfgPath            string
	startedAt          time.Time
	snapshot           atomic.Value
	authenticator      *auth.Authenticator
	httpClient         *http.Client
	ocrHTTPClient      *http.Client
	builtinOCR         ocr.Provider
	clientAdapters     []protocol.ClientAdapter
	providerAdapters   map[string]protocol.ProviderAdapter
	metrics            *metrics.Prometheus
	sink               telemetry.EventSink
	recent             *telemetry.RecentStore
	observability      telemetry.ObservabilityReader
	catalog            *modelcatalog.Service
	semaphore          sync.Map
	adminTokenOverride string
	desktopSessions    *auth.DesktopSessionStore
	passwordAuth       *auth.PasswordAuth
	desktopController  desktopbridge.Controller
}

// New builds a runtime server with the default options.
func New(cfgPath string, cfg *config.RuntimeConfig, sink telemetry.EventSink, prom *metrics.Prometheus) *Server {
	return NewWithOptions(cfgPath, cfg, sink, prom, Options{})
}

// NewWithOptions builds a runtime server with optional behavior.
func NewWithOptions(cfgPath string, cfg *config.RuntimeConfig, sink telemetry.EventSink, prom *metrics.Prometheus, options Options) *Server {
	var recent *telemetry.RecentStore
	if r, ok := sink.(*telemetry.RecentStore); ok {
		recent = r
	}
	if ms, ok := sink.(interface{ RecentStore() *telemetry.RecentStore }); ok {
		recent = ms.RecentStore()
	}
	var observability telemetry.ObservabilityReader
	if reader, ok := sink.(telemetry.ObservabilityReader); ok {
		observability = reader
	}
	if provider, ok := sink.(telemetry.ObservabilityReaderProvider); ok {
		observability = provider.ObservabilityReader()
	}
	s := &Server{cfgPath: cfgPath, startedAt: time.Now(), authenticator: auth.NewAuthenticator(), httpClient: &http.Client{Timeout: 0}, ocrHTTPClient: &http.Client{}, builtinOCR: ocr.NewBuiltinProvider(), metrics: prom, sink: sink, recent: recent, observability: observability, catalog: modelcatalog.NewService(modelcatalog.Options{CachePath: modelCatalogCachePath(cfgPath, cfg)}), clientAdapters: []protocol.ClientAdapter{clientopenai.ChatAdapter{}, clientopenai.ResponsesAdapter{}, clientanthropic.MessagesAdapter{}}, providerAdapters: map[string]protocol.ProviderAdapter{"anthropic": provideranthropic.Provider{}, "openai-compatible": provideropenai.Provider{}}, adminTokenOverride: options.AdminTokenOverride, desktopSessions: options.DesktopSessions, passwordAuth: options.PasswordAuth, desktopController: options.DesktopController}
	s.applyRuntimeConfig(cfg)
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

func (s *Server) SetOCRHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	s.ocrHTTPClient = client
	if snap, ok := s.snapshot.Load().(*Snapshot); ok && snap != nil {
		s.snapshot.Store(s.buildSnapshot(snap.Config))
	}
}

func modelCatalogCachePath(cfgPath string, cfg *config.RuntimeConfig) string {
	if cfgPath == "" || cfg == nil || cfg.Storage.SQLitePath == "" || cfg.Storage.SQLitePath == ":memory:" {
		return ""
	}
	return cfg.Storage.SQLitePath + ".models-dev.json"
}

func (s *Server) applyRuntimeConfig(cfg *config.RuntimeConfig) {
	if s.catalog != nil {
		// The URL has already passed configuration validation. Keep catalog
		// networking in sync with raw-config reloads and admin mutations.
		_ = s.catalog.SetProxyURL(cfg.ModelCatalog.ProxyURL)
	}
	s.snapshot.Store(s.buildSnapshot(cfg))
}

func (s *Server) buildSnapshot(cfg *config.RuntimeConfig) *Snapshot {
	resolver := modelresolver.New(cfg.ModelResolver)
	visionProviders := make(map[string]providerRuntime, len(cfg.Providers))
	for id, provider := range cfg.Providers {
		provider := provider
		visionProviders[id] = providerRuntime{
			MaxConcurrency: provider.MaxConcurrency,
			Timeout:        provider.Timeout.Duration,
			Auth:           provider.Auth.Apply,
		}
	}
	processor := multimodal.NewProcessor(multimodal.ProcessorOptions{
		Config:         cfg.Multimodal,
		Catalog:        s.catalog,
		Client:         s.ocrHTTPClient,
		BuiltinOCR:     s.builtinOCR,
		Resolver:       resolver,
		Providers:      cfg.Providers,
		VisionAnalyzer: newRuntimeVisionAnalyzer(s, visionProviders),
		AdapterCapabilities: func(providerType string) (protocol.Capabilities, bool) {
			adapter, ok := s.providerAdapters[providerType]
			if !ok {
				return protocol.Capabilities{}, false
			}
			return adapter.Capabilities(), true
		},
	})
	adminToken := s.adminTokenOverride
	if adminToken == "" {
		adminToken = os.Getenv(cfg.Security.AdminBearerTokenEnv)
	}
	return &Snapshot{
		LoadedAt:      time.Now(),
		Config:        cfg,
		Resolver:      resolver,
		Preprocessors: preprocess.New(processor),
		AgentProfiles: compileTelemetryAgentProfiles(cfg.AgentProfiles),
		CapturePolicy: telemetry.CapturePolicy{
			Mode:             telemetry.CaptureMode(cfg.Observability.Capture.Mode),
			MaxSnapshotBytes: cfg.Observability.Capture.MaxSnapshotBytes,
			CaptureResponse:  cfg.Observability.Capture.CaptureResponse,
			CaptureReasoning: cfg.Observability.Capture.CaptureReasoning,
			ImagePayloads:    cfg.Observability.Capture.ImagePayloads,
			HeaderAllowlist:  append([]string(nil), cfg.Observability.Capture.HeaderAllowlist...),
		},
		ContentRetention: time.Duration(cfg.Observability.Retention.ContentDays) * 24 * time.Hour,
		AdminToken:       adminToken,
	}
}

func compileTelemetryAgentProfiles(profiles []config.AgentProfileConfig) []telemetry.AgentProfile {
	compiled := make([]telemetry.AgentProfile, 0, len(profiles))
	for _, profile := range profiles {
		compiled = append(compiled, telemetry.AgentProfile{ID: profile.ID, Name: profile.Name, Detect: profile.Detect})
	}
	return compiled
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
	mux.HandleFunc("/auth/status", s.authStatus)
	mux.HandleFunc("/auth/setup", s.authSetup)
	mux.HandleFunc("/auth/login", s.authLogin)
	mux.HandleFunc("/auth/logout", s.authLogout)

	// Admin API endpoints
	mux.HandleFunc("/admin/playground/v1/chat/completions", s.adminPlayground("/v1/chat/completions"))
	mux.HandleFunc("/admin/playground/v1/responses", s.adminPlayground("/v1/responses"))
	mux.HandleFunc("/admin/playground/anthropic/v1/messages", s.adminPlayground("/anthropic/v1/messages"))
	mux.HandleFunc("/admin/config/reload", s.reload)
	mux.HandleFunc("/admin/desktop", s.desktopSnapshot)
	mux.HandleFunc("/admin/desktop/preferences", s.desktopPreferences)
	mux.HandleFunc("/admin/desktop/open-data-dir", s.desktopOpenDataDir)
	mux.HandleFunc("/admin/desktop/import-config", s.desktopImportConfig)
	mux.HandleFunc("/admin/config/snapshot", s.adminSnapshot)
	mux.HandleFunc("/admin/config/validate", s.adminValidateConfig)
	mux.HandleFunc("/admin/config/raw", s.adminRawConfig)
	mux.HandleFunc("/admin/providers/test", s.adminProviderTest)
	mux.HandleFunc("/admin/providers/models", s.adminProviderModels)
	mux.HandleFunc("/admin/providers/health", s.adminProviderHealth)
	mux.HandleFunc("/admin/model-catalog/status", s.adminModelCatalogStatus)
	mux.HandleFunc("/admin/model-catalog/settings", s.adminModelCatalogSettings)
	mux.HandleFunc("/admin/model-catalog/refresh", s.adminModelCatalogRefresh)
	mux.HandleFunc("/admin/model-catalog/import", s.adminModelCatalogImport)
	mux.HandleFunc("/admin/model-catalog/lookup", s.adminModelCatalogLookup)
	mux.HandleFunc("/admin/multimodal", s.adminMultimodal)
	mux.HandleFunc("/admin/multimodal/ocr/test", s.adminOCRTest)
	mux.HandleFunc("/admin/providers", s.adminProviders) // GET (list), POST (create), PUT (update), DELETE (delete)
	mux.HandleFunc("/admin/local/configure", s.adminLocalConfigure)
	mux.HandleFunc("/admin/aliases/default", s.adminAliasesDefaults)
	mux.HandleFunc("/admin/aliases", s.adminAliases)               // GET (list), POST (create)
	mux.HandleFunc("/admin/aliases/", s.adminAliasesDelete)        // DELETE by alias name
	mux.HandleFunc("/admin/client-keys", s.adminClientKeys)        // GET (list), POST (create)
	mux.HandleFunc("/admin/client-keys/", s.adminClientKeysByName) // PUT (update), DELETE (delete)
	mux.HandleFunc("/admin/requests/recent", s.adminRecentRequests)
	mux.HandleFunc("/admin/observability/requests", s.adminObservabilityRequests)
	mux.HandleFunc("/admin/observability/requests/", s.adminObservabilityRequest)
	mux.HandleFunc("/admin/observability/sessions", s.adminObservabilitySessions)
	mux.HandleFunc("/admin/observability/sessions/", s.adminObservabilitySession)
	mux.HandleFunc("/admin/metrics/summary", s.adminMetricsSummary)
	mux.HandleFunc("/admin/metrics/history", s.adminMetricsHistory)

	if s.desktopSessions != nil {
		mux.HandleFunc(desktopBootstrapPrefix, s.desktopBootstrap)
	}

	// SPA dashboard (must be last as catch-all)
	mux.Handle("/", s.spaRoutes())
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
	aliases := make([]string, 0, len(snap.Config.ModelResolver.Aliases))
	for alias := range snap.Config.ModelResolver.Aliases {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		if alias == "" || seen[alias] {
			continue
		}
		seen[alias] = true
		data = append(data, map[string]any{"id": alias, "object": "model", "created": now, "owned_by": "vibe-proxy", "vibe_type": "alias"})
	}
	providerIDs := make([]string, 0, len(snap.Config.Providers))
	for providerID := range snap.Config.Providers {
		providerIDs = append(providerIDs, providerID)
	}
	sort.Strings(providerIDs)
	for _, providerID := range providerIDs {
		provider := snap.Config.Providers[providerID]
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
	s.handleWithClient(w, r, nil)
}

func (s *Server) handleWithClient(w http.ResponseWriter, r *http.Request, trustedClient *auth.Client) {
	snap := s.current()
	clientAdapter := s.detectClientAdapter(r)
	if clientAdapter == nil {
		http.NotFound(w, r)
		return
	}
	requestID := uuid.NewString()
	identity := telemetry.ExtractRequestIdentity(r, nil, snap.AgentProfiles)
	tracker := telemetry.NewTracker(telemetry.Event{
		RequestID:       requestID,
		TraceID:         identity.TraceID,
		SpanID:          identity.SpanID,
		ParentSpanID:    identity.ParentSpanID,
		SessionID:       identity.SessionID,
		SessionName:     identity.SessionName,
		SessionKind:     identity.SessionKind,
		SessionPath:     identity.SessionPath,
		ParentRequestID: identity.ParentRequestID,
		AgentID:         identity.AgentID,
		AgentName:       identity.AgentName,
		AgentVersion:    identity.AgentVersion,
		AgentSource:     identity.AgentSource,
		AgentConfidence: identity.AgentConfidence,
		ProjectID:       identity.ProjectID,
		HTTPMethod:      r.Method,
		HTTPPath:        r.URL.Path,
		ProtocolIn:      string(clientAdapter.Protocol()),
		StartedAt:       time.Now(),
	}, s.sink)
	captureOverride := r.Header.Get("X-Vibe-Capture")
	effectiveCaptureMode := telemetry.EffectiveCaptureMode(snap.CapturePolicy.Mode, captureOverride)
	tracker.Event.CaptureMode = string(effectiveCaptureMode)
	tracker.Event.CaptureStatus = telemetry.CaptureStatusNotCaptured
	captureBudget := telemetry.NewCaptureBudget(snap.CapturePolicy.MaxSnapshotBytes)
	recordCaptureResult := func(stage telemetry.PayloadStage, result telemetry.CaptureResult, mediaType string) {
		mergeCaptureResult(&tracker.Event, result)
		if result.Mode != telemetry.CaptureModeStructured && result.Mode != telemetry.CaptureModeRaw {
			return
		}
		now := time.Now().UTC()
		snapshot := telemetry.NewPayloadSnapshot(requestID, stage, result, mediaType, now, now.Add(snap.ContentRetention))
		if err := telemetry.RecordPayload(s.sink, snapshot); err != nil {
			tracker.Event.CaptureStatus = telemetry.CaptureStatusDropped
		}
	}
	recordCaptureBytes := func(stage telemetry.PayloadStage, body []byte, headers http.Header, mediaType string) {
		result := captureBudget.Capture(body, headers, snap.CapturePolicy, captureOverride)
		recordCaptureResult(stage, result, mediaType)
	}
	recordCaptureValue := func(stage telemetry.PayloadStage, value any) {
		result := captureBudget.CaptureValue(value, nil, snap.CapturePolicy, captureOverride)
		recordCaptureResult(stage, result, "application/json")
	}
	w.Header().Set("X-Vibe-Proxy-Request-ID", requestID)
	w.Header().Set("X-Vibe-Proxy-Trace-ID", identity.TraceID)
	finishError := func(gatewayError ir.GatewayError) {
		tracker.Finish(gatewayError.StatusCode, types.Usage{}, gatewayError.Code)
		_ = clientAdapter.EncodeError(r.Context(), w, gatewayError)
	}
	if r.Method != http.MethodPost {
		finishError(ir.GatewayError{StatusCode: 405, Kind: "invalid_request_error", Code: "method_not_allowed", Message: "Method not allowed."})
		return
	}
	var client auth.Client
	if trustedClient != nil {
		client = *trustedClient
	} else {
		var gerr *types.GatewayError
		client, gerr = s.authenticator.AuthenticateDataPlane(r, snap.Config.ClientKeys)
		if gerr != nil {
			finishError(toIRError(*gerr))
			return
		}
	}
	tracker.Event.PrincipalName = client.Name
	tracker.Event.ClientName = client.Name
	if effectiveCaptureMode == telemetry.CaptureModeStructured || effectiveCaptureMode == telemetry.CaptureModeRaw {
		body, readErr := io.ReadAll(io.LimitReader(r.Body, (32<<20)+1))
		if readErr != nil {
			finishError(ir.GatewayError{StatusCode: 400, Kind: "invalid_request_error", Code: "body_read_failed", Message: "Could not read request body."})
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		recordCaptureBytes(telemetry.PayloadStageClientRequest, body, r.Header, r.Header.Get("Content-Type"))
	}
	creq, err := clientAdapter.ParseRequest(r.Context(), r)
	if err != nil {
		finishError(errorToIR(err))
		return
	}
	creq.ID = requestID
	identity = telemetry.EnrichRequestIdentity(identity, creq.Metadata)
	applyRequestIdentity(&tracker.Event, identity)
	creq.AgentID = identity.AgentID
	creq.AgentName = identity.AgentName
	creq.AgentVersion = identity.AgentVersion
	creq.ProjectID = identity.ProjectID
	creq.SessionID = identity.SessionID
	tracker.Event.VirtualModel = creq.RequestedModel
	tracker.Event.RequestShape = telemetry.SummarizeRequestShape(creq)
	recordCaptureValue(telemetry.PayloadStageCanonicalRequest, creq)
	if !auth.ModelAllowed(client.AllowedModels, creq.RequestedModel) {
		finishError(ir.GatewayError{StatusCode: 403, Kind: "permission_error", Code: "model_not_allowed", Message: "This API key is not allowed to use the requested model."})
		return
	}
	target, ierr := snap.Resolver.Resolve(creq)
	if ierr != nil {
		finishError(*ierr)
		return
	}
	creq.ResolvedProvider = target.ProviderID
	creq.ResolvedModel = target.Model
	tracker.Event.InitialProvider = target.ProviderID
	tracker.Event.InitialModel = target.Model
	tracker.Event.UpstreamModel = target.Model
	tracker.Event.ChannelID = target.ProviderID
	providerCfg, ok := snap.Config.Providers[target.ProviderID]
	if !ok {
		finishError(ir.GatewayError{StatusCode: 503, Kind: "config_error", Code: "provider_not_found", Message: "Resolved provider is not configured."})
		return
	}
	providerAdapter := s.providerAdapters[target.ProviderType]
	if providerAdapter == nil {
		finishError(ir.GatewayError{StatusCode: 501, Kind: "config_error", Code: "provider_adapter_not_found", Message: "Provider adapter is not available."})
		return
	}
	tracker.Event.ProtocolOut = string(providerAdapter.Protocol())
	originalTarget := target
	prepared, err := snap.Preprocessors.Prepare(r.Context(), creq, preprocess.RouteContext{
		Target:              target,
		ProviderConfig:      providerCfg,
		AdapterCapabilities: providerAdapter.Capabilities(),
	})
	for _, diagnostic := range prepared.Diagnostics {
		recordCaptureValue(telemetry.PayloadStage(diagnostic.Stage), diagnostic.Value)
	}
	for _, item := range prepared.Observations {
		observation := telemetry.NewObservation(uuid.NewString(), requestID, identity.TraceID, identity.SpanID, item.Type, item.Name, item.StartedAt)
		observation.Attributes = item.Attributes
		observation.Finish(item.CompletedAt, item.Status, item.ErrorCode)
		_ = telemetry.RecordObservation(s.sink, observation)
	}
	if err != nil {
		ge := errorToIR(err)
		tracker.Event.Transformation = transformationSummary(prepared.Decisions, originalTarget, prepared.Target)
		tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
		_ = clientAdapter.EncodeError(r.Context(), w, ge)
		return
	}
	creq = prepared.Request
	recordCaptureValue(telemetry.PayloadStageEffectiveCanonicalRequest, creq)
	if prepared.Target.ProviderID != target.ProviderID || prepared.Target.Model != target.Model {
		target = prepared.Target
		creq.ResolvedProvider = target.ProviderID
		creq.ResolvedModel = target.Model
		providerCfg, ok = snap.Config.Providers[target.ProviderID]
		if !ok {
			ge := ir.GatewayError{StatusCode: 503, Kind: "config_error", Code: "provider_not_found", Message: "Preprocessor target provider is not configured."}
			tracker.Event.Transformation = transformationSummary(prepared.Decisions, originalTarget, target)
			tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
			_ = clientAdapter.EncodeError(r.Context(), w, ge)
			return
		}
		providerAdapter = s.providerAdapters[target.ProviderType]
		if providerAdapter == nil {
			ge := ir.GatewayError{StatusCode: 501, Kind: "config_error", Code: "provider_adapter_not_found", Message: "Preprocessor target adapter is not available."}
			tracker.Event.Transformation = transformationSummary(prepared.Decisions, originalTarget, target)
			tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
			_ = clientAdapter.EncodeError(r.Context(), w, ge)
			return
		}
	}
	tracker.Event.UpstreamModel = target.Model
	tracker.Event.ChannelID = target.ProviderID
	tracker.Event.ProtocolOut = string(providerAdapter.Protocol())
	tracker.Event.Transformation = transformationSummary(prepared.Decisions, originalTarget, target)
	applyTransformationHeaders(w, tracker.Event.Transformation)
	if !s.acquire(target.ProviderID, providerCfg.MaxConcurrency) {
		ge := ir.GatewayError{StatusCode: 429, Kind: "rate_limit_error", Code: "provider_busy", Message: "Selected provider is busy.", RetryAfter: "1"}
		tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
		_ = clientAdapter.EncodeError(r.Context(), w, ge)
		return
	}
	defer s.release(target.ProviderID)
	ctx, cancel := context.WithTimeout(r.Context(), providerCfg.Timeout.Duration)
	defer cancel()
	upReq, err := providerAdapter.BuildRequest(ctx, creq, target)
	if err != nil {
		ge := errorToIR(err)
		tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
		_ = clientAdapter.EncodeError(ctx, w, ge)
		return
	}
	if effectiveCaptureMode == telemetry.CaptureModeStructured || effectiveCaptureMode == telemetry.CaptureModeRaw {
		if upstreamBody, bodyErr := requestBodyCopy(upReq); bodyErr == nil {
			recordCaptureBytes(telemetry.PayloadStageUpstreamRequest, upstreamBody, upReq.Header, upReq.Header.Get("Content-Type"))
		}
	}
	if authErr := providerCfg.Auth.Apply(upReq); authErr != nil {
		tracker.Finish(authErr.StatusCode, types.Usage{}, authErr.Code)
		_ = clientAdapter.EncodeError(ctx, w, *authErr)
		return
	}
	upstreamObservation := telemetry.NewObservation(uuid.NewString(), requestID, identity.TraceID, identity.SpanID, "upstream", "provider.http", time.Now().UTC())
	upstreamObservation.Attributes = map[string]any{"provider": target.ProviderID, "model": target.Model, "protocol": providerAdapter.Protocol()}
	observationFinished := false
	finishUpstreamObservation := func(status, errorCode string) {
		if observationFinished {
			return
		}
		observationFinished = true
		upstreamObservation.Finish(time.Now().UTC(), status, errorCode)
		_ = telemetry.RecordObservation(s.sink, upstreamObservation)
	}
	resp, err := s.httpClient.Do(upReq)
	if err != nil {
		ge := ir.GatewayError{StatusCode: 502, Kind: "upstream_error", Code: "upstream_connection_failed", Message: "Could not connect to upstream provider."}
		finishUpstreamObservation("error", ge.Code)
		tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
		_ = clientAdapter.EncodeError(ctx, w, ge)
		return
	}
	if creq.Stream {
		events, err := providerAdapter.ParseStream(ctx, resp)
		if err != nil {
			ge := errorToIR(err)
			finishUpstreamObservation("error", ge.Code)
			tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
			_ = clientAdapter.EncodeError(ctx, w, ge)
			return
		}
		var streamStats streamengine.Stats
		var streamStatsMu sync.Mutex
		tracked := streamengine.Track(ctx, events, func(stats streamengine.Stats) {
			streamStatsMu.Lock()
			streamStats = stats
			streamStatsMu.Unlock()
		})
		streamAccumulator := telemetry.NewStreamAccumulator(target.Model, snap.CapturePolicy.CaptureReasoning)
		canonicalEvents := streamAccumulator.Wrap(ctx, tracked)
		if err := clientAdapter.EncodeStream(ctx, w, canonicalEvents); err != nil {
			ge := errorToIR(err)
			finishUpstreamObservation("error", ge.Code)
			tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
			if !headersWritten(w) {
				_ = clientAdapter.EncodeError(ctx, w, ge)
			}
			return
		}
		streamStatsMu.Lock()
		finalStreamStats := streamStats
		streamStatsMu.Unlock()
		canonicalResponse := streamAccumulator.Response()
		tracker.Event.RequestShape = telemetry.MergeResponseShape(tracker.Event.RequestShape, telemetry.SummarizeResponseShape(canonicalResponse))
		if snap.CapturePolicy.CaptureResponse {
			recordCaptureValue(telemetry.PayloadStageCanonicalResponse, canonicalResponse)
		}
		tracker.Event.Usage = toTelemetryUsage(finalStreamStats.Usage)
		if !finalStreamStats.FirstTokenAt.IsZero() {
			firstTokenAt := finalStreamStats.FirstTokenAt
			tracker.Event.FirstTokenAt = &firstTokenAt
			tracker.Event.TTFTMillis = firstTokenAt.Sub(tracker.Event.StartedAt).Milliseconds()
		}
		if finalStreamStats.OutputTokenCount > 1 && !finalStreamStats.FirstTokenAt.IsZero() && !finalStreamStats.CompletedAt.IsZero() {
			tracker.Event.TPOTMillis = float64(finalStreamStats.CompletedAt.Sub(finalStreamStats.FirstTokenAt).Milliseconds()) / float64(finalStreamStats.OutputTokenCount-1)
		}
		finishUpstreamObservation("ok", "")
		tracker.Finish(http.StatusOK, toTelemetryUsage(finalStreamStats.Usage), "")
		return
	}
	out, err := providerAdapter.ParseUnary(ctx, resp)
	if err != nil {
		ge := errorToIR(err)
		finishUpstreamObservation("error", ge.Code)
		tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
		_ = clientAdapter.EncodeError(ctx, w, ge)
		return
	}
	if out.Model == "" {
		out.Model = creq.RequestedModel
	}
	tracker.Event.UpstreamRequestID = out.ID
	tracker.Event.FinishReason = out.StopReason
	tracker.Event.RequestShape = telemetry.MergeResponseShape(tracker.Event.RequestShape, telemetry.SummarizeResponseShape(out))
	if snap.CapturePolicy.CaptureResponse {
		recordCaptureValue(telemetry.PayloadStageCanonicalResponse, out)
	}
	if err := clientAdapter.EncodeUnary(ctx, w, out); err != nil {
		ge := errorToIR(err)
		finishUpstreamObservation("error", ge.Code)
		tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
		return
	}
	finishUpstreamObservation("ok", "")
	tracker.Finish(http.StatusOK, toTelemetryUsage(out.Usage), "")
}

func requestBodyCopy(request *http.Request) ([]byte, error) {
	if request == nil || request.Body == nil {
		return nil, nil
	}
	if request.GetBody != nil {
		body, err := request.GetBody()
		if err != nil {
			return nil, err
		}
		defer body.Close()
		return io.ReadAll(io.LimitReader(body, (32<<20)+1))
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, (32<<20)+1))
	if err != nil {
		return nil, err
	}
	_ = request.Body.Close()
	request.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func mergeCaptureResult(event *telemetry.Event, result telemetry.CaptureResult) {
	if event == nil {
		return
	}
	event.CaptureMode = string(result.Mode)
	event.CaptureTruncated = event.CaptureTruncated || result.Truncated
	event.RedactionCount += result.RedactionCount
	priority := map[string]int{
		telemetry.CaptureStatusNotCaptured: 1,
		telemetry.CaptureStatusCaptured:    2,
		telemetry.CaptureStatusRedacted:    3,
		telemetry.CaptureStatusTruncated:   4,
		telemetry.CaptureStatusDropped:     5,
	}
	if priority[result.Status] >= priority[event.CaptureStatus] {
		event.CaptureStatus = result.Status
	}
}

func applyRequestIdentity(event *telemetry.Event, identity telemetry.RequestIdentity) {
	event.TraceID = identity.TraceID
	event.SpanID = identity.SpanID
	event.ParentSpanID = identity.ParentSpanID
	event.SessionID = identity.SessionID
	event.SessionName = identity.SessionName
	event.SessionKind = identity.SessionKind
	event.SessionPath = identity.SessionPath
	event.ParentRequestID = identity.ParentRequestID
	event.AgentID = identity.AgentID
	event.AgentName = identity.AgentName
	event.AgentVersion = identity.AgentVersion
	event.AgentSource = identity.AgentSource
	event.AgentConfidence = identity.AgentConfidence
	event.ProjectID = identity.ProjectID
}

func (s *Server) adminPlayground(targetPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.adminAuthorize(w, r) {
			return
		}
		clone := r.Clone(r.Context())
		clonedURL := *r.URL
		clonedURL.Path = targetPath
		clone.URL = &clonedURL
		client := auth.Client{Name: "admin-playground", AllowedModels: []string{"*"}}
		s.handleWithClient(w, clone, &client)
	}
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
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "started_at": s.startedAt, "loaded_at": s.current().LoadedAt})
}
func (s *Server) reload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	if !s.reloadRuntimeConfig(w) {
		return
	}
	s.writeJSON(w, map[string]any{"reloaded": true, "loaded_at": s.current().LoadedAt})
}

// reloadRuntimeConfig loads, validates, and atomically replaces the runtime snapshot.
func (s *Server) reloadRuntimeConfig(w http.ResponseWriter) bool {
	cfg, err := config.LoadRuntime(s.cfgPath)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return false
	}
	if issues := config.ValidateRuntime(cfg); config.HasErrors(issues) {
		s.writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid configuration", "issues": issues})
		return false
	}
	s.applyRuntimeConfig(cfg)
	return true
}
func (s *Server) adminSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorize(w, r) {
		return
	}
	snap := s.current()
	providers := []map[string]any{}
	providerIDs := make([]string, 0, len(snap.Config.Providers))
	for id := range snap.Config.Providers {
		providerIDs = append(providerIDs, id)
	}
	sort.Strings(providerIDs)
	for _, id := range providerIDs {
		p := snap.Config.Providers[id]
		authType, keySource, keyEnv := providerAuthMeta(p)
		providers = append(providers, map[string]any{"id": id, "type": p.Type, "base_url": p.BaseURL, "catalog_provider": p.CatalogProvider, "default_capabilities": p.DefaultCapabilities, "model_capabilities": p.ModelCapabilities, "models": p.Models, "max_concurrency": p.MaxConcurrency, "auth_type": authType, "api_key_source": keySource, "api_key_env": keyEnv})
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
	if !s.adminAuthorize(w, r) {
		return
	}
	snap := s.current()
	issues := config.ValidateRuntime(snap.Config)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"valid": !config.HasErrors(issues), "issues": issues})
}

func (s *Server) adminProviderTest(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorize(w, r) {
		return
	}
	snap := s.current()
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
	modelListing := "supported"
	probeOK := resp.StatusCode >= 200 && resp.StatusCode < 400
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		// A reachable provider may not expose a model-listing endpoint. This is
		// not, by itself, evidence that its configured chat endpoint is unusable.
		probeOK = true
		modelListing = "unsupported"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": probeOK, "reachable": true, "model_listing": modelListing, "provider": providerID, "status": resp.StatusCode, "latency_ms": latency, "target": testURL})
}

func (s *Server) adminProviderModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
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
	if !s.adminAuthorize(w, r) {
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
	s.applyRuntimeConfig(next)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "loaded_at": s.current().LoadedAt, "issues": config.ValidateRuntime(next)})
}

func (s *Server) adminRecentRequests(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorize(w, r) {
		return
	}
	limit := 50
	active := []telemetry.Event{}
	recent := []telemetry.Event{}
	if s.recent != nil {
		active = s.recent.Active()
		recent = s.recent.Recent(limit)
	}
	if s.observability != nil && len(recent) < limit {
		persisted, err := s.observability.RecentFinished(limit)
		if err == nil {
			seen := make(map[string]struct{}, len(recent))
			for _, event := range recent {
				seen[event.RequestID] = struct{}{}
			}
			for _, event := range persisted {
				if len(recent) >= limit {
					break
				}
				if _, exists := seen[event.RequestID]; exists {
					continue
				}
				recent = append(recent, event)
				seen[event.RequestID] = struct{}{}
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"active": active, "recent": recent})
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
