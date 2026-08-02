package telemetry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

func TestPayloadCaptureBudgetIsSharedAcrossStages(t *testing.T) {
	budget := NewCaptureBudget(180)
	policy := CapturePolicy{Mode: CaptureModeStructured, MaxSnapshotBytes: 1024}
	first := budget.Capture([]byte(`{"prompt":"`+strings.Repeat("a", 300)+`"}`), nil, policy, "")
	second := budget.Capture([]byte(`{"response":"hello"}`), nil, policy, "")
	if first.StoredBytes > 180 || budget.Remaining() < 0 {
		t.Fatalf("first capture exceeded budget: result=%+v remaining=%d", first, budget.Remaining())
	}
	if second.Status != CaptureStatusDropped || second.Error != "request_capture_budget_exhausted" {
		t.Fatalf("second capture should be payload-first dropped: %+v", second)
	}
}

func TestStreamAccumulatorBuildsCanonicalResponseWithoutRawFrames(t *testing.T) {
	input := make(chan ir.StreamEvent, 4)
	input <- ir.StreamEvent{Type: ir.EventMessageStart, MessageID: "message-1", Raw: []byte("data: secret-frame")}
	input <- ir.StreamEvent{Type: ir.EventContentDelta, Index: 0, Delta: ir.ContentBlock{Type: ir.ContentText, Text: "hello"}, Raw: []byte("data: hello")}
	input <- ir.StreamEvent{Type: ir.EventReasoningDelta, Index: 1, Delta: ir.ContentBlock{Type: ir.ContentReasoning, Text: "private chain"}}
	input <- ir.StreamEvent{Type: ir.EventMessageDone, Usage: &ir.Usage{CompletionTokens: 1}}
	close(input)

	accumulator := NewStreamAccumulator("model-a", false)
	for range accumulator.Wrap(context.Background(), input) {
	}
	response := accumulator.Response()
	if response.ID != "message-1" || response.Model != "model-a" || len(response.Messages) != 1 {
		t.Fatalf("unexpected canonical response: %+v", response)
	}
	encodedBytes, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(encodedBytes)
	if !strings.Contains(encoded, "hello") || strings.Contains(encoded, "secret-frame") || strings.Contains(encoded, "private chain") {
		t.Fatalf("stream accumulator leaked raw/reasoning data: %s", encoded)
	}
}

func TestObservationCompletesWithDurationAndStatus(t *testing.T) {
	started := time.Date(2026, time.August, 2, 10, 0, 0, 0, time.UTC)
	observation := NewObservation("obs-1", "request-1", "trace-1", "span-1", "upstream", "provider.call", started)
	observation.Finish(started.Add(25*time.Millisecond), "ok", "")
	if observation.CompletedAt == nil || observation.DurationMillis != 25 || observation.Status != "ok" {
		t.Fatalf("observation = %+v", observation)
	}
}
