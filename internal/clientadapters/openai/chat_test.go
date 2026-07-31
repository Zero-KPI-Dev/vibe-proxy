package openai

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

func TestParseChatImageURL(t *testing.T) {
	body := []byte(`{"model":"vision","messages":[{"role":"user","content":[{"type":"text","text":"read"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]}]}`)
	r, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req, err := (ChatAdapter{}).ParseRequest(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Content) != 2 {
		t.Fatalf("unexpected content: %+v", req.Messages)
	}
	image := req.Messages[0].Content[1]
	if image.Type != ir.ContentImage || image.Image == nil || image.Image.URL != "data:image/png;base64,AA==" {
		t.Fatalf("image not parsed: %+v", image)
	}
}

func TestParseChatToolChoice(t *testing.T) {
	body := []byte(`{"model":"agent","messages":[{"role":"user","content":"lookup"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"lookup"}},"response_format":{"type":"json_object"}}`)
	request, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	parsed, err := (ChatAdapter{}).ParseRequest(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ToolChoice == nil || parsed.ToolChoice.Type != "tool" || parsed.ToolChoice.Name != "lookup" {
		t.Fatalf("tool choice not parsed: %+v", parsed.ToolChoice)
	}
	if parsed.ResponseFormat == nil || parsed.ResponseFormat.Type != "json_object" {
		t.Fatalf("response format not parsed: %+v", parsed.ResponseFormat)
	}
}

func TestEncodeChatUnaryPreservesToolCalls(t *testing.T) {
	response := &ir.Response{
		ID:         "resp_1",
		Model:      "claude",
		StopReason: "tool_use",
		Messages: []ir.Message{{
			Role: ir.RoleAssistant,
			Content: []ir.ContentBlock{{
				Type: ir.ContentToolCall,
				ToolCall: &ir.ToolCall{
					ID:        "call_1",
					Name:      "lookup",
					Arguments: json.RawMessage(`{"q":"vibe"}`),
				},
			}},
		}},
	}
	recorder := httptest.NewRecorder()
	if err := (ChatAdapter{}).EncodeUnary(context.Background(), recorder, response); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"finish_reason":"tool_calls"`) || !strings.Contains(body, `"id":"call_1"`) || !strings.Contains(body, `"name":"lookup"`) || !strings.Contains(body, `"arguments":"{\"q\":\"vibe\"}"`) {
		t.Fatalf("tool call was not encoded: %s", body)
	}
}

func TestEncodeChatKeepsReasoningOutOfVisibleContent(t *testing.T) {
	response := &ir.Response{Messages: []ir.Message{{
		Role: ir.RoleAssistant,
		Content: []ir.ContentBlock{
			{Type: ir.ContentReasoning, Text: "private reasoning"},
			{Type: ir.ContentText, Text: "final answer"},
		},
	}}}
	recorder := httptest.NewRecorder()
	if err := (ChatAdapter{}).EncodeUnary(context.Background(), recorder, response); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	message := payload.Choices[0].Message
	if message.Content != "final answer" || message.ReasoningContent != "private reasoning" {
		t.Fatalf("reasoning was merged into content: %s", recorder.Body.String())
	}
}

func TestEncodeChatStreamKeepsReasoningInExtensionDelta(t *testing.T) {
	events := make(chan ir.StreamEvent, 3)
	events <- ir.StreamEvent{Type: ir.EventReasoningDelta, Delta: ir.ContentBlock{Type: ir.ContentReasoning, Text: "private reasoning"}}
	events <- ir.StreamEvent{Type: ir.EventContentDelta, Delta: ir.ContentBlock{Type: ir.ContentText, Text: "final answer"}}
	events <- ir.StreamEvent{Type: ir.EventMessageDone}
	close(events)
	recorder := httptest.NewRecorder()
	if err := (ChatAdapter{}).EncodeStream(context.Background(), recorder, events); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"reasoning_content":"private reasoning"`) ||
		!strings.Contains(body, `"content":"final answer"`) ||
		strings.Contains(body, `"content":"private reasoning"`) {
		t.Fatalf("reasoning stream leaked into visible content: %s", body)
	}
}
