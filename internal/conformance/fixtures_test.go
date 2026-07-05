package conformance

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	clientanthropic "github.com/a448582655/vibe-proxy/internal/clientadapters/anthropic"
	clientopenai "github.com/a448582655/vibe-proxy/internal/clientadapters/openai"
	"github.com/a448582655/vibe-proxy/internal/ir"
)

func TestConformanceFixturesParseToIR(t *testing.T) {
	cases := []struct {
		path    string
		url     string
		adapter interface {
			ParseRequest(context.Context, *http.Request) (*ir.Request, error)
		}
		protocol ir.Protocol
		model    string
	}{
		{"openai_chat_basic.json", "/v1/chat/completions", clientopenai.ChatAdapter{}, ir.ProtocolOpenAIChat, "vibe-fast"},
		{"openai_responses_basic.json", "/v1/responses", clientopenai.ResponsesAdapter{}, ir.ProtocolOpenAIResponses, "vibe-fast"},
		{"anthropic_messages_basic.json", "/anthropic/v1/messages", clientanthropic.MessagesAdapter{}, ir.ProtocolAnthropicMessages, "vibe-coder"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "conformance", "fixtures", tc.path))
			if err != nil {
				t.Fatal(err)
			}
			r, _ := http.NewRequest(http.MethodPost, tc.url, bytes.NewReader(body))
			req, err := tc.adapter.ParseRequest(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if req.ClientProtocol != tc.protocol || req.RequestedModel != tc.model {
				t.Fatalf("unexpected IR: %+v", req)
			}
			if len(req.Messages) == 0 {
				t.Fatalf("expected messages in IR")
			}
		})
	}
}
