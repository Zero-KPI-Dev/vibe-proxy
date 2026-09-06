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

	"github.com/a448582655/vibe-proxy/internal/httpstream"
	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/protocol"
)

type ChatAdapter struct{}

func (a ChatAdapter) Name() string                { return "openai-chat" }
func (a ChatAdapter) Protocol() ir.Protocol       { return ir.ProtocolOpenAIChat }
func (a ChatAdapter) Detect(r *http.Request) bool { return r.URL.Path == "/v1/chat/completions" }

func (a ChatAdapter) ParseRequest(ctx context.Context, r *http.Request) (*ir.Request, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		return nil, ir.GatewayError{StatusCode: 400, Kind: "invalid_request", Code: "body_read_failed", Message: "Could not read request body."}
	}
	var in chatRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, ir.GatewayError{StatusCode: 400, Kind: "invalid_request", Code: "invalid_json", Message: "Request body is not valid JSON."}
	}
	if in.Model == "" {
		return nil, ir.GatewayError{StatusCode: 400, Kind: "invalid_request", Code: "missing_model", Message: "Request JSON must include a model."}
	}
	maxTokens := intPtrFromInt64(in.MaxTokens)
	req := &ir.Request{ID: uuid.NewString(), ClientProtocol: ir.ProtocolOpenAIChat, RequestedModel: in.Model, Stream: in.Stream, MaxTokens: maxTokens, Temperature: in.Temperature, TopP: in.TopP, Stop: stringSlice(in.Stop), VendorExtensions: map[string]any{"openai_raw": json.RawMessage(body)}}
	for _, m := range in.Messages {
		req.Messages = append(req.Messages, toIRMessage(m))
	}
	for _, t := range in.Tools {
		req.Tools = append(req.Tools, ir.Tool{Name: t.Function.Name, Description: t.Function.Description, Parameters: t.Function.Parameters})
	}
	req.ToolChoice = parseOpenAIToolChoice(in.ToolChoice)
	if len(in.ResponseFormat) > 0 {
		var format struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(in.ResponseFormat, &format) == nil && format.Type != "" {
			req.ResponseFormat = &ir.ResponseFormat{Type: format.Type, JSONSchema: append(json.RawMessage(nil), in.ResponseFormat...)}
		}
	}
	return req, nil
}

func (a ChatAdapter) EncodeUnary(ctx context.Context, w http.ResponseWriter, resp *ir.Response) error {
	content := ""
	reasoning := ""
	var toolCalls []any
	if len(resp.Messages) > 0 {
		blocks := resp.Messages[len(resp.Messages)-1].Content
		content = contentText(blocks)
		reasoning = reasoningText(blocks)
		toolCalls = openAIToolCalls(blocks)
	}
	message := map[string]any{"role": "assistant", "content": content}
	if reasoning != "" {
		message["reasoning_content"] = reasoning
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
		if content == "" {
			message["content"] = nil
		}
	}
	out := map[string]any{"id": resp.ID, "object": "chat.completion", "created": time.Now().Unix(), "model": resp.Model, "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finishReason(resp.StopReason, len(toolCalls) > 0)}}, "usage": openAIUsage(resp.Usage)}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(out)
}

func (a ChatAdapter) EncodeStream(ctx context.Context, w http.ResponseWriter, events <-chan ir.StreamEvent) error {
	writer := httpstream.NewWriter(ctx, w, 0)
	defer writer.Close()
	w = writer

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	id := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()
	model := ""
	sawToolCall := false
	toolIndexes := map[int]int{}
	var usage ir.Usage
	for {
		if err := writer.Err(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return protocol.UnexpectedStreamEnd(ctx)
			}
			if ev.Error != nil {
				return *ev.Error
			}
			switch ev.Type {
			case ir.EventMessageStart:
				if ev.MessageID != "" {
					id = ev.MessageID
				}
			case ir.EventContentDelta:
				text := ev.Delta.Text
				if text == "" {
					continue
				}
				writeSSE(w, streamChunk(id, created, model, text, ""))
				if flusher != nil {
					flusher.Flush()
				}
			case ir.EventReasoningDelta:
				if ev.Delta.Text == "" {
					continue
				}
				writeSSE(w, reasoningStreamChunk(id, created, model, ev.Delta.Text))
				if flusher != nil {
					flusher.Flush()
				}
			case ir.EventToolCallStart, ir.EventToolCallDelta:
				if ev.ToolCall == nil {
					continue
				}
				sawToolCall = sawToolCall || ev.ToolCall != nil
				index, exists := toolIndexes[ev.Index]
				if !exists {
					index = len(toolIndexes)
					toolIndexes[ev.Index] = index
				}
				writeSSE(w, toolChunk(id, created, model, index, ev.ToolCall))
				if flusher != nil {
					flusher.Flush()
				}
			case ir.EventUsageDelta:
				if ev.Usage != nil {
					usage = mergeStreamUsage(usage, *ev.Usage)
				}
			case ir.EventMessageDone:
				if usage.TotalTokens > 0 || usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
					writeSSE(w, streamUsageChunk(id, created, model, usage))
				}
				finish := "stop"
				if sawToolCall {
					finish = "tool_calls"
				}
				writeSSE(w, streamChunk(id, created, model, "", finish))
				writeRawSSE(w, "[DONE]")
				if flusher != nil {
					flusher.Flush()
				}
				return writer.Err()
			}
		}
	}
}

