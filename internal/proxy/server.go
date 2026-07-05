package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/a448582655/vibe-proxy/internal/adapters"
	"github.com/a448582655/vibe-proxy/internal/auth"
	"github.com/a448582655/vibe-proxy/internal/config"
	vcrypto "github.com/a448582655/vibe-proxy/internal/crypto"
	"github.com/a448582655/vibe-proxy/internal/registry"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
	"github.com/a448582655/vibe-proxy/internal/types"
)

type Server struct {
	cfgPath   string
	registry  *registry.Store
	auth      *auth.Authenticator
	sink      telemetry.EventSink
	metrics   http.Handler
	client    *http.Client
	adapters  map[types.Protocol]adapters.Adapter
	semaphore sync.Map
}

func New(cfgPath string, snap *registry.Snapshot, sink telemetry.EventSink, metrics http.Handler) *Server {
	hc := &http.Client{Timeout: 0}
	anthropic := &adapters.OpenAIToAnthropic{HTTPClient: hc}
	return &Server{cfgPath: cfgPath, registry: registry.NewStore(snap), auth: auth.NewAuthenticator(), sink: sink, metrics: metrics, client: hc, adapters: map[types.Protocol]adapters.Adapter{types.ProtocolAnthropicMessage: anthropic}}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handleOpenAIChat)
	mux.HandleFunc("/v1/responses", s.notYetSupported("OpenAI Responses API is reserved in this skeleton; use /v1/chat/completions for the implemented data plane."))
	mux.HandleFunc("/anthropic/v1/messages", s.notYetSupported("Anthropic client protocol endpoint is reserved in this skeleton; OpenAI client to Anthropic upstream is implemented."))
	mux.Handle("/metrics", s.metrics)
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/admin/config/reload", s.adminReload)
	mux.HandleFunc("/admin/config/snapshot", s.adminSnapshot)
	mux.HandleFunc("/", s.dashboard)
	return withRequestSize(mux, 32<<20)
}

func (s *Server) handleOpenAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, &types.GatewayError{StatusCode: 405, Type: "invalid_request_error", Code: "method_not_allowed", Message: "Method not allowed."})
		return
	}
	snap := s.registry.Current()
	client, gerr := s.auth.AuthenticateDataPlane(r, snap.ClientKeys)
	if gerr != nil {
		writeOpenAIError(w, gerr)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeOpenAIError(w, &types.GatewayError{StatusCode: 400, Type: "invalid_request_error", Code: "body_read_failed", Message: "Could not read request body."})
		return
	}
	defer r.Body.Close()
	var peek struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &peek) != nil || peek.Model == "" {
		writeOpenAIError(w, &types.GatewayError{StatusCode: 400, Type: "invalid_request_error", Code: "invalid_json", Message: "Request JSON must include a model."})
		return
	}
	if !auth.ModelAllowed(client.AllowedModels, peek.Model) {
		writeOpenAIError(w, &types.GatewayError{StatusCode: 403, Type: "permission_error", Code: "model_not_allowed", Message: "This API key is not allowed to use the requested model."})
		return
	}
	route, ok := snap.MatchRoute(peek.Model)
	if !ok || len(route.Channels) == 0 {
		writeOpenAIError(w, &types.GatewayError{StatusCode: 404, Type: "invalid_request_error", Code: "model_not_found", Message: "No route is configured for the requested model."})
		return
	}
	channel, ok := s.selectChannel(snap, route.Channels)
	if !ok {
		writeOpenAIError(w, &types.GatewayError{StatusCode: 503, Type: "server_error", Code: "no_channel_available", Message: "No upstream channel is available."})
		return
	}
	upstreamModel := channel.Models[peek.Model]
	if upstreamModel == "" {
		upstreamModel = peek.Model
	}
	adapter := s.adapters[types.Protocol(channel.Protocol)]
	if adapter == nil {
		writeOpenAIError(w, &types.GatewayError{StatusCode: 502, Type: "server_error", Code: "adapter_not_found", Message: "No protocol adapter is available for the selected channel."})
		return
	}
	apiKey, gerr := resolveProviderKey(channel, snap)
	if gerr != nil {
		writeOpenAIError(w, gerr)
		return
	}
	if !s.acquire(channel.ID, channel.MaxConcurrency) {
		writeOpenAIError(w, &types.GatewayError{StatusCode: 429, Type: "rate_limit_error", Code: "channel_concurrency_exceeded", Message: "Selected upstream channel is busy.", RetryAfter: "1"})
		return
	}
	defer s.release(channel.ID)
	ctx, cancel := context.WithTimeout(r.Context(), channel.Timeout.Duration)
	defer cancel()
	tracker := telemetry.NewTracker(telemetry.Event{RequestID: uuid.NewString(), ClientName: client.Name, VirtualModel: peek.Model, UpstreamModel: upstreamModel, ChannelID: channel.ID, ProtocolIn: string(types.ProtocolOpenAIChat), ProtocolOut: channel.Protocol, InputLabelsJSON: dissectInput(peek.Messages)}, s.sink)
	preq := adapters.ProxyRequest{RawBody: body, ClientHeaders: r.Header.Clone(), VirtualModel: peek.Model, UpstreamModel: upstreamModel, Stream: peek.Stream, Overrides: route.Overrides}
	upReq, gerr := adapter.TransformRequest(ctx, preq, channel, apiKey)
	if gerr != nil {
		tracker.Finish(gerr.StatusCode, types.Usage{}, gerr.Code)
		writeOpenAIError(w, gerr)
		return
	}
	resp, err := s.client.Do(upReq)
	if err != nil {
		g := &types.GatewayError{StatusCode: 502, Type: "server_error", Code: "upstream_connection_failed", Message: "Could not connect to upstream provider."}
		tracker.Finish(g.StatusCode, types.Usage{}, g.Code)
		writeOpenAIError(w, g)
		return
	}
	gerr = adapter.HandleResponse(ctx, resp, w, preq, tracker)
	if gerr != nil {
		tracker.Finish(gerr.StatusCode, types.Usage{}, gerr.Code)
		if !headersSent(w) {
			writeOpenAIError(w, gerr)
		}
	}
}

