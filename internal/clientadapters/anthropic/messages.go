package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

type MessagesAdapter struct{}

func (a MessagesAdapter) Name() string          { return "anthropic-messages" }
func (a MessagesAdapter) Protocol() ir.Protocol { return ir.ProtocolAnthropicMessages }
func (a MessagesAdapter) Detect(r *http.Request) bool {
	return r.URL.Path == "/anthropic/v1/messages" || r.URL.Path == "/v1/messages"
}

func (a MessagesAdapter) ParseRequest(ctx context.Context, r *http.Request) (*ir.Request, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		return nil, ir.GatewayError{StatusCode: 400, Kind: "invalid_request_error", Code: "body_read_failed", Message: "Could not read request body."}
	}
	var in request
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, ir.GatewayError{StatusCode: 400, Kind: "invalid_request_error", Code: "invalid_json", Message: "Request body is not valid JSON."}
	}
	if in.Model == "" {
		return nil, ir.GatewayError{StatusCode: 400, Kind: "invalid_request_error", Code: "missing_model", Message: "Request JSON must include a model."}
	}
	max := int(in.MaxTokens)
	req := &ir.Request{ID: uuid.NewString(), ClientProtocol: ir.ProtocolAnthropicMessages, RequestedModel: in.Model, Stream: in.Stream, MaxTokens: &max, Temperature: in.Temperature, TopP: in.TopP, Stop: in.StopSequences, VendorExtensions: map[string]any{"anthropic_raw": json.RawMessage(body)}}
	for _, s := range in.System {
		req.Messages = append(req.Messages, ir.Message{Role: ir.RoleSystem, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: s.Text}}})
	}
	for _, m := range in.Messages {
		req.Messages = append(req.Messages, fromAnthropicMessage(m))
	}
	for _, t := range in.Tools {
		req.Tools = append(req.Tools, ir.Tool{Name: t.Name, Description: t.Description, Parameters: t.InputSchema})
	}
	return req, nil
}

func (a MessagesAdapter) EncodeUnary(ctx context.Context, w http.ResponseWriter, resp *ir.Response) error {
	out := map[string]any{"id": resp.ID, "type": "message", "role": "assistant", "model": resp.Model, "content": toAnthropicBlocks(lastContent(resp)), "stop_reason": stopReason(resp.StopReason), "usage": usage(resp.Usage)}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(out)
}

func (a MessagesAdapter) EncodeStream(ctx context.Context, w http.ResponseWriter, events <-chan ir.StreamEvent) error {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	messageID := "msg_" + uuid.NewString()
	writeEvent(w, "message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": messageID, "type": "message", "role": "assistant", "content": []any{}, "model": "", "stop_reason": nil, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}}})
	flush(flusher)
	blockOpen := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				if blockOpen {
					writeEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
				}
				writeEvent(w, "message_stop", map[string]any{"type": "message_stop"})
				flush(flusher)
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
				if !blockOpen {
					writeEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
					blockOpen = true
				}
				writeEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": ev.Delta.Text}})
				flush(flusher)
			case ir.EventUsageDelta:
				if ev.Usage != nil {
					writeEvent(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{}, "usage": usage(*ev.Usage)})
					flush(flusher)
				}
			case ir.EventMessageDone:
				if blockOpen {
					writeEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
					blockOpen = false
				}
				writeEvent(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}})
				writeEvent(w, "message_stop", map[string]any{"type": "message_stop"})
				flush(flusher)
				return nil
			}
		}
	}
}

func (a MessagesAdapter) EncodeError(ctx context.Context, w http.ResponseWriter, err ir.GatewayError) error {
	if err.StatusCode == 0 {
		err.StatusCode = 500
	}
	w.Header().Set("Content-Type", "application/json")
	if err.RetryAfter != "" {
		w.Header().Set("Retry-After", err.RetryAfter)
	}
	w.WriteHeader(err.StatusCode)
	return json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]any{"type": anthropicErrorType(err), "message": err.Message}})
}

func fromAnthropicMessage(m message) ir.Message {
	out := ir.Message{Role: ir.Role(m.Role)}
	for _, b := range m.Content {
		switch b.Type {
		case "text":
			out.Content = append(out.Content, ir.ContentBlock{Type: ir.ContentText, Text: b.Text})
		case "image":
			if b.Source != nil {
				out.Content = append(out.Content, ir.ContentBlock{Type: ir.ContentImage, Image: &ir.ImageContent{MediaType: b.Source.MediaType, Base64: b.Source.Data, URL: b.Source.URL}})
			}
		case "tool_use":
			out.Content = append(out.Content, ir.ContentBlock{Type: ir.ContentToolCall, ToolCall: &ir.ToolCall{ID: b.ID, Name: b.Name, Arguments: b.Input}})
		case "tool_result":
			out.Role = ir.RoleTool
			out.Content = append(out.Content, ir.ContentBlock{Type: ir.ContentToolResult, ToolResult: &ir.ToolResult{ToolCallID: b.ToolUseID, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: b.Content}}}})
		}
	}
	return out
}
func toAnthropicBlocks(blocks []ir.ContentBlock) []map[string]any {
	out := []map[string]any{}
	for _, b := range blocks {
		switch b.Type {
		case ir.ContentText:
			out = append(out, map[string]any{"type": "text", "text": b.Text})
		case ir.ContentReasoning:
			out = append(out, map[string]any{"type": "thinking", "thinking": b.Text})
		case ir.ContentToolCall:
			if b.ToolCall != nil {
				out = append(out, map[string]any{"type": "tool_use", "id": b.ToolCall.ID, "name": b.ToolCall.Name, "input": json.RawMessage(b.ToolCall.Arguments)})
			}
		}
	}
	if len(out) == 0 {
		out = append(out, map[string]any{"type": "text", "text": ""})
	}
	return out
}
func lastContent(resp *ir.Response) []ir.ContentBlock {
	if len(resp.Messages) == 0 {
		return nil
	}
	return resp.Messages[len(resp.Messages)-1].Content
}
func usage(u ir.Usage) map[string]any {
	return map[string]any{"input_tokens": u.PromptTokens, "output_tokens": u.CompletionTokens, "cache_read_input_tokens": u.CacheReadTokens, "cache_creation_input_tokens": u.CacheWriteTokens}
}
func stopReason(s string) string {
	if s == "" {
		return "end_turn"
	}
	return s
}
func anthropicErrorType(err ir.GatewayError) string {
	if err.StatusCode == 429 {
		return "rate_limit_error"
	}
	if err.StatusCode >= 500 {
		return "api_error"
	}
	return "invalid_request_error"
}
func writeEvent(w http.ResponseWriter, event string, payload any) {
	b, _ := json.Marshal(payload)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}
func flush(f http.Flusher) {
	if f != nil {
		f.Flush()
	}
}

type request struct {
	Model         string    `json:"model"`
	MaxTokens     int64     `json:"max_tokens"`
	System        []block   `json:"system"`
	Messages      []message `json:"messages"`
	Tools         []tool    `json:"tools"`
	Temperature   *float64  `json:"temperature"`
	TopP          *float64  `json:"top_p"`
	StopSequences []string  `json:"stop_sequences"`
	Stream        bool      `json:"stream"`
}
type message struct {
	Role    string  `json:"role"`
	Content []block `json:"content"`
}
type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Source    *source         `json:"source,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}
type source struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}
type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

var _ = strings.Builder{}
