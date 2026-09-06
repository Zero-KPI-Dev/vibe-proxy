package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/protocol"
)

type Provider struct{}

func (p Provider) Name() string          { return "anthropic" }
func (p Provider) Protocol() ir.Protocol { return ir.ProtocolAnthropicMessages }
func (p Provider) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{Streaming: true, Tools: true, Vision: true, PromptCache: true, TrailingUsage: true}
}

func (p Provider) BuildRequest(ctx context.Context, req *ir.Request, target modelresolver.Target) (*http.Request, error) {
	out := anthropicRequest{Model: target.Model, Stream: req.Stream, MaxTokens: 1024, StopSequences: req.Stop}
	if req.MaxTokens != nil {
		out.MaxTokens = int64(*req.MaxTokens)
	}
	out.Temperature = req.Temperature
	out.TopP = req.TopP
	for _, m := range req.Messages {
		switch m.Role {
		case ir.RoleSystem:
			out.System = append(out.System, toBlocks(m.Content)...)
		case ir.RoleUser, ir.RoleAssistant:
			out.Messages = append(out.Messages, anthropicMessage{Role: string(m.Role), Content: toBlocks(m.Content)})
		case ir.RoleTool:
			out.Messages = append(out.Messages, anthropicMessage{Role: "user", Content: toBlocks(m.Content)})
		}
	}
	for _, tool := range req.Tools {
		schema := tool.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out.Tools = append(out.Tools, anthropicTool{Name: tool.Name, Description: tool.Description, InputSchema: schema})
	}
	out.ToolChoice = anthropicToolChoiceFromIR(req.ToolChoice)
	body, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(target.BaseURL, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("anthropic-version", "2023-06-01")
	if req.Stream {
		hreq.Header.Set("Accept", "text/event-stream")
	}
	return hreq, nil
}

func (p Provider) ParseUnary(ctx context.Context, resp *http.Response) (*ir.Response, error) {
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, p.NormalizeError(ctx, resp)
	}
	var ar anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return nil, ir.GatewayError{StatusCode: 502, Kind: "upstream_error", Code: "invalid_upstream_response", Message: "Upstream returned an invalid response."}
	}
	return &ir.Response{ID: ar.ID, Model: ar.Model, StopReason: ar.StopReason, Messages: []ir.Message{{Role: ir.RoleAssistant, Content: fromBlocks(ar.Content)}}, Usage: usageFromAnthropic(ar.Usage)}, nil
}

