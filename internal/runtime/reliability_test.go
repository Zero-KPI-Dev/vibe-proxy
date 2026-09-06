package runtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clientanthropic "github.com/a448582655/vibe-proxy/internal/clientadapters/anthropic"
	clientopenai "github.com/a448582655/vibe-proxy/internal/clientadapters/openai"
	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/protocol"
)

var errDisconnected = errors.New("client stopped reading")

type failedStreamWriter struct {
	*httptest.ResponseRecorder
	flushOnly bool
}

func (w *failedStreamWriter) Write(p []byte) (int, error) {
	if !w.flushOnly {
		return 0, errDisconnected
	}
	return w.ResponseRecorder.Write(p)
}

func (w *failedStreamWriter) FlushError() error { return errDisconnected }
func (w *failedStreamWriter) Flush()            { _ = w.FlushError() }

func TestStreamAdaptersPropagateDownstreamFailure(t *testing.T) {
	for _, adapter := range []protocol.ClientAdapter{clientopenai.ChatAdapter{}, clientopenai.ResponsesAdapter{}, clientanthropic.MessagesAdapter{}} {
		for _, flushOnly := range []bool{false, true} {
			t.Run(adapter.Name()+map[bool]string{false: "/write", true: "/flush"}[flushOnly], func(t *testing.T) {
				events := make(chan ir.StreamEvent, 2)
				events <- ir.StreamEvent{Type: ir.EventContentDelta, Delta: ir.ContentBlock{Text: "hello"}}
				events <- ir.StreamEvent{Type: ir.EventMessageDone}
				close(events)
				writer := &failedStreamWriter{ResponseRecorder: httptest.NewRecorder(), flushOnly: flushOnly}
				// Production middleware must preserve ResponseController support.
				wrapped := &recorder{ResponseWriter: writer}
				if err := adapter.EncodeStream(context.Background(), wrapped, events); !errors.Is(err, errDisconnected) {
					t.Fatalf("stream reported success after broken downstream: %v", err)
				}
			})
		}
	}
}

func TestStreamAdaptersRejectMissingCompletion(t *testing.T) {
	for _, adapter := range []protocol.ClientAdapter{clientopenai.ChatAdapter{}, clientopenai.ResponsesAdapter{}, clientanthropic.MessagesAdapter{}} {
		t.Run(adapter.Name(), func(t *testing.T) {
			events := make(chan ir.StreamEvent)
			close(events)
			w := httptest.NewRecorder()
			if err := adapter.EncodeStream(context.Background(), w, events); err == nil {
				t.Fatal("closed event channel was treated as a successful response")
			}
			if strings.Contains(w.Body.String(), "[DONE]") || strings.Contains(w.Body.String(), "response.completed") || strings.Contains(w.Body.String(), "message_stop") {
				t.Fatal("incomplete upstream stream emitted a success terminator")
			}
		})
	}
}

func TestProviderConcurrencyLimitChangesWithoutRestart(t *testing.T) {
	s := &Server{}
	if !s.acquire("provider", 1) || s.acquire("provider", 1) {
		t.Fatal("initial limit not enforced")
	}
	if !s.acquire("provider", 2) {
		t.Fatal("increased concurrency setting did not take effect")
	}
	if s.acquire("provider", 1) {
		t.Fatal("lowering the limit admitted additional work")
	}
	s.release("provider")
	if s.acquire("provider", 1) {
		t.Fatal("lowered limit ignored an existing request")
	}
	s.release("provider")
	if !s.acquire("provider", 1) {
		t.Fatal("provider did not recover after active requests finished")
	}
	s.release("provider")
}

var _ http.ResponseWriter = (*failedStreamWriter)(nil)
