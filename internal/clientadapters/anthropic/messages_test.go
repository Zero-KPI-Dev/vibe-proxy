package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

func TestParseAnthropicMessagesRequest(t *testing.T) {
	body := []byte(`{"model":"claude-test","max_tokens":100,"system":[{"type":"text","text":"sys"}],"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"lookup"}}`)
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
	if req.ToolChoice == nil || req.ToolChoice.Type != "tool" || req.ToolChoice.Name != "lookup" {
		t.Fatalf("tool choice not parsed: %+v", req.ToolChoice)
	}
}

func TestParseAnthropicMessagesAcceptsStringSystemAndContent(t *testing.T) {
	body := []byte(`{"model":"claude-test","max_tokens":100,"system":"Follow instructions.","messages":[{"role":"user","content":"hello"}]}`)
	r, _ := http.NewRequest(http.MethodPost, "/anthropic/v1/messages", bytes.NewReader(body))
	req, err := MessagesAdapter{}.ParseRequest(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != ir.RoleSystem || req.Messages[0].Content[0].Text != "Follow instructions." || req.Messages[1].Content[0].Text != "hello" {
		t.Fatalf("string content was not normalized: %+v", req.Messages)
	}
}

func TestParseAnthropicBase64Image(t *testing.T) {
	body := []byte(`{"model":"vision","max_tokens":32,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}}]}]}`)
	r, _ := http.NewRequest(http.MethodPost, "/anthropic/v1/messages", bytes.NewReader(body))
	req, err := (MessagesAdapter{}).ParseRequest(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	image := req.Messages[0].Content[0]
	if image.Type != ir.ContentImage || image.Image == nil || image.Image.MediaType != "image/png" || image.Image.Base64 != "AA==" {
		t.Fatalf("image not parsed: %+v", image)
	}
}

func TestEncodeAnthropicStreamPreservesToolCalls(t *testing.T) {
	events := make(chan ir.StreamEvent, 4)
	events <- ir.StreamEvent{Type: ir.EventToolCallStart, ToolCall: &ir.ToolCall{ID: "toolu_1", Name: "lookup", Arguments: json.RawMessage(`{}`)}}
	events <- ir.StreamEvent{Type: ir.EventToolCallDelta, ToolCall: &ir.ToolCall{ID: "toolu_1", Name: "lookup", Arguments: json.RawMessage(`{"q":"vibe"}`)}}
	events <- ir.StreamEvent{Type: ir.EventToolCallDone, ToolCall: &ir.ToolCall{ID: "toolu_1", Name: "lookup"}}
	events <- ir.StreamEvent{Type: ir.EventMessageDone}
	close(events)
	recorder := httptest.NewRecorder()
	if err := (MessagesAdapter{}).EncodeStream(context.Background(), recorder, events); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"type":"tool_use"`) || !strings.Contains(body, `"id":"toolu_1"`) || !strings.Contains(body, `"name":"lookup"`) || !strings.Contains(body, `"type":"input_json_delta"`) || !strings.Contains(body, `"stop_reason":"tool_use"`) {
		t.Fatalf("tool call stream was not encoded: %s", body)
	}
}