func (p Provider) ParseStream(ctx context.Context, resp *http.Response) (<-chan ir.StreamEvent, error) {
	if resp.StatusCode >= 400 {
		return nil, p.NormalizeError(ctx, resp)
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "text/event-stream") {
		resp.Body.Close()
		return nil, ir.GatewayError{StatusCode: 502, Kind: "upstream_error", Code: "invalid_upstream_stream", Message: "Upstream did not return an event stream."}
	}
	stopRead := context.AfterFunc(ctx, func() { _ = resp.Body.Close() })
	out := make(chan ir.StreamEvent, 32)
	go func() {
		defer close(out)
		defer stopRead()
		defer resp.Body.Close()
		reader := bufio.NewScanner(resp.Body)
		reader.Buffer(make([]byte, 64<<10), 1<<20)
		var eventName string
		toolCalls := map[int]ir.ToolCall{}
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if !reader.Scan() {
				if ctx.Err() == nil {
					emitStreamEvent(ctx, out, ir.StreamEvent{Type: ir.EventError, Time: time.Now(), Error: &ir.GatewayError{StatusCode: 502, Kind: "upstream_error", Code: "stream_interrupted", Message: "Upstream stream interrupted."}})
				}
				return
			}
			line := strings.TrimRight(reader.Text(), "\r\n")
			if strings.HasPrefix(line, "event:") {
				eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			var frame map[string]json.RawMessage
			if json.Unmarshal([]byte(data), &frame) != nil {
				emitStreamEvent(ctx, out, ir.StreamEvent{Type: ir.EventError, Error: &ir.GatewayError{StatusCode: 502, Kind: "upstream_error", Code: "invalid_upstream_stream", Message: "Upstream returned a malformed stream frame."}})
				return
			}
			if eventName == "error" || (len(frame["error"]) > 0 && string(frame["error"]) != "null") {
				emitStreamEvent(ctx, out, ir.StreamEvent{Type: ir.EventError, Error: &ir.GatewayError{StatusCode: 502, Kind: "upstream_error", Code: "upstream_stream_error", Message: "Upstream reported an error while streaming."}})
				return
			}
			switch eventName {
			case "message_start":
				if !emitStreamEvent(ctx, out, ir.StreamEvent{Type: ir.EventMessageStart, Time: time.Now(), Raw: []byte(data)}) {
					return
				}
				if usage := parseMessageStartUsage(frame["message"]); usage.TotalTokens > 0 || usage.PromptTokens > 0 {
					if !emitStreamEvent(ctx, out, ir.StreamEvent{Type: ir.EventUsageDelta, Time: time.Now(), Usage: &usage, Raw: []byte(data)}) {
						return
					}
				}
			case "content_block_start":
				index, block := parseContentBlockStart(data)
				if block.Type == "tool_use" {
					arguments := block.Input
					if string(arguments) == "{}" {
						arguments = nil
					}
					call := ir.ToolCall{ID: block.ID, Name: block.Name, Arguments: arguments}
					toolCalls[index] = call
					if !emitStreamEvent(ctx, out, ir.StreamEvent{Type: ir.EventToolCallStart, Time: time.Now(), Index: index, ToolCall: &call, Raw: []byte(data)}) {
						return
					}
				}
			case "content_block_delta":
				index, delta := parseContentBlockDelta(data)
				if delta.Text != "" || delta.Thinking != "" {
					text := delta.Text
					eventType := ir.EventContentDelta
					contentType := ir.ContentText
					if text == "" {
						text = delta.Thinking
						eventType = ir.EventReasoningDelta
						contentType = ir.ContentReasoning
					}
					if !emitStreamEvent(ctx, out, ir.StreamEvent{Type: eventType, Time: time.Now(), Index: index, Delta: ir.ContentBlock{Type: contentType, Text: text}, Raw: []byte(data)}) {
						return
					}
				}
				if delta.PartialJSON != "" {
					call := toolCalls[index]
					fragment := ir.ToolCall{ID: call.ID, Name: call.Name, Arguments: json.RawMessage(delta.PartialJSON)}
					call.Arguments = append(call.Arguments, delta.PartialJSON...)
					toolCalls[index] = call
					if !emitStreamEvent(ctx, out, ir.StreamEvent{Type: ir.EventToolCallDelta, Time: time.Now(), Index: index, ToolCall: &fragment, Raw: []byte(data)}) {
						return
					}
				}
			case "content_block_stop":
				index := parseContentBlockIndex(data)
				if call, exists := toolCalls[index]; exists {
					if !emitStreamEvent(ctx, out, ir.StreamEvent{Type: ir.EventToolCallDone, Time: time.Now(), Index: index, ToolCall: &call, Raw: []byte(data)}) {
						return
					}
					delete(toolCalls, index)
				}
			case "message_delta":
				u := parseUsage(frame["usage"])
				if u.TotalTokens > 0 || u.PromptTokens > 0 || u.CompletionTokens > 0 {
					if !emitStreamEvent(ctx, out, ir.StreamEvent{Type: ir.EventUsageDelta, Time: time.Now(), Usage: &u, Raw: []byte(data)}) {
						return
					}
				}
			case "message_stop":
				emitStreamEvent(ctx, out, ir.StreamEvent{Type: ir.EventMessageDone, Time: time.Now(), Raw: []byte(data)})
				return
			}
		}
	}()
	return out, nil
}

func emitStreamEvent(ctx context.Context, out chan<- ir.StreamEvent, event ir.StreamEvent) bool {
	select {
	case out <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func (p Provider) NormalizeError(ctx context.Context, resp *http.Response) ir.GatewayError {
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == 429 {
		return ir.GatewayError{StatusCode: 429, Kind: "rate_limit_error", Code: "upstream_rate_limited", Message: "Upstream rate limit exceeded.", RetryAfter: resp.Header.Get("Retry-After")}
	}
	if resp.StatusCode >= 500 {
		return ir.GatewayError{StatusCode: resp.StatusCode, Kind: "upstream_error", Code: "upstream_unavailable", Message: "Upstream service is temporarily unavailable."}
	}
	return ir.GatewayError{StatusCode: resp.StatusCode, Kind: "upstream_error", Code: "upstream_rejected", Message: "The upstream provider rejected the request."}
}

func toBlocks(blocks []ir.ContentBlock) []contentBlock {
	out := []contentBlock{}
	for _, b := range blocks {
		switch b.Type {
		case ir.ContentText:
			out = append(out, contentBlock{Type: "text", Text: b.Text})
		case ir.ContentReasoning:
			// Reasoning captured from an earlier response is not ordinary
			// conversation text. Replaying it as a text block both exposes
			// private reasoning and changes the meaning of the next request.
			// Anthropic thinking blocks require provider-owned metadata such as
			// signatures, so omit untrusted IR reasoning instead of fabricating
			// a replayable thinking block.
			continue
		case ir.ContentImage:
			if b.Image != nil {
				src := &source{Type: "base64", MediaType: b.Image.MediaType, Data: b.Image.Base64}
				if b.Image.Base64 == "" {
					src = &source{Type: "url", URL: b.Image.URL}
				}
				out = append(out, contentBlock{Type: "image", Source: src})
			}
		case ir.ContentToolCall:
			if b.ToolCall != nil {
				out = append(out, contentBlock{Type: "tool_use", ID: b.ToolCall.ID, Name: b.ToolCall.Name, Input: b.ToolCall.Arguments})
			}
		case ir.ContentToolResult:
			if b.ToolResult != nil {
				out = append(out, contentBlock{Type: "tool_result", ToolUseID: b.ToolResult.ToolCallID, Content: flatten(b.ToolResult.Content)})
			}
		}
	}
	if len(out) == 0 {
		out = append(out, contentBlock{Type: "text", Text: ""})
	}
	return out
}
func fromBlocks(blocks []contentBlock) []ir.ContentBlock {
	out := []ir.ContentBlock{}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			out = append(out, ir.ContentBlock{Type: ir.ContentText, Text: b.Text})
		case "thinking":
			out = append(out, ir.ContentBlock{Type: ir.ContentReasoning, Text: b.Thinking})
		case "tool_use":
			arguments := b.Input
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			out = append(out, ir.ContentBlock{Type: ir.ContentToolCall, ToolCall: &ir.ToolCall{ID: b.ID, Name: b.Name, Arguments: arguments}})
		}
	}
	return out
}
func flatten(blocks []ir.ContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == ir.ContentText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}
func parseMessageStartUsage(raw json.RawMessage) ir.Usage {
	var message struct {
		Usage anthropicUsage `json:"usage"`
	}
	_ = json.Unmarshal(raw, &message)
	return usageFromAnthropic(message.Usage)
}
func parseContentBlockStart(raw string) (int, contentBlock) {
	var frame struct {
		Index        int          `json:"index"`
		ContentBlock contentBlock `json:"content_block"`
	}
	_ = json.Unmarshal([]byte(raw), &frame)
	return frame.Index, frame.ContentBlock
}

