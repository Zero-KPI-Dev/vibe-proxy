package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/a448582655/vibe-proxy/internal/auth"
	clientanthropic "github.com/a448582655/vibe-proxy/internal/clientadapters/anthropic"
	clientopenai "github.com/a448582655/vibe-proxy/internal/clientadapters/openai"
	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/metrics"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/protocol"
	provideranthropic "github.com/a448582655/vibe-proxy/internal/provideradapters/anthropic"
	provideropenai "github.com/a448582655/vibe-proxy/internal/provideradapters/openai"
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
	semaphore        sync.Map
}

func New(cfgPath string, cfg *config.RuntimeConfig, sink telemetry.EventSink, prom *metrics.Prometheus) *Server {
	s := &Server{cfgPath: cfgPath, authenticator: auth.NewAuthenticator(), httpClient: &http.Client{Timeout: 0}, metrics: prom, sink: sink, clientAdapters: []protocol.ClientAdapter{clientopenai.ChatAdapter{}, clientopenai.ResponsesAdapter{}, clientanthropic.MessagesAdapter{}}, providerAdapters: map[string]protocol.ProviderAdapter{"anthropic": provideranthropic.Provider{}, "openai-compatible": provideropenai.Provider{}}}
	s.snapshot.Store(s.buildSnapshot(cfg))
	return s
}

func (s *Server) SetHTTPClient(client *http.Client) {
	if client != nil {
		s.httpClient = client
	}
}

func (s *Server) buildSnapshot(cfg *config.RuntimeConfig) *Snapshot {
	return &Snapshot{LoadedAt: time.Now(), Config: cfg, Resolver: modelresolver.New(cfg.ModelResolver), AdminToken: os.Getenv(cfg.Security.AdminBearerTokenEnv)}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handle)
	mux.HandleFunc("/v1/responses", s.handle)
	mux.HandleFunc("/anthropic/v1/messages", s.handle)
	mux.HandleFunc("/v1/messages", s.handle)
	mux.Handle("/metrics", s.metrics.Handler())
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/admin/config/reload", s.reload)
	mux.HandleFunc("/admin/config/snapshot", s.adminSnapshot)
	mux.HandleFunc("/", s.dashboard)
	return limitBody(recordResponse(mux), 32<<20)
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
		tracked := s.trackStream(events, tracker)
		if err := clientAdapter.EncodeStream(ctx, w, tracked); err != nil {
			ge := errorToIR(err)
			tracker.Finish(ge.StatusCode, types.Usage{}, ge.Code)
			if !headersWritten(w) {
				_ = clientAdapter.EncodeError(ctx, w, ge)
			}
			return
		}
		tracker.Finish(http.StatusOK, trackerUsage(tracker), "")
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

func (s *Server) trackStream(in <-chan ir.StreamEvent, tracker *telemetry.Tracker) <-chan ir.StreamEvent {
	out := make(chan ir.StreamEvent, 32)
	go func() {
		defer close(out)
		usage := ir.Usage{}
		for ev := range in {
			if ev.Type == ir.EventContentDelta || ev.Type == ir.EventReasoningDelta {
				tracker.MarkToken(ev.Delta.Text)
			}
			if ev.Usage != nil {
				usage = *ev.Usage
				tracker.Event.Usage = toTelemetryUsage(usage)
			}
			out <- ev
		}
	}()
	return out
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
		providers = append(providers, map[string]any{"id": id, "type": p.Type, "base_url": p.BaseURL, "models": p.Models, "max_concurrency": p.MaxConcurrency})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"loaded_at": snap.LoadedAt, "providers": providers, "model_resolver": snap.Config.ModelResolver})
}
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, `<!doctype html><html><head><title>vibe-proxy</title><style>body{margin:0;background:#08090a;color:#f4f4f5;font-family:Inter,ui-sans-serif,system-ui}.wrap{max-width:1040px;margin:64px auto;padding:32px}.card{border:1px solid #27272a;background:#111113;border-radius:18px;padding:24px}.muted{color:#a1a1aa}</style></head><body><main class="wrap"><section class="card"><p class="muted">vibe-proxy local</p><h1>Agent-first LLM protocol switcher</h1><p class="muted">Use /v1/chat/completions, /v1/responses, or /anthropic/v1/messages.</p></section></main></body></html>`)
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
func trackerUsage(t *telemetry.Tracker) types.Usage { return t.Snapshot().Usage }

var _ = upstreamauth.Profile{}