func (s *Server) selectChannel(snap *registry.Snapshot, ids []string) (config.ChannelConfig, bool) {
	var best config.ChannelConfig
	for _, id := range ids {
		ch, ok := snap.Channels[id]
		if !ok {
			continue
		}
		if best.ID == "" || ch.Weight > best.Weight {
			best = ch
		}
	}
	return best, best.ID != ""
}

func resolveProviderKey(ch config.ChannelConfig, snap *registry.Snapshot) (string, *types.GatewayError) {
	if ch.APIKeyEnv != "" {
		if v := os.Getenv(ch.APIKeyEnv); v != "" {
			return v, nil
		}
	}
	if ch.EncryptedKey != "" {
		master := os.Getenv(snap.MasterKeyEnv)
		plain, err := vcrypto.Decrypt(master, ch.EncryptedKey)
		if err == nil {
			return plain, nil
		}
	}
	return "", &types.GatewayError{StatusCode: 503, Type: "server_error", Code: "provider_key_unavailable", Message: "Selected upstream channel is not configured."}
}

func (s *Server) acquire(id string, max int) bool {
	if max <= 0 {
		max = 32
	}
	v, _ := s.semaphore.LoadOrStore(id, make(chan struct{}, max))
	sem := v.(chan struct{})
	select {
	case sem <- struct{}{}:
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
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "loaded_at": s.registry.Current().LoadedAt})
}

func (s *Server) adminReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	snap := s.registry.Current()
	if !auth.AuthorizeAdmin(r, snap.AdminToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	adminToken := os.Getenv(cfg.Security.AdminBearerTokenEnv)
	next, err := registry.BuildSnapshot(cfg, adminToken)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.registry.Swap(next)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"reloaded": true, "loaded_at": next.LoadedAt})
}

