package streamengine

import (
	"context"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

type Stats struct {
	StartedAt        time.Time
	FirstTokenAt     time.Time
	CompletedAt      time.Time
	OutputTokenCount int64
	Usage            ir.Usage
}

func (s Stats) TTFT() time.Duration {
	if s.FirstTokenAt.IsZero() || s.StartedAt.IsZero() {
		return 0
	}
	return s.FirstTokenAt.Sub(s.StartedAt)
}

func Track(ctx context.Context, in <-chan ir.StreamEvent, onStats func(Stats)) <-chan ir.StreamEvent {
	out := make(chan ir.StreamEvent, 32)
	go func() {
		defer close(out)
		stats := Stats{StartedAt: time.Now()}
		for {
			select {
			case <-ctx.Done():
				stats.CompletedAt = time.Now()
				if onStats != nil {
					onStats(stats)
				}
				out <- ir.StreamEvent{Type: ir.EventError, Time: time.Now(), Error: &ir.GatewayError{StatusCode: 499, Kind: "canceled", Code: "client_closed", Message: "Client closed the request."}}
				return
			case ev, ok := <-in:
				if !ok {
					stats.CompletedAt = time.Now()
					if onStats != nil {
						onStats(stats)
					}
					return
				}
				if countsAsToken(ev) {
					if stats.FirstTokenAt.IsZero() {
						stats.FirstTokenAt = time.Now()
					}
					stats.OutputTokenCount++
				}
				if ev.Usage != nil {
					stats.Usage = mergeUsage(stats.Usage, *ev.Usage)
				}
				if ev.Type == ir.EventMessageDone {
					stats.CompletedAt = time.Now()
					if onStats != nil {
						onStats(stats)
					}
				}
				out <- ev
			}
		}
	}()
	return out
}

func Accumulate(ctx context.Context, events <-chan ir.StreamEvent, model string) (*ir.Response, Stats, error) {
	stats := Stats{StartedAt: time.Now()}
	var text strings.Builder
	resp := &ir.Response{Model: model}
	for {
		select {
		case <-ctx.Done():
			return nil, stats, ctx.Err()
		case ev, ok := <-events:
			if !ok {
				stats.CompletedAt = time.Now()
				resp.Messages = []ir.Message{{Role: ir.RoleAssistant, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: text.String()}}}}
				resp.Usage = stats.Usage
				return resp, stats, nil
			}
			if ev.Error != nil {
				return nil, stats, *ev.Error
			}
			if countsAsToken(ev) {
				if stats.FirstTokenAt.IsZero() {
					stats.FirstTokenAt = time.Now()
				}
				stats.OutputTokenCount++
				text.WriteString(ev.Delta.Text)
			}
			if ev.Usage != nil {
				stats.Usage = mergeUsage(stats.Usage, *ev.Usage)
			}
			if ev.Type == ir.EventMessageDone {
				stats.CompletedAt = time.Now()
				resp.Messages = []ir.Message{{Role: ir.RoleAssistant, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: text.String()}}}}
				resp.Usage = stats.Usage
				return resp, stats, nil
			}
		}
	}
}

func countsAsToken(ev ir.StreamEvent) bool {
	if ev.Type == ir.EventContentDelta || ev.Type == ir.EventReasoningDelta {
		return ev.Delta.Text != ""
	}
	if ev.Type == ir.EventToolCallDelta && ev.ToolCall != nil {
		return len(ev.ToolCall.Arguments) > 0
	}
	return false
}

func mergeUsage(a, b ir.Usage) ir.Usage {
	if b.PromptTokens != 0 {
		a.PromptTokens = b.PromptTokens
	}
	if b.CompletionTokens != 0 {
		a.CompletionTokens = b.CompletionTokens
	}
	if b.TotalTokens != 0 {
		a.TotalTokens = b.TotalTokens
	}
	if b.ReasoningTokens != 0 {
		a.ReasoningTokens = b.ReasoningTokens
	}
	if b.CacheReadTokens != 0 {
		a.CacheReadTokens = b.CacheReadTokens
	}
	if b.CacheWriteTokens != 0 {
		a.CacheWriteTokens = b.CacheWriteTokens
	}
	if b.CacheHitRatio != 0 {
		a.CacheHitRatio = b.CacheHitRatio
	}
	if a.TotalTokens == 0 {
		a.TotalTokens = a.PromptTokens + a.CompletionTokens
	}
	return a
}
