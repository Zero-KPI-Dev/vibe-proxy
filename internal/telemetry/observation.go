package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

type PayloadStage string

const (
	PayloadStageClientRequest     PayloadStage = "client_request"
	PayloadStageCanonicalRequest  PayloadStage = "canonical_request"
	PayloadStageUpstreamRequest   PayloadStage = "upstream_request"
	PayloadStageCanonicalResponse PayloadStage = "canonical_response"
)

type PayloadSnapshot struct {
	RequestID        string       `json:"request_id"`
	Stage            PayloadStage `json:"stage"`
	SchemaVersion    int          `json:"schema_version"`
	CaptureMode      CaptureMode  `json:"capture_mode"`
	CaptureStatus    string       `json:"capture_status"`
	MediaType        string       `json:"media_type,omitempty"`
	ContentEncoding  string       `json:"content_encoding,omitempty"`
	Body             []byte       `json:"body,omitempty"`
	OriginalBytes    int          `json:"original_bytes"`
	StoredBytes      int          `json:"stored_bytes"`
	Truncated        bool         `json:"truncated"`
	TruncationReason string       `json:"truncation_reason,omitempty"`
	RedactionCount   int          `json:"redaction_count"`
	SHA256           string       `json:"sha256,omitempty"`
	Headers          http.Header  `json:"headers,omitempty"`
	Error            string       `json:"error,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
	ExpiresAt        time.Time    `json:"expires_at"`
}

type TraceObservation struct {
	ObservationID  string         `json:"observation_id"`
	RequestID      string         `json:"request_id"`
	TraceID        string         `json:"trace_id"`
	SpanID         string         `json:"span_id"`
	ParentSpanID   string         `json:"parent_span_id,omitempty"`
	Type           string         `json:"type"`
	Name           string         `json:"name"`
	StartedAt      time.Time      `json:"started_at"`
	CompletedAt    *time.Time     `json:"completed_at,omitempty"`
	DurationMillis int64          `json:"duration_ms"`
	Status         string         `json:"status"`
	ErrorCode      string         `json:"error_code,omitempty"`
	Attributes     map[string]any `json:"attributes,omitempty"`
}

func NewObservation(id, requestID, traceID, spanID, observationType, name string, startedAt time.Time) TraceObservation {
	return TraceObservation{
		ObservationID: id,
		RequestID:     requestID,
		TraceID:       traceID,
		SpanID:        spanID,
		Type:          observationType,
		Name:          name,
		StartedAt:     startedAt,
		Status:        "in_progress",
	}
}

func (o *TraceObservation) Finish(completedAt time.Time, status, errorCode string) {
	if o == nil {
		return
	}
	o.CompletedAt = &completedAt
	o.DurationMillis = completedAt.Sub(o.StartedAt).Milliseconds()
	o.Status = status
	o.ErrorCode = errorCode
}

type PayloadRecorder interface {
	RecordPayload(PayloadSnapshot) error
}

type ObservationRecorder interface {
	RecordObservation(TraceObservation) error
}

func RecordPayload(sink EventSink, snapshot PayloadSnapshot) error {
	if recorder, ok := sink.(PayloadRecorder); ok {
		return recorder.RecordPayload(snapshot)
	}
	return nil
}

func RecordObservation(sink EventSink, observation TraceObservation) error {
	if recorder, ok := sink.(ObservationRecorder); ok {
		return recorder.RecordObservation(observation)
	}
	return nil
}

type CaptureBudget struct {
	mu        sync.Mutex
	remaining int
}

func NewCaptureBudget(maxBytes int) *CaptureBudget {
	if maxBytes < 0 {
		maxBytes = 0
	}
	return &CaptureBudget{remaining: maxBytes}
}

func (b *CaptureBudget) Remaining() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remaining
}

func (b *CaptureBudget) Capture(body []byte, headers http.Header, policy CapturePolicy, requestMode string) CaptureResult {
	if b == nil {
		return CapturePayload(body, headers, policy, requestMode)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.remaining <= 0 && resolveCaptureMode(policy.Mode, requestMode) != CaptureModeMetadata && resolveCaptureMode(policy.Mode, requestMode) != CaptureModeOff {
		return CaptureResult{Mode: resolveCaptureMode(policy.Mode, requestMode), Status: CaptureStatusDropped, OriginalBytes: len(body), Error: "request_capture_budget_exhausted"}
	}
	boundedPolicy := policy
	if boundedPolicy.MaxSnapshotBytes <= 0 || boundedPolicy.MaxSnapshotBytes > b.remaining {
		boundedPolicy.MaxSnapshotBytes = b.remaining
	}
	result := CapturePayload(body, headers, boundedPolicy, requestMode)
	b.remaining -= result.StoredBytes
	return result
}

func (b *CaptureBudget) CaptureValue(value any, headers http.Header, policy CapturePolicy, requestMode string) CaptureResult {
	mode := resolveCaptureMode(policy.Mode, requestMode)
	if mode == CaptureModeMetadata || mode == CaptureModeOff {
		return CaptureResult{Mode: mode, Status: CaptureStatusNotCaptured}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return CaptureResult{Mode: resolveCaptureMode(policy.Mode, requestMode), Status: CaptureStatusDropped, Error: "encode_capture_value"}
	}
	return b.Capture(encoded, headers, policy, requestMode)
}

func NewPayloadSnapshot(requestID string, stage PayloadStage, result CaptureResult, mediaType string, createdAt, expiresAt time.Time) PayloadSnapshot {
	return PayloadSnapshot{
		RequestID:        requestID,
		Stage:            stage,
		SchemaVersion:    1,
		CaptureMode:      result.Mode,
		CaptureStatus:    result.Status,
		MediaType:        mediaType,
		Body:             append([]byte(nil), result.Body...),
		OriginalBytes:    result.OriginalBytes,
		StoredBytes:      result.StoredBytes,
		Truncated:        result.Truncated,
		TruncationReason: result.TruncationReason,
		RedactionCount:   result.RedactionCount,
		SHA256:           result.SHA256,
		Headers:          result.Headers.Clone(),
		Error:            result.Error,
		CreatedAt:        createdAt,
		ExpiresAt:        expiresAt,
	}
}

// StreamAccumulator observes Canonical IR events while forwarding them. Raw
// provider frames are deliberately ignored.
type StreamAccumulator struct {
	mu               sync.Mutex
	response         ir.Response
	includeReasoning bool
	text             strings.Builder
	reasoning        strings.Builder
	toolCalls        map[int]ir.ToolCall
}

func NewStreamAccumulator(model string, includeReasoning bool) *StreamAccumulator {
	return &StreamAccumulator{
		response:         ir.Response{Model: model},
		includeReasoning: includeReasoning,
		toolCalls:        map[int]ir.ToolCall{},
	}
}

func (a *StreamAccumulator) Wrap(ctx context.Context, input <-chan ir.StreamEvent) <-chan ir.StreamEvent {
	output := make(chan ir.StreamEvent, 32)
	go func() {
		defer close(output)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-input:
				if !ok {
					return
				}
				a.add(event)
				select {
				case output <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return output
}

func (a *StreamAccumulator) add(event ir.StreamEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if event.MessageID != "" {
		a.response.ID = event.MessageID
	}
	if event.Usage != nil {
		a.response.Usage = *event.Usage
	}
	switch event.Type {
	case ir.EventContentDelta:
		a.text.WriteString(event.Delta.Text)
	case ir.EventReasoningDelta:
		if a.includeReasoning {
			a.reasoning.WriteString(event.Delta.Text)
		}
	case ir.EventToolCallStart, ir.EventToolCallDelta, ir.EventToolCallDone:
		if event.ToolCall != nil {
			call := a.toolCalls[event.Index]
			if event.ToolCall.ID != "" {
				call.ID = event.ToolCall.ID
			}
			if event.ToolCall.Name != "" {
				call.Name = event.ToolCall.Name
			}
			if event.Type == ir.EventToolCallDone {
				call.Arguments = append([]byte(nil), event.ToolCall.Arguments...)
			} else {
				call.Arguments = append(call.Arguments, event.ToolCall.Arguments...)
			}
			a.toolCalls[event.Index] = call
		}
	}
}

func (a *StreamAccumulator) Response() *ir.Response {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	response := a.response
	indexes := make([]int, 0, len(a.toolCalls))
	for index := range a.toolCalls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	content := make([]ir.ContentBlock, 0, len(indexes)+2)
	if a.reasoning.Len() > 0 {
		content = append(content, ir.ContentBlock{Type: ir.ContentReasoning, Text: a.reasoning.String()})
	}
	if a.text.Len() > 0 {
		content = append(content, ir.ContentBlock{Type: ir.ContentText, Text: a.text.String()})
	}
	for _, index := range indexes {
		if call, ok := a.toolCalls[index]; ok {
			copyCall := call
			content = append(content, ir.ContentBlock{Type: ir.ContentToolCall, ToolCall: &copyCall})
		}
	}
	response.Messages = []ir.Message{{Role: ir.RoleAssistant, Content: content}}
	return &response
}
