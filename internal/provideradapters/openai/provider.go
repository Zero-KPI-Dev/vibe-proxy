package openai

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

func (p Provider) Name() string          { return "openai-compatible" }
func (p Provider) Protocol() ir.Protocol { return ir.ProtocolOpenAIChat }
func (p Provider) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{Streaming: true, Tools: true, ParallelToolCalls: true, Vision: true, JSONMode: true, TrailingUsage: true}
}

func (p Provider) BuildRequest(ctx context.Context, req *ir.Request, target modelresolver.Target) (*http.Request, error) {
	out := chatRequest{Model: target.Model, Stream: req.Stream, MaxTokens: req.MaxTokens, Temperature: req.Temperature, TopP: req.TopP, Stop: req.Stop}
	for _, m := range req.Messages {
		out.Messages = append(out.Messages, fromIRMessage(m))
	}
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, chatTool{Type: "function", Function: chatFunction{Name: t.Name, Description: t.Description, Parameters: t.Parameters}})
	}
	body, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(target.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
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
	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, ir.GatewayError{StatusCode: 502, Kind: "upstream_error", Code: "invalid_upstream_response", Message: "Upstream returned an invalid response."}
	}
	out := &ir.Response{ID: cr.ID, Model: cr.Model, Usage: ir.Usage{PromptTokens: cr.Usage.PromptTokens, CompletionTokens: cr.Usage.CompletionTokens, TotalTokens: cr.Usage.TotalTokens}}
	if len(cr.Choices) > 0 {
		out.StopReason = cr.Choices[0].FinishReason
		out.Messages = []ir.Message{toIRMessage(cr.Choices[0].Message)}
	}
	return out, nil
}

func (p Provider) ParseStream(ctx context.Context, resp *http.Response) (<-chan ir.StreamEvent, error) {
	if resp.StatusCode >= 400 {
		return nil, p.NormalizeError(ctx, resp)
	}
	out := make(chan ir.StreamEvent, 32)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		reader := bufio.NewReader(resp.Body)
		for {
			select {
			case <-ctx.Done():
				out <- ir.StreamEvent{Type: ir.EventError, Time: time.Now(), Error: &ir.GatewayError{StatusCode: 499, Kind: "canceled", Code: "client_closed", Message: "Client closed the request."}}
				return
			default:
			}
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					out <- ir.StreamEvent{Type: ir.EventError, Time: time.Now(), Error: &ir.GatewayError{StatusCode: 502, Kind: "upstream_error", Code: "stream_interrupted", Message: "Upstream stream interrupted."}}
				}
				return
			}
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				out <- ir.StreamEvent{Type: ir.EventMessageDone, Time: time.Now()}
				return
			}
			var chunk streamChunk
			if json.Unmarshal([]byte(data), &chunk) != nil {
				continue
			}
			for _, ch := range chunk.Choices {
				if ch.Delta.Content != "" {
					out <- ir.StreamEvent{Type: ir.EventContentDelta, Time: time.Now(), Delta: ir.ContentBlock{Type: ir.ContentText, Text: ch.Delta.Content}, Raw: []byte(data)}
				}
				for _, tc := range ch.Delta.ToolCalls {
					out <- ir.StreamEvent{Type: ir.EventToolCallDelta, Time: time.Now(), ToolCall: &ir.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: json.RawMessage(tc.Function.Arguments)}, Raw: []byte(data)}
				}
				if ch.FinishReason != "" {
					out <- ir.StreamEvent{Type: ir.EventMessageDone, Time: time.Now(), Raw: []byte(data)}
					return
				}
			}
			if chunk.Usage.TotalTokens > 0 {
				u := ir.Usage{PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens, TotalTokens: chunk.Usage.TotalTokens}
				out <- ir.StreamEvent{Type: ir.EventUsageDelta, Time: time.Now(), Usage: &u, Raw: []byte(data)}
			}
		}
	}()
	return out, nil
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

func fromIRMessage(m ir.Message) chatMessage {
	cm := chatMessage{Role: string(m.Role), Name: m.Name}
	if m.Role == ir.RoleTool {
		cm.Role = "tool"
		for _, b := range m.Content {
			if b.ToolResult != nil {
				cm.ToolCallID = b.ToolResult.ToolCallID
				cm.Content = flatten(b.ToolResult.Content)
				return cm
			}
		}
	}
	if onlyText(m.Content) {
		cm.Content = flatten(m.Content)
		return cm
	}
	parts := []any{}
	for _, b := range m.Content {
		switch b.Type {
		case ir.ContentText:
			parts = append(parts, map[string]any{"type": "text", "text": b.Text})
		case ir.ContentImage:
			if b.Image != nil {
				url := b.Image.URL
				if url == "" && b.Image.Base64 != "" {
					url = "data:" + b.Image.MediaType + ";base64," + b.Image.Base64
				}
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
			}
		case ir.ContentToolCall:
			if b.ToolCall != nil {
				cm.ToolCalls = append(cm.ToolCalls, chatToolCall{ID: b.ToolCall.ID, Type: "function", Function: chatToolCallFunction{Name: b.ToolCall.Name, Arguments: string(b.ToolCall.Arguments)}})
			}
		}
	}
	cm.Content = parts
	return cm
}

func toIRMessage(m chatMessage) ir.Message {
	msg := ir.Message{Role: ir.Role(m.Role), Name: m.Name}
	if s, ok := m.Content.(string); ok {
		msg.Content = []ir.ContentBlock{{Type: ir.ContentText, Text: s}}
	}
	for _, tc := range m.ToolCalls {
		msg.Content = append(msg.Content, ir.ContentBlock{Type: ir.ContentToolCall, ToolCall: &ir.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: json.RawMessage(tc.Function.Arguments)}})
	}
	return msg
}
func onlyText(blocks []ir.ContentBlock) bool {
	if len(blocks) == 0 {
		return true
	}
	for _, b := range blocks {
		if b.Type != ir.ContentText && b.Type != ir.ContentReasoning {
			return false
		}
	}
	return true
}
func flatten(blocks []ir.ContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == ir.ContentText || b.Type == ir.ContentReasoning {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
	Tools       []chatTool    `json:"tools,omitempty"`
}
type chatMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
}
type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}
type chatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}
type chatToolCall struct {
	ID       string               `json:"id,omitempty"`
	Type     string               `json:"type,omitempty"`
	Function chatToolCallFunction `json:"function"`
}
type chatToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}
type chatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage usage `json:"usage"`
}
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string         `json:"content"`
			ToolCalls []chatToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage usage `json:"usage"`
}
type usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}
