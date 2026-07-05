package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

type ResponsesAdapter struct{}

func (a ResponsesAdapter) Name() string                { return "openai-responses" }
func (a ResponsesAdapter) Protocol() ir.Protocol       { return ir.ProtocolOpenAIResponses }
func (a ResponsesAdapter) Detect(r *http.Request) bool { return r.URL.Path == "/v1/responses" }

func (a ResponsesAdapter) ParseRequest(ctx context.Context, r *http.Request) (*ir.Request, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		return nil, ir.GatewayError{StatusCode: 400, Kind: "invalid_request_error", Code: "body_read_failed", Message: "Could not read request body."}
	}
	var in responsesRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, ir.GatewayError{StatusCode: 400, Kind: "invalid_request_error", Code: "invalid_json", Message: "Request body is not valid JSON."}
	}
	if in.Model == "" {
		return nil, ir.GatewayError{StatusCode: 400, Kind: "invalid_request_error", Code: "missing_model", Message: "Request JSON must include a model."}
	}
	req := &ir.Request{ID: uuid.NewString(), ClientProtocol: ir.ProtocolOpenAIResponses, RequestedModel: in.Model, Stream: in.Stream, MaxTokens: in.MaxOutputTokens, Temperature: in.Temperature, TopP: in.TopP, VendorExtensions: map[string]any{"openai_responses_raw": json.RawMessage(body)}}
	req.Messages = parseResponsesInput(in.Input)
	for _, t := range in.Tools {
		req.Tools = append(req.Tools, ir.Tool{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	if in.Text.Format.Type != "" {
		raw, _ := json.Marshal(in.Text.Format)
		req.ResponseFormat = &ir.ResponseFormat{Type: in.Text.Format.Type, JSONSchema: raw}
	}
	return req, nil
}

func (a ResponsesAdapter) EncodeUnary(ctx context.Context, w http.ResponseWriter, resp *ir.Response) error {
	text := ""
	if len(resp.Messages) > 0 {
		text = flattenText(resp.Messages[len(resp.Messages)-1].Content)
	}
	id := resp.ID
	if id == "" {
		id = "resp_" + uuid.NewString()
	}
	out := map[string]any{"id": id, "object": "response", "created_at": time.Now().Unix(), "status": "completed", "model": resp.Model, "output": []any{map[string]any{"id": "msg_" + uuid.NewString(), "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}}, "usage": map[string]any{"input_tokens": resp.Usage.PromptTokens, "output_tokens": resp.Usage.CompletionTokens, "total_tokens": resp.Usage.TotalTokens}}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(out)
}

func (a ResponsesAdapter) EncodeStream(ctx context.Context, w http.ResponseWriter, events <-chan ir.StreamEvent) error {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	responseID := "resp_" + uuid.NewString()
	itemID := "msg_" + uuid.NewString()
	writeResponsesEvent(w, "response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": responseID, "object": "response", "status": "in_progress"}})
	flushOpenAI(flusher)
	writeResponsesEvent(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": itemID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}})
	flushOpenAI(flusher)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				writeResponsesDone(w, responseID)
				flushOpenAI(flusher)
				return nil
			}
			if ev.Error != nil {
				return *ev.Error
			}
			switch ev.Type {
			case ir.EventContentDelta, ir.EventReasoningDelta:
				if ev.Delta.Text == "" {
					continue
				}
				writeResponsesEvent(w, "response.output_text.delta", map[string]any{"type": "response.output_text.delta", "item_id": itemID, "output_index": 0, "content_index": 0, "delta": ev.Delta.Text})
				flushOpenAI(flusher)
			case ir.EventToolCallDelta, ir.EventToolCallStart:
				if ev.ToolCall != nil {
					writeResponsesEvent(w, "response.function_call_arguments.delta", map[string]any{"type": "response.function_call_arguments.delta", "item_id": ev.ToolCall.ID, "output_index": 0, "delta": string(ev.ToolCall.Arguments)})
					flushOpenAI(flusher)
				}
			case ir.EventMessageDone:
				writeResponsesEvent(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{"id": itemID, "type": "message", "status": "completed", "role": "assistant"}})
				writeResponsesDone(w, responseID)
				flushOpenAI(flusher)
				return nil
			}
		}
	}
}

func (a ResponsesAdapter) EncodeError(ctx context.Context, w http.ResponseWriter, err ir.GatewayError) error {
	return ChatAdapter{}.EncodeError(ctx, w, err)
}

func parseResponsesInput(input any) []ir.Message {
	switch v := input.(type) {
	case string:
		return []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: v}}}}
	case []any:
		messages := []ir.Message{}
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			role, _ := m["role"].(string)
			if role == "" {
				role = "user"
			}
			switch t {
			case "message", "":
				messages = append(messages, ir.Message{Role: ir.Role(role), Content: responsesContent(m["content"])})
			case "function_call_output":
				callID, _ := m["call_id"].(string)
				output, _ := m["output"].(string)
				messages = append(messages, ir.Message{Role: ir.RoleTool, Content: []ir.ContentBlock{{Type: ir.ContentToolResult, ToolResult: &ir.ToolResult{ToolCallID: callID, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: output}}}}}})
			}
		}
		return messages
	default:
		return nil
	}
}

func responsesContent(content any) []ir.ContentBlock {
	switch c := content.(type) {
	case string:
		return []ir.ContentBlock{{Type: ir.ContentText, Text: c}}
	case []any:
		blocks := []ir.ContentBlock{}
		for _, it := range c {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			switch t {
			case "input_text", "output_text", "text":
				txt, _ := m["text"].(string)
				blocks = append(blocks, ir.ContentBlock{Type: ir.ContentText, Text: txt})
			case "input_image":
				url, _ := m["image_url"].(string)
				blocks = append(blocks, ir.ContentBlock{Type: ir.ContentImage, Image: &ir.ImageContent{URL: url}})
			}
		}
		return blocks
	default:
		return nil
	}
}
func writeResponsesEvent(w http.ResponseWriter, event string, payload any) {
	b, _ := json.Marshal(payload)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}
func writeResponsesDone(w http.ResponseWriter, id string) {
	writeResponsesEvent(w, "response.completed", map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed"}})
	fmt.Fprint(w, "data: [DONE]\n\n")
}
func flushOpenAI(f http.Flusher) {
	if f != nil {
		f.Flush()
	}
}

type responsesRequest struct {
	Model           string          `json:"model"`
	Input           any             `json:"input"`
	Stream          bool            `json:"stream"`
	MaxOutputTokens *int            `json:"max_output_tokens"`
	Temperature     *float64        `json:"temperature"`
	TopP            *float64        `json:"top_p"`
	Tools           []responsesTool `json:"tools"`
	Text            struct {
		Format struct {
			Type   string          `json:"type"`
			Name   string          `json:"name,omitempty"`
			Schema json.RawMessage `json:"schema,omitempty"`
		} `json:"format"`
	} `json:"text"`
}
type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

var _ = strings.Builder{}
