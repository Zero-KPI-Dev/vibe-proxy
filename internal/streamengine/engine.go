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

// TPOT returns the average generation time after the first output token. The
// final provider usage is authoritative: stream delta count is a transport
// detail and may not correspond one-to-one with model tokens.
func (s Stats) TPOT() time.Duration {
	if s.Usage.CompletionTokens <= 1 || s.FirstTokenAt.IsZero() || s.CompletedAt.IsZero() || s.CompletedAt.Before(s.FirstTokenAt) {
		return 0
	}
	return s.CompletedAt.Sub(s.FirstTokenAt) / time.Duration(s.Usage.CompletionTokens-1)
}

func Track(ctx context.Context, in <-chan ir.StreamEvent, onStats func(Stats)) <-chan ir.StreamEvent {
	return TrackWithProgress(ctx, in, nil, onStats)
}

// TrackWithProgress preserves Track's once-only completion callback while also
// exposing live token/usage progress to non-durable telemetry sinks.
func TrackWithProgress(ctx context.Context, in <-chan ir.StreamEvent, onProgress, onFinished func(Stats)) <-chan ir.StreamEvent {
	out := make(chan ir.StreamEvent, 32)
	go func() {
		defer close(out)
		stats := Stats{StartedAt: time.Now()}
		reported := false
		report := func() {
			if reported {
				return
			}
			reported = true
			stats.CompletedAt = time.Now()
			if onFinished != nil {
				onFinished(stats)
			}
		}
		for {
			select {
			case <-ctx.Done():
				report()
				return
			case ev, ok := <-in:
				if !ok {
					report()
					return
				}
				if countsAsToken(ev) {
					if stats.FirstTokenAt.IsZero() {
						stats.FirstTokenAt = time.Now()
					}
					stats.OutputTokenCount++
					if onProgress != nil {
						onProgress(stats)
					}
				}
				if ev.Usage != nil {
					stats.Usage = mergeUsage(stats.Usage, *ev.Usage)
					if onProgress != nil && !stats.FirstTokenAt.IsZero() {
						onProgress(stats)
					}
				}
				if ev.Type == ir.EventMessageDone {
					report()
				}
				if ev.Error != nil {
					report()
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					report()
					return
				}
				if ev.Type == ir.EventMessageDone || ev.Error != nil {
					return
				}
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
	if (ev.Type == ir.EventToolCallStart || ev.Type == ir.EventToolCallDelta) && ev.ToolCall != nil {
		return len(ev.ToolCall.Arguments) > 0 || ev.ToolCall.Name != "" || ev.ToolCall.ID != ""
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
	if b.CacheMetricsReported {
		a.CacheMetricsReported = true
	}
	if b.CacheHitRatio != 0 || b.CacheMetricsReported {
		a.CacheHitRatio = b.CacheHitRatio
	}
	if a.TotalTokens == 0 {
		a.TotalTokens = a.PromptTokens + a.CompletionTokens
	}
	return a
}