func (a ChatAdapter) EncodeError(ctx context.Context, w http.ResponseWriter, err ir.GatewayError) error {
	if err.StatusCode == 0 {
		err.StatusCode = 500
	}
	w.Header().Set("Content-Type", "application/json")
	if err.RetryAfter != "" {
		w.Header().Set("Retry-After", err.RetryAfter)
	}
	w.WriteHeader(err.StatusCode)
	return json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": err.Message, "type": openAIErrorType(err), "param": nil, "code": err.Code}})
}

func toIRMessage(m chatMessage) ir.Message {
	msg := ir.Message{Role: ir.Role(m.Role), Name: m.Name}
	if len(m.ToolCalls) > 0 {
		for _, tc := range m.ToolCalls {
			msg.Content = append(msg.Content, ir.ContentBlock{Type: ir.ContentToolCall, ToolCall: &ir.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: json.RawMessage(tc.Function.Arguments)}})
		}
	}
	if m.Role == "tool" {
		msg.Role = ir.RoleTool
		msg.Content = []ir.ContentBlock{{Type: ir.ContentToolResult, ToolResult: &ir.ToolResult{ToolCallID: m.ToolCallID, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: m.Text()}}}}}
		return msg
	}
	msg.Content = append(msg.Content, openAIContentToIR(m.Content)...)
	return msg
}

func openAIContentToIR(content any) []ir.ContentBlock {
	switch c := content.(type) {
	case string:
		return []ir.ContentBlock{{Type: ir.ContentText, Text: c}}
	case []any:
		blocks := make([]ir.ContentBlock, 0, len(c))
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			switch t {
			case "text":
				if txt, _ := m["text"].(string); txt != "" {
					blocks = append(blocks, ir.ContentBlock{Type: ir.ContentText, Text: txt})
				}
			case "image_url":
				img, _ := m["image_url"].(map[string]any)
				url, _ := img["url"].(string)
				blocks = append(blocks, ir.ContentBlock{Type: ir.ContentImage, Image: &ir.ImageContent{URL: url}})
			case "file":
				blocks = append(blocks, ir.ContentBlock{Type: ir.ContentFile, VendorExtensions: map[string]any{"openai": m}})
			}
		}
		return blocks
	default:
		return []ir.ContentBlock{{Type: ir.ContentText, Text: ""}}
	}
}

