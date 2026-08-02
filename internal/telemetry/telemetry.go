package telemetry

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/a448582655/vibe-proxy/internal/types"
)

type EventSink interface {
	RequestStarted(Event)
	RequestFinished(Event)
	Token(Event)
}

type Event struct {
	RequestID         string                 `json:"request_id"`
	TraceID           string                 `json:"trace_id"`
	SpanID            string                 `json:"span_id"`
	ParentSpanID      string                 `json:"parent_span_id,omitempty"`
	SessionID         string                 `json:"session_id,omitempty"`
	SessionName       string                 `json:"session_name,omitempty"`
	SessionKind       string                 `json:"session_kind,omitempty"`
	SessionPath       string                 `json:"session_path,omitempty"`
	ParentRequestID   string                 `json:"parent_request_id,omitempty"`
	PrincipalName     string                 `json:"principal_name,omitempty"`
	ClientName        string                 `json:"client_name"`
	AgentID           string                 `json:"agent_id"`
	AgentName         string                 `json:"agent_name,omitempty"`
	AgentVersion      string                 `json:"agent_version,omitempty"`
	AgentSource       string                 `json:"agent_source"`
	AgentConfidence   string                 `json:"agent_confidence"`
	ProjectID         string                 `json:"project_id,omitempty"`
	HTTPMethod        string                 `json:"http_method,omitempty"`
	HTTPPath          string                 `json:"http_path,omitempty"`
	VirtualModel      string                 `json:"virtual_model"`
	InitialProvider   string                 `json:"initial_provider,omitempty"`
	InitialModel      string                 `json:"initial_model,omitempty"`
	UpstreamModel     string                 `json:"upstream_model"`
	ChannelID         string                 `json:"channel_id"`
	ProtocolIn        string                 `json:"protocol_in"`
	ProtocolOut       string                 `json:"protocol_out"`
	StartedAt         time.Time              `json:"started_at"`
	FirstTokenAt      *time.Time             `json:"first_token_at,omitempty"`
	CompletedAt       *time.Time             `json:"completed_at,omitempty"`
	DurationMillis    int64                  `json:"duration_ms"`
	TTFTMillis        int64                  `json:"ttft_ms"`
	TPOTMillis        float64                `json:"tpot_ms"`
	TPS               float64                `json:"tps"`
	StatusCode        int                    `json:"status_code"`
	ErrorCode         string                 `json:"error_code,omitempty"`
	FinishReason      string                 `json:"finish_reason,omitempty"`
	UpstreamRequestID string                 `json:"upstream_request_id,omitempty"`
	RetryCount        int                    `json:"retry_count"`
	Usage             types.Usage            `json:"usage"`
	RequestShape      RequestShapeSummary    `json:"request_shape"`
	InputLabelsJSON   string                 `json:"input_labels_json,omitempty"`
	OutputLabelsJSON  string                 `json:"output_labels_json,omitempty"`
	Transformation    *TransformationSummary `json:"transformation,omitempty"`
}

type RequestShapeSummary struct {
	InputMessageCount    int `json:"input_message_count"`
	InputBlockCount      int `json:"input_block_count"`
	InputToolCount       int `json:"input_tool_count"`
	InputImageCount      int `json:"input_image_count"`
	InputTextChars       int `json:"input_text_chars"`
	OutputMessageCount   int `json:"output_message_count"`
	OutputBlockCount     int `json:"output_block_count"`
	OutputToolCallCount  int `json:"output_tool_call_count"`
	OutputReasoningChars int `json:"output_reasoning_chars"`
	OutputTextChars      int `json:"output_text_chars"`
}

type TransformationSummary struct {
	MultimodalRoute   string   `json:"multimodal_route,omitempty"`
	RouteReason       string   `json:"route_reason,omitempty"`
	ModelImageSupport string   `json:"model_image_support,omitempty"`
	CapabilitySource  string   `json:"capability_source,omitempty"`
	CatalogMatch      string   `json:"catalog_match,omitempty"`
	InputImages       int      `json:"input_images,omitempty"`
	OriginalProvider  string   `json:"original_provider,omitempty"`
	OriginalModel     string   `json:"original_model,omitempty"`
	EffectiveProvider string   `json:"effective_provider,omitempty"`
	EffectiveModel    string   `json:"effective_model,omitempty"`
	OCRProvider       string   `json:"ocr_provider,omitempty"`
	OCRProcessed      int      `json:"ocr_processed,omitempty"`
	OCRCacheHits      int      `json:"ocr_cache_hits,omitempty"`
	OCRLatencyMS      int64    `json:"ocr_latency_ms,omitempty"`
	OCRMinConfidence  *float64 `json:"ocr_min_confidence,omitempty"`
	OCRFailureCode    string   `json:"ocr_failure_code,omitempty"`
}

type Tracker struct {
	mu             sync.Mutex
	Event          Event
	lastTokenAt    time.Time
	outputTokens   int64
	interTokenTime time.Duration
	sink           EventSink
}

func NewTracker(base Event, sink EventSink) *Tracker {
	if base.StartedAt.IsZero() {
		base.StartedAt = time.Now()
	}
	tr := &Tracker{Event: base, lastTokenAt: base.StartedAt, sink: sink}
	if sink != nil {
		sink.RequestStarted(base)
	}
	return tr
}

func (t *Tracker) MarkToken(text string) {
	if text == "" {
		return
	}
	now := time.Now()
	t.mu.Lock()
	if t.Event.FirstTokenAt == nil {
		t.Event.FirstTokenAt = &now
		t.Event.TTFTMillis = now.Sub(t.Event.StartedAt).Milliseconds()
	} else {
		t.interTokenTime += now.Sub(t.lastTokenAt)
	}
	t.lastTokenAt = now
	t.outputTokens++
	t.Event.Usage.CompletionTokens = t.outputTokens
	t.mu.Unlock()
	if t.sink != nil {
		t.sink.Token(t.Snapshot())
	}
}

func (t *Tracker) Finish(status int, usage types.Usage, errCode string) Event {
	now := time.Now()
	t.mu.Lock()
	t.Event.CompletedAt = &now
	t.Event.DurationMillis = now.Sub(t.Event.StartedAt).Milliseconds()
	t.Event.StatusCode = status
	t.Event.ErrorCode = errCode
	if usage.TotalTokens > 0 || usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		t.Event.Usage = usage
	}
	if t.outputTokens > 1 {
		t.Event.TPOTMillis = float64(t.interTokenTime.Milliseconds()) / float64(t.outputTokens-1)
	}
	genDur := now.Sub(t.Event.StartedAt).Seconds()
	if t.Event.FirstTokenAt != nil {
		genDur = now.Sub(*t.Event.FirstTokenAt).Seconds()
	}
	if genDur > 0 {
		t.Event.TPS = float64(t.Event.Usage.CompletionTokens) / genDur
	}
	e := t.Event
	t.mu.Unlock()
	if t.sink != nil {
		t.sink.RequestFinished(e)
	}
	return e
}

func (t *Tracker) Snapshot() Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Event
}

func LabelsJSON(labels map[string]any) string {
	b, _ := json.Marshal(labels)
	return string(b)
}
