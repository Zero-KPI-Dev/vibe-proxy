package streamengine

import (
	"context"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

func TestTrackRecordsTokenAndUsage(t *testing.T) {
	in := make(chan ir.StreamEvent, 4)
	in <- ir.StreamEvent{Type: ir.EventMessageStart}
	in <- ir.StreamEvent{Type: ir.EventContentDelta, Delta: ir.ContentBlock{Type: ir.ContentText, Text: "hi"}}
	in <- ir.StreamEvent{Type: ir.EventUsageDelta, Usage: &ir.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}}
	in <- ir.StreamEvent{Type: ir.EventMessageDone}
	close(in)
	var final Stats
	reports := 0
	out := Track(context.Background(), in, func(s Stats) {
		final = s
		reports++
	})
	count := 0
	for range out {
		count++
	}
	if count != 4 {
		t.Fatalf("unexpected event count: %d", count)
	}
	if final.OutputTokenCount != 1 || final.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected stats: %+v", final)
	}
	if final.StartedAt.IsZero() || final.FirstTokenAt.IsZero() || final.CompletedAt.IsZero() {
		t.Fatalf("expected lifecycle timestamps to be recorded: %+v", final)
	}
	if final.FirstTokenAt.Before(final.StartedAt) || final.CompletedAt.Before(final.FirstTokenAt) {
		t.Fatalf("unexpected lifecycle timestamp order: %+v", final)
	}
	if reports != 1 {
		t.Fatalf("stats callback invoked %d times", reports)
	}
}

func TestAccumulateStreamToUnary(t *testing.T) {
	in := make(chan ir.StreamEvent, 4)
	in <- ir.StreamEvent{Type: ir.EventContentDelta, Delta: ir.ContentBlock{Type: ir.ContentText, Text: "he"}}
	in <- ir.StreamEvent{Type: ir.EventContentDelta, Delta: ir.ContentBlock{Type: ir.ContentText, Text: "llo"}}
	in <- ir.StreamEvent{Type: ir.EventUsageDelta, Usage: &ir.Usage{PromptTokens: 1, CompletionTokens: 5, TotalTokens: 6}}
	in <- ir.StreamEvent{Type: ir.EventMessageDone}
	close(in)
	resp, stats, err := Accumulate(context.Background(), in, "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Messages[0].Content[0].Text != "hello" {
		t.Fatalf("unexpected text: %+v", resp.Messages)
	}
	if stats.OutputTokenCount != 2 || resp.Usage.TotalTokens != 6 {
		t.Fatalf("unexpected stats/usage: %+v %+v", stats, resp.Usage)
	}
}

func TestAccumulateHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan ir.StreamEvent)
	cancel()
	_, _, err := Accumulate(ctx, in, "test")
	if err == nil {
		t.Fatal("expected context error")
	}
	_ = time.Now()
}
