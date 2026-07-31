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

func TestParseResponsesRequest(t *testing.T) {
	body := []byte(`{"model":"vibe-coder","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"tool_choice":"required","text":{"format":{"type":"json_schema","name":"answer","schema":{"type":"object"}}},"stream":true,"max_output_tokens":128}`)
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
	if req.ToolChoice == nil || req.ToolChoice.Type != "required" {
		t.Fatalf("tool choice not parsed: %+v", req.ToolChoice)
	}
	if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_schema" {
		t.Fatalf("response format not parsed: %+v", req.ResponseFormat)
	}
}

func TestParseResponsesImage(t *testing.T) {
	body := []byte(`{"model":"vision","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"data:image/jpeg;base64,AA=="}]}]}`)
	r, _ := http.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	req, err := (ResponsesAdapter{}).ParseRequest(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	image := req.Messages[0].Content[0]
	if image.Type != ir.ContentImage || image.Image == nil || image.Image.URL != "data:image/jpeg;base64,AA==" {
		t.Fatalf("image not parsed: %+v", image)
	}
}

func TestEncodeResponsesUnaryPreservesFunctionCalls(t *testing.T) {
	response := &ir.Response{
		ID:    "resp_1",
		Model: "claude",
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
	if err := (ResponsesAdapter{}).EncodeUnary(context.Background(), recorder, response); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"type":"function_call"`) || !strings.Contains(body, `"call_id":"call_1"`) || !strings.Contains(body, `"name":"lookup"`) || !strings.Contains(body, `"arguments":"{\"q\":\"vibe\"}"`) {
		t.Fatalf("function call was not encoded: %s", body)
	}
	if strings.Contains(body, `"type":"message"`) {
		t.Fatalf("tool-only response should not contain an empty message item: %s", body)
	}
}

func TestEncodeResponsesStreamAddsFunctionCallItem(t *testing.T) {
	events := make(chan ir.StreamEvent, 3)
	events <- ir.StreamEvent{Type: ir.EventToolCallStart, ToolCall: &ir.ToolCall{ID: "call_1", Name: "lookup", Arguments: json.RawMessage(`{}`)}}
	events <- ir.StreamEvent{Type: ir.EventToolCallDelta, ToolCall: &ir.ToolCall{Arguments: json.RawMessage(`{"q":"vibe"}`)}}
	events <- ir.StreamEvent{Type: ir.EventMessageDone}
	close(events)
	recorder := httptest.NewRecorder()
	if err := (ResponsesAdapter{}).EncodeStream(context.Background(), recorder, events); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"type":"function_call"`) || !strings.Contains(body, `"call_id":"call_1"`) || !strings.Contains(body, `response.function_call_arguments.delta`) || !strings.Contains(body, `\"q\":\"vibe\"`) {
		t.Fatalf("function call stream was not encoded: %s", body)
	}
}

func TestEncodeResponsesKeepsReasoningSeparateFromOutputText(t *testing.T) {
	response := &ir.Response{Messages: []ir.Message{{
		Role: ir.RoleAssistant,
		Content: []ir.ContentBlock{
			{Type: ir.ContentReasoning, Text: "private reasoning"},
			{Type: ir.ContentText, Text: "final answer"},
		},
	}}}
	recorder := httptest.NewRecorder()
	if err := (ResponsesAdapter{}).EncodeUnary(context.Background(), recorder, response); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"type":"reasoning"`) ||
		!strings.Contains(body, `"type":"summary_text"`) ||
		!strings.Contains(body, `"text":"private reasoning"`) ||
		!strings.Contains(body, `"type":"output_text"`) ||
		!strings.Contains(body, `"text":"final answer"`) {
		t.Fatalf("reasoning was not encoded separately from output text: %s", body)
	}
}

func TestEncodeResponsesStreamKeepsReasoningSeparateFromOutputText(t *testing.T) {
	events := make(chan ir.StreamEvent, 3)
	events <- ir.StreamEvent{Type: ir.EventReasoningDelta, Delta: ir.ContentBlock{Type: ir.ContentReasoning, Text: "private reasoning"}}
	events <- ir.StreamEvent{Type: ir.EventContentDelta, Delta: ir.ContentBlock{Type: ir.ContentText, Text: "final answer"}}
	events <- ir.StreamEvent{Type: ir.EventMessageDone}
	close(events)
	recorder := httptest.NewRecorder()
	if err := (ResponsesAdapter{}).EncodeStream(context.Background(), recorder, events); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `event: response.reasoning_summary_text.delta`) ||
		!strings.Contains(body, `"delta":"private reasoning"`) ||
		!strings.Contains(body, `event: response.output_text.delta`) ||
		!strings.Contains(body, `"delta":"final answer"`) {
		t.Fatalf("reasoning stream was not encoded separately from output text: %s", body)
	}
}
