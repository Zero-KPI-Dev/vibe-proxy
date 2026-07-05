package openai

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

func TestParseResponsesRequest(t *testing.T) {
	body := []byte(`{"model":"vibe-coder","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"stream":true,"max_output_tokens":128}`)
	r, _ := http.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	req, err := ResponsesAdapter{}.ParseRequest(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if req.ClientProtocol != ir.ProtocolOpenAIResponses || !req.Stream || req.RequestedModel != "vibe-coder" {
		t.Fatalf("unexpected request: %+v", req)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != ir.RoleUser || req.Messages[0].Content[0].Text != "hello" {
		t.Fatalf("input not parsed: %+v", req.Messages)
	}
	if req.MaxTokens == nil || *req.MaxTokens != 128 {
		t.Fatalf("max output not parsed: %+v", req.MaxTokens)
	}
}