func (s *Server) adminSnapshot(w http.ResponseWriter, r *http.Request) {
	snap := s.registry.Current()
	if !auth.AuthorizeAdmin(r, snap.AdminToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	channels := make([]map[string]any, 0, len(snap.Channels))
	for _, ch := range snap.Channels {
		channels = append(channels, map[string]any{"id": ch.ID, "name": ch.Name, "protocol": ch.Protocol, "base_url": ch.BaseURL, "weight": ch.Weight, "max_concurrency": ch.MaxConcurrency, "region": ch.Region, "has_key": ch.APIKeyEnv != "" || ch.EncryptedKey != ""})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"loaded_at": snap.LoadedAt, "channels": channels, "routes": snap.Routes})
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, `<!doctype html><html><head><title>vibe-proxy</title><style>body{margin:0;background:#08090a;color:#f4f4f5;font-family:Inter,ui-sans-serif,system-ui}.wrap{max-width:1040px;margin:64px auto;padding:32px}.card{border:1px solid #27272a;background:#111113;border-radius:18px;padding:24px;box-shadow:0 20px 80px #0008}.muted{color:#a1a1aa}.grid{display:grid;grid-template-columns:repeat(3,1fr);gap:16px}.kpi{background:#18181b;border:1px solid #27272a;border-radius:14px;padding:18px}</style></head><body><main class="wrap"><section class="card"><p class="muted">vibe-proxy</p><h1>Unified LLM Gateway</h1><p class="muted">Backend data plane is running. Use /v1/chat/completions, /metrics, /healthz, and admin reload APIs.</p><div class="grid"><div class="kpi">Streaming<br><b>SSE transform</b></div><div class="kpi">Telemetry<br><b>TTFT / TPOT / TPS</b></div><div class="kpi">Config<br><b>RCU hot reload</b></div></div></section></main></body></html>`)
}

func (s *Server) notYetSupported(message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeOpenAIError(w, &types.GatewayError{StatusCode: 501, Type: "invalid_request_error", Code: "protocol_not_implemented", Message: message})
	}
}

func dissectInput(messages []struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}) string {
	labels := map[string]any{"system": 0, "user": 0, "assistant": 0, "tool_calls": 0, "tool_results": 0, "images": 0, "files": 0}
	for _, m := range messages {
		if _, ok := labels[m.Role]; ok {
			labels[m.Role] = labels[m.Role].(int) + contentLen(m.Content)
		}
		countMedia(m.Content, labels)
	}
	return telemetry.LabelsJSON(labels)
}

func contentLen(v any) int {
	switch x := v.(type) {
	case string:
		return len(x)
	case []any:
		total := 0
		for _, it := range x {
			total += contentLen(it)
		}
		return total
	case map[string]any:
		if s, ok := x["text"].(string); ok {
			return len(s)
		}
	}
	return 0
}

func countMedia(v any, labels map[string]any) {
	arr, ok := v.([]any)
	if !ok {
		return
	}
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		t, _ := m["type"].(string)
		if strings.Contains(t, "image") {
			labels["images"] = labels["images"].(int) + 1
		}
		if strings.Contains(t, "file") {
			labels["files"] = labels["files"].(int) + 1
		}
	}
}

type responseRecorder struct {
	http.ResponseWriter
	written bool
}

func (r *responseRecorder) WriteHeader(code int) {
	if !r.written {
		r.written = true
		r.ResponseWriter.WriteHeader(code)
	}
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.written {
		r.written = true
	}
	return r.ResponseWriter.Write(b)
}

func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func headersSent(w http.ResponseWriter) bool {
	if rr, ok := w.(*responseRecorder); ok {
		return rr.written
	}
	return false
}

func writeOpenAIError(w http.ResponseWriter, err *types.GatewayError) {
	if err.StatusCode == 0 {
		err.StatusCode = 500
	}
	w.Header().Set("Content-Type", "application/json")
	if err.RetryAfter != "" {
		w.Header().Set("Retry-After", err.RetryAfter)
	}
	w.WriteHeader(err.StatusCode)
	w.Write(adapters.OpenAIErrorJSON(err))
}

func withRequestSize(next http.Handler, max int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, max)
		next.ServeHTTP(&responseRecorder{ResponseWriter: w}, r)
	})
}