type contentBlockDelta struct {
	Text        string `json:"text"`
	Thinking    string `json:"thinking"`
	PartialJSON string `json:"partial_json"`
}

func parseContentBlockDelta(raw string) (int, contentBlockDelta) {
	var frame struct {
		Index int               `json:"index"`
		Delta contentBlockDelta `json:"delta"`
	}
	_ = json.Unmarshal([]byte(raw), &frame)
	return frame.Index, frame.Delta
}
func parseContentBlockIndex(raw string) int {
	var frame struct {
		Index int `json:"index"`
	}
	_ = json.Unmarshal([]byte(raw), &frame)
	return frame.Index
}
func parseUsage(raw json.RawMessage) ir.Usage {
	var u anthropicUsage
	json.Unmarshal(raw, &u)
	return usageFromAnthropic(u)
}
func usageFromAnthropic(u anthropicUsage) ir.Usage {
	cacheRead := pointerValue(u.CacheReadInputTokens)
	cacheWrite := pointerValue(u.CacheCreationInputTokens)
	cacheReported := u.CacheReadInputTokens != nil || u.CacheCreationInputTokens != nil
	r := ir.Usage{
		PromptTokens:         u.InputTokens + cacheRead + cacheWrite,
		CompletionTokens:     u.OutputTokens,
		CacheReadTokens:      cacheRead,
		CacheWriteTokens:     cacheWrite,
		CacheMetricsReported: cacheReported,
	}
	r.TotalTokens = r.PromptTokens + r.CompletionTokens
	if cacheReported && r.PromptTokens > 0 {
		r.CacheHitRatio = float64(r.CacheReadTokens) / float64(r.PromptTokens)
	}
	return r
}

func pointerValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

type anthropicRequest struct {
	Model         string               `json:"model"`
	MaxTokens     int64                `json:"max_tokens"`
	System        []contentBlock       `json:"system,omitempty"`
	Messages      []anthropicMessage   `json:"messages"`
	Temperature   *float64             `json:"temperature,omitempty"`
	TopP          *float64             `json:"top_p,omitempty"`
	Stream        bool                 `json:"stream"`
	StopSequences []string             `json:"stop_sequences,omitempty"`
	Tools         []anthropicTool      `json:"tools,omitempty"`
	ToolChoice    *anthropicToolChoice `json:"tool_choice,omitempty"`
}
type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}
type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}
type anthropicMessage struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}
type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
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
type anthropicResponse struct {
	ID         string         `json:"id"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	Content    []contentBlock `json:"content"`
	Usage      anthropicUsage `json:"usage"`
}
type anthropicUsage struct {
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
}

func anthropicToolChoiceFromIR(choice *ir.ToolChoice) *anthropicToolChoice {
	if choice == nil || choice.Type == "" {
		return nil
	}
	switch choice.Type {
	case "required":
		return &anthropicToolChoice{Type: "any"}
	case "function":
		return &anthropicToolChoice{Type: "tool", Name: choice.Name}
	default:
		return &anthropicToolChoice{Type: choice.Type, Name: choice.Name}
	}
}