func contentText(blocks []ir.ContentBlock) string {
	var b strings.Builder
	for _, c := range blocks {
		if c.Type == ir.ContentText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}
func reasoningText(blocks []ir.ContentBlock) string {
	var b strings.Builder
	for _, c := range blocks {
		if c.Type == ir.ContentReasoning {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}
func finishReason(s string, hasToolCalls bool) string {
	switch s {
	case "tool_use", "tool_calls":
		return "tool_calls"
	case "max_tokens", "length":
		return "length"
	case "", "end_turn", "stop_sequence", "stop":
		if hasToolCalls {
			return "tool_calls"
		}
		return "stop"
	default:
		return s
	}
}
func intPtrFromInt64(v int64) *int {
	if v == 0 {
		return nil
	}
	i := int(v)
	return &i
}
func stringSlice(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		out := []string{}
		for _, it := range x {
			if s, ok := it.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
func openAIUsage(u ir.Usage) map[string]any {
	result := map[string]any{"prompt_tokens": u.PromptTokens, "completion_tokens": u.CompletionTokens, "total_tokens": u.TotalTokens}
	if u.CacheMetricsReported {
		result["prompt_tokens_details"] = map[string]any{"cached_tokens": u.CacheReadTokens}
	}
	return result
}
func mergeStreamUsage(a, b ir.Usage) ir.Usage {
	if b.PromptTokens != 0 {
		a.PromptTokens = b.PromptTokens
	}
	if b.CompletionTokens != 0 {
		a.CompletionTokens = b.CompletionTokens
	}
	if b.TotalTokens != 0 {
		a.TotalTokens = b.TotalTokens
	}
	if b.ReasoningTokens != 0 {
		a.ReasoningTokens = b.ReasoningTokens
	}
	if b.CacheReadTokens != 0 {
		a.CacheReadTokens = b.CacheReadTokens
	}
	if b.CacheWriteTokens != 0 {
		a.CacheWriteTokens = b.CacheWriteTokens
	}
	if b.CacheMetricsReported {
		a.CacheMetricsReported = true
	}
	if b.CacheHitRatio != 0 || b.CacheMetricsReported {
		a.CacheHitRatio = b.CacheHitRatio
	}
	if a.TotalTokens == 0 {
		a.TotalTokens = a.PromptTokens + a.CompletionTokens
	}
	return a
}
func openAIToolCalls(blocks []ir.ContentBlock) []any {
	out := []any{}
	for _, block := range blocks {
		if block.Type != ir.ContentToolCall || block.ToolCall == nil {
			continue
		}
		out = append(out, map[string]any{
			"id":   block.ToolCall.ID,
			"type": "function",
			"function": map[string]any{
				"name":      block.ToolCall.Name,
				"arguments": string(block.ToolCall.Arguments),
			},
		})
	}
	return out
}
func openAIErrorType(err ir.GatewayError) string {
	if err.Kind != "" {
		return err.Kind
	}
	if err.StatusCode == 429 {
		return "rate_limit_error"
	}
	if err.StatusCode >= 500 {
		return "server_error"
	}
	return "invalid_request_error"
}
func writeSSE(w http.ResponseWriter, payload any) {
	b, _ := json.Marshal(payload)
	fmt.Fprintf(w, "data: %s\n\n", b)
}
func writeRawSSE(w http.ResponseWriter, payload string) { fmt.Fprintf(w, "data: %s\n\n", payload) }
func streamChunk(id string, created int64, model, text, finish string) map[string]any {
	delta := map[string]any{}
	if text != "" {
		delta["content"] = text
	}
	choice := map[string]any{"index": 0, "delta": delta, "finish_reason": nil}
	if finish != "" {
		choice["finish_reason"] = finish
	}
	return map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{choice}}
}
func streamUsageChunk(id string, created int64, model string, usage ir.Usage) map[string]any {
	return map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{}, "usage": openAIUsage(usage)}
}
func reasoningStreamChunk(id string, created int64, model, text string) map[string]any {
	return map[string]any{
		"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning_content": text}, "finish_reason": nil}},
	}
}
func toolChunk(id string, created int64, model string, index int, tc *ir.ToolCall) map[string]any {
	delta := map[string]any{}
	if tc != nil {
		function := map[string]any{"arguments": string(tc.Arguments)}
		if tc.Name != "" {
			function["name"] = tc.Name
		}
		call := map[string]any{"index": index, "function": function}
		if tc.ID != "" {
			call["id"], call["type"] = tc.ID, "function"
		}
		delta["tool_calls"] = []any{call}
	}
	return map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}}
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Stream         bool            `json:"stream"`
	MaxTokens      int64           `json:"max_tokens"`
	Temperature    *float64        `json:"temperature"`
	TopP           *float64        `json:"top_p"`
	Stop           any             `json:"stop"`
	Tools          []chatTool      `json:"tools"`
	ToolChoice     any             `json:"tool_choice"`
	ResponseFormat json.RawMessage `json:"response_format"`
}
type chatMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content"`
	Name       string         `json:"name"`
	ToolCallID string         `json:"tool_call_id"`
	ToolCalls  []chatToolCall `json:"tool_calls"`
}

func (m chatMessage) Text() string {
	if s, ok := m.Content.(string); ok {
		return s
	}
	b, _ := json.Marshal(m.Content)
	return string(b)
}

type chatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}
type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func parseOpenAIToolChoice(value any) *ir.ToolChoice {
	switch choice := value.(type) {
	case string:
		if choice == "" {
			return nil
		}
		return &ir.ToolChoice{Type: choice}
	case map[string]any:
		choiceType, _ := choice["type"].(string)
		if choiceType == "" {
			return nil
		}
		if choiceType == "function" {
			function, _ := choice["function"].(map[string]any)
			name, _ := function["name"].(string)
			return &ir.ToolChoice{Type: "tool", Name: name}
		}
		name, _ := choice["name"].(string)
		return &ir.ToolChoice{Type: choiceType, Name: name}
	default:
		return nil
	}
}
