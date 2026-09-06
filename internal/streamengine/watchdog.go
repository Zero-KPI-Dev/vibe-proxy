package streamengine

import (
	"context"
	"sync"
	"time"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

const DefaultProgressTimeout = 60 * time.Second

// Watchdog bounds time to first output and gaps between actual output events.
// HTTP headers, SSE comments and keepalives do not count as model progress.
// The owning request supplies a separate total deadline.
type Watchdog struct {
	mu       sync.Mutex
	cancel   context.CancelCauseFunc
	timer    *time.Timer
	deadline time.Time
	idle     time.Duration
	started  bool
	closed   bool
}

func NewWatchdog(parent context.Context, firstToken, idle time.Duration) (context.Context, *Watchdog) {
	if firstToken <= 0 {
		firstToken = DefaultProgressTimeout
	}
	if idle <= 0 {
		idle = DefaultProgressTimeout
	}
	ctx, cancel := context.WithCancelCause(parent)
	w := &Watchdog{cancel: cancel, idle: idle, deadline: time.Now().Add(firstToken)}
	w.timer = time.AfterFunc(firstToken, w.expire)
	return ctx, w
}

func (w *Watchdog) expire() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if remaining := time.Until(w.deadline); remaining > 0 {
		w.timer.Reset(remaining)
		return
	}
	w.closed = true
	code, message := "upstream_first_token_timeout", "Upstream did not produce the first token within the configured time limit."
	if w.started {
		code, message = "upstream_stream_idle_timeout", "Upstream stopped producing output within the configured time limit."
	}
	w.cancel(ir.GatewayError{StatusCode: 504, Kind: "upstream_error", Code: code, Message: message})
}

func (w *Watchdog) Progress() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.started = true
	w.deadline = time.Now().Add(w.idle)
	w.timer.Reset(w.idle)
}

func (w *Watchdog) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	w.timer.Stop()
	w.cancel(context.Canceled)
}
