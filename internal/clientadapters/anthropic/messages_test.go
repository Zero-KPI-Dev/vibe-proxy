package anthropic

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

func TestParseAnthropicMessagesRequest(t *testing.T) {
	body := []byte(`{"model":"claude-test","max_tokens":100,"system":[{"type":"text","text":"sys"}],"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`)
	r, _ := http.NewRequest(http.MethodPost, "/anthropic/v1/messages", bytes.NewReader(body))
	req, err := MessagesAdapter{}.ParseRequest(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if req.ClientProtocol != ir.ProtocolAnthropicMessages || req.RequestedModel != "claude-test" {
		t.Fatalf("unexpected request: %+v", req)
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != ir.RoleSystem || req.Messages[1].Content[0].Text != "hello" {
		t.Fatalf("messages not parsed: %+v", req.Messages)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "lookup" {
		t.Fatalf("tools not parsed: %+v", req.Tools)
	}
}
