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
	out := anthropicRequest{Model: target.Model, Stream: req.Stream, MaxTokens: 1024}
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
	out := make(chan ir.StreamEvent, 32)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		reader := bufio.NewReader(resp.Body)
		var eventName string
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
			line = strings.TrimRight(line, "\r\n")
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
				continue
			}
			switch eventName {
			case "message_start":
				out <- ir.StreamEvent{Type: ir.EventMessageStart, Time: time.Now(), Raw: []byte(data)}
			case "content_block_delta":
				if txt := parseTextDelta(frame["delta"]); txt != "" {
					out <- ir.StreamEvent{Type: ir.EventContentDelta, Time: time.Now(), Delta: ir.ContentBlock{Type: ir.ContentText, Text: txt}, Raw: []byte(data)}
				}
			case "message_delta":
				u := parseUsage(frame["usage"])
				if u.TotalTokens > 0 || u.PromptTokens > 0 || u.CompletionTokens > 0 {
					out <- ir.StreamEvent{Type: ir.EventUsageDelta, Time: time.Now(), Usage: &u, Raw: []byte(data)}
				}
			case "message_stop":
				out <- ir.StreamEvent{Type: ir.EventMessageDone, Time: time.Now(), Raw: []byte(data)}
				return
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

func toBlocks(blocks []ir.ContentBlock) []contentBlock {
	out := []contentBlock{}
	for _, b := range blocks {
		switch b.Type {
		case ir.ContentText, ir.ContentReasoning:
			out = append(out, contentBlock{Type: "text", Text: b.Text})
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
func parseTextDelta(raw json.RawMessage) string {
	var d struct {
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
	}
	json.Unmarshal(raw, &d)
	if d.Text != "" {
		return d.Text
	}
	return d.Thinking
}
func parseUsage(raw json.RawMessage) ir.Usage {
	var u anthropicUsage
	json.Unmarshal(raw, &u)
	return usageFromAnthropic(u)
}
func usageFromAnthropic(u anthropicUsage) ir.Usage {
	r := ir.Usage{PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens, CacheReadTokens: u.CacheReadInputTokens, CacheWriteTokens: u.CacheCreationInputTokens}
	r.TotalTokens = r.PromptTokens + r.CompletionTokens
	denom := r.CacheReadTokens + r.CacheWriteTokens
	if denom > 0 {
		r.CacheHitRatio = float64(r.CacheReadTokens) / float64(denom)
	}
	return r
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int64              `json:"max_tokens"`
	System      []contentBlock     `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	Stream      bool               `json:"stream"`
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
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}
