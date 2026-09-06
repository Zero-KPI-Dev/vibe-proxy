package streamengine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

func TestWatchdogSeparatesFirstTokenAndIdleTimeout(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "first_token", true: "idle"}[started], func(t *testing.T) {
			ctx, watcher := NewWatchdog(context.Background(), 30*time.Millisecond, 30*time.Millisecond)
			defer watcher.Close()
			if started {
				watcher.Progress()
			}
			select {
			case <-ctx.Done():
				var failure ir.GatewayError
				want := "upstream_first_token_timeout"
				if started {
					want = "upstream_stream_idle_timeout"
				}
				if !errors.As(context.Cause(ctx), &failure) || failure.Code != want {
					t.Fatalf("unexpected cause: %v", context.Cause(ctx))
				}
			case <-time.After(time.Second):
				t.Fatal("watchdog did not expire")
			}
		})
	}
}

func TestWatchdogProgressExtendsIdleButNotTotalDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
	defer cancel()
	ctx, watcher := NewWatchdog(parent, time.Second, 100*time.Millisecond)
	defer watcher.Close()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			watcher.Progress()
		case <-ctx.Done():
			if !errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
				t.Fatalf("active stream expired early: %v", context.Cause(ctx))
			}
			return
		}
	}
}
