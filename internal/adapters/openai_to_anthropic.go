package adapters

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
	"github.com/a448582655/vibe-proxy/internal/types"
)

type OpenAIToAnthropic struct{ HTTPClient *http.Client }

func (a *OpenAIToAnthropic) Protocol() types.Protocol { return types.ProtocolAnthropicMessage }

func (a *OpenAIToAnthropic) TransformRequest(ctx context.Context, req ProxyRequest, ch config.ChannelConfig, apiKey string) (*http.Request, *types.GatewayError) {
	var in openAIChatRequest
	if err := json.Unmarshal(req.RawBody, &in); err != nil {
		return nil, &types.GatewayError{StatusCode: 400, Type: "invalid_request_error", Code: "invalid_json", Message: "Request body is not valid JSON."}
	}
	if req.UpstreamModel == "" {
		req.UpstreamModel = in.Model
	}
	out := anthropicRequest{Model: req.UpstreamModel, MaxTokens: int64(in.MaxTokens), Temperature: in.Temperature, TopP: in.TopP, Stream: in.Stream}
	if out.MaxTokens == 0 {
		out.MaxTokens = 1024
	}
	applyOverrides(&out, req.Overrides)
	for _, m := range in.Messages {
		switch m.Role {
		case "system":
			out.System = append(out.System, contentBlock{Type: "text", Text: m.Text()})
		case "user", "assistant":
			blocks, err := openAIContentToAnthropic(ctx, m.Content)
			if err != nil {
				return nil, &types.GatewayError{StatusCode: 400, Type: "invalid_request_error", Code: "invalid_multimodal_content", Message: "Multimodal content could not be transformed."}
			}
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					blocks = append(blocks, contentBlock{Type: "tool_use", ID: tc.ID, Name: tc.Function.Name, Input: json.RawMessage(tc.Function.Arguments)})
				}
			}
			out.Messages = append(out.Messages, anthropicMessage{Role: m.Role, Content: blocks})
		case "tool":
			out.Messages = append(out.Messages, anthropicMessage{Role: "user", Content: []contentBlock{{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Text()}}})
		}
	}
	body, err := json.Marshal(out)
	if err != nil {
		return nil, &types.GatewayError{StatusCode: 500, Type: "server_error", Code: "transform_failed", Message: "Request transformation failed."}
	}
	url := strings.TrimRight(ch.BaseURL, "/") + "/v1/messages"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, &types.GatewayError{StatusCode: 500, Type: "server_error", Code: "request_build_failed", Message: "Could not build upstream request."}
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "application/json")
	if in.Stream {
		hreq.Header.Set("Accept", "text/event-stream")
	}
	hreq.Header.Set("x-api-key", apiKey)
	hreq.Header.Set("anthropic-version", "2023-06-01")
	return hreq, nil
}

func (a *OpenAIToAnthropic) HandleResponse(ctx context.Context, upstream *http.Response, client http.ResponseWriter, req ProxyRequest, tracker *telemetry.Tracker) *types.GatewayError {
	defer upstream.Body.Close()
	if upstream.StatusCode >= 400 {
		io.Copy(io.Discard, io.LimitReader(upstream.Body, 4096))
		return &types.GatewayError{StatusCode: upstream.StatusCode, Type: classifyHTTP(upstream.StatusCode), Code: "upstream_error", Message: safeHTTPMessage(upstream.StatusCode), RetryAfter: upstream.Header.Get("Retry-After")}
	}
	if req.Stream {
		return a.streamAnthropicAsOpenAI(ctx, upstream, client, req, tracker)
	}
	return a.unaryAnthropicAsOpenAI(upstream, client, req, tracker)
}

func (a *OpenAIToAnthropic) NormalizeError(err *types.GatewayError) []byte {
	return OpenAIErrorJSON(err)
}

func (a *OpenAIToAnthropic) streamAnthropicAsOpenAI(ctx context.Context, upstream *http.Response, client http.ResponseWriter, req ProxyRequest, tracker *telemetry.Tracker) *types.GatewayError {
	h := client.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	client.WriteHeader(http.StatusOK)
	flusher, _ := client.(http.Flusher)
	id := "chatcmpl-" + tracker.Event.RequestID
	created := time.Now().Unix()
	reader := bufio.NewReader(upstream.Body)
	var eventName string
	var usage types.Usage
	var fullText strings.Builder
	for {
		select {
		case <-ctx.Done():
			return &types.GatewayError{StatusCode: 499, Type: "canceled", Code: "client_closed", Message: "Client closed the request."}
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return &types.GatewayError{StatusCode: 502, Type: "server_error", Code: "upstream_stream_error", Message: "Upstream stream interrupted."}
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
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			continue
		}
		switch eventName {
		case "message_start":
			usage = mergeUsage(usage, parseAnthropicUsage(frame["message"]))
		case "content_block_delta":
			text := parseTextDelta(frame["delta"])
			if text != "" {
				tracker.MarkToken(text)
				fullText.WriteString(text)
				writeSSE(client, openAIStreamChunk(id, created, req.VirtualModel, text, ""))
				if flusher != nil {
					flusher.Flush()
				}
			}
		case "message_delta":
			usage = mergeUsage(usage, parseAnthropicUsage(frame["usage"]))
		case "message_stop":
			if usage.CompletionTokens == 0 {
				usage.CompletionTokens = tracker.Snapshot().Usage.CompletionTokens
			}
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			writeSSE(client, openAIStreamChunk(id, created, req.VirtualModel, "", "stop"))
			writeRawSSE(client, "[DONE]")
			if flusher != nil {
				flusher.Flush()
			}
			tracker.Event.OutputLabelsJSON = telemetry.LabelsJSON(map[string]any{"assistant": map[string]any{"text_length": fullText.Len()}})
			tracker.Finish(http.StatusOK, usage, "")
			return nil
		}
	}
	if usage.CompletionTokens == 0 {
		usage.CompletionTokens = tracker.Snapshot().Usage.CompletionTokens
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	writeSSE(client, openAIStreamChunk(id, created, req.VirtualModel, "", "stop"))
	writeRawSSE(client, "[DONE]")
	if flusher != nil {
		flusher.Flush()
	}
	tracker.Finish(http.StatusOK, usage, "")
	return nil
}

func (a *OpenAIToAnthropic) unaryAnthropicAsOpenAI(upstream *http.Response, client http.ResponseWriter, req ProxyRequest, tracker *telemetry.Tracker) *types.GatewayError {
	var ar anthropicResponse
	if err := json.NewDecoder(upstream.Body).Decode(&ar); err != nil {
		return &types.GatewayError{StatusCode: 502, Type: "server_error", Code: "invalid_upstream_response", Message: "Upstream returned an invalid response."}
	}
	var text strings.Builder
	for _, c := range ar.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	usage := types.Usage{PromptTokens: ar.Usage.InputTokens, CompletionTokens: ar.Usage.OutputTokens, CacheReadTokens: ar.Usage.CacheReadInputTokens, CacheWriteTokens: ar.Usage.CacheCreationInputTokens}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	resp := map[string]any{"id": "chatcmpl-" + tracker.Event.RequestID, "object": "chat.completion", "created": time.Now().Unix(), "model": req.VirtualModel, "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": text.String()}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens, "total_tokens": usage.TotalTokens}}
	client.Header().Set("Content-Type", "application/json")
	client.WriteHeader(http.StatusOK)
	json.NewEncoder(client).Encode(resp)
	tracker.MarkToken(text.String())
	tracker.Finish(http.StatusOK, usage, "")
	return nil
}

func writeSSE(w http.ResponseWriter, payload any) {
	b, _ := json.Marshal(payload)
	fmt.Fprintf(w, "data: %s\n\n", b)
}
func writeRawSSE(w http.ResponseWriter, payload string) { fmt.Fprintf(w, "data: %s\n\n", payload) }

func openAIStreamChunk(id string, created int64, model, text, finish string) map[string]any {
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

func classifyHTTP(code int) string {
	if code == 429 {
		return "rate_limit_error"
	}
	if code >= 500 {
		return "server_error"
	}
	return "invalid_request_error"
}
func safeHTTPMessage(code int) string {
	if code == 429 {
		return "Upstream rate limit exceeded."
	}
	if code >= 500 {
		return "Upstream service is temporarily unavailable."
	}
	return "The upstream provider rejected the request."
}

func applyOverrides(out *anthropicRequest, overrides map[string]any) {
	for k, v := range overrides {
		switch k {
		case "max_tokens":
			if f, ok := number(v); ok {
				out.MaxTokens = int64(f)
			}
		case "temperature":
			if f, ok := number(v); ok {
				out.Temperature = &f
			}
		case "top_p":
			if f, ok := number(v); ok {
				out.TopP = &f
			}
		case "system_prefix":
			if s, ok := v.(string); ok && s != "" {
				out.System = append([]contentBlock{{Type: "text", Text: s}}, out.System...)
			}
		}
	}
}
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	default:
		return 0, false
	}
}

func parseTextDelta(raw json.RawMessage) string {
	var d struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	json.Unmarshal(raw, &d)
	return d.Text
}
func parseAnthropicUsage(raw json.RawMessage) types.Usage {
	var wrapper struct {
		Usage anthropicUsage `json:"usage"`
	}
	if json.Unmarshal(raw, &wrapper) == nil && (wrapper.Usage.InputTokens > 0 || wrapper.Usage.OutputTokens > 0) {
		return usageFromAnthropic(wrapper.Usage)
	}
	var u anthropicUsage
	json.Unmarshal(raw, &u)
	return usageFromAnthropic(u)
}
func usageFromAnthropic(u anthropicUsage) types.Usage {
	r := types.Usage{PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens, CacheReadTokens: u.CacheReadInputTokens, CacheWriteTokens: u.CacheCreationInputTokens}
	r.TotalTokens = r.PromptTokens + r.CompletionTokens
	denom := r.CacheReadTokens + r.CacheWriteTokens
	if denom > 0 {
		r.CacheHitRatio = float64(r.CacheReadTokens) / float64(denom)
	}
	return r
}
func mergeUsage(a, b types.Usage) types.Usage {
	if b.PromptTokens != 0 {
		a.PromptTokens = b.PromptTokens
	}
	if b.CompletionTokens != 0 {
		a.CompletionTokens = b.CompletionTokens
	}
	if b.CacheReadTokens != 0 {
		a.CacheReadTokens = b.CacheReadTokens
	}
	if b.CacheWriteTokens != 0 {
		a.CacheWriteTokens = b.CacheWriteTokens
	}
	a.TotalTokens = a.PromptTokens + a.CompletionTokens
	return a
}

func openAIContentToAnthropic(ctx context.Context, content any) ([]contentBlock, error) {
	switch c := content.(type) {
	case string:
		return []contentBlock{{Type: "text", Text: c}}, nil
	case []any:
		blocks := make([]contentBlock, 0, len(c))
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			switch t {
			case "text":
				if txt, _ := m["text"].(string); txt != "" {
					blocks = append(blocks, contentBlock{Type: "text", Text: txt})
				}
			case "image_url":
				img, _ := m["image_url"].(map[string]any)
				url, _ := img["url"].(string)
				if url == "" {
					continue
				}
				media, data, err := imageURLToBase64(ctx, url)
				if err != nil {
					return nil, err
				}
				blocks = append(blocks, contentBlock{Type: "image", Source: &anthropicSource{Type: "base64", MediaType: media, Data: data}})
			}
		}
		if len(blocks) == 0 {
			return []contentBlock{{Type: "text", Text: ""}}, nil
		}
		return blocks, nil
	default:
		return []contentBlock{{Type: "text", Text: ""}}, nil
	}
}

func imageURLToBase64(ctx context.Context, url string) (string, string, error) {
	if strings.HasPrefix(url, "data:") {
		parts := strings.SplitN(strings.TrimPrefix(url, "data:"), ",", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid data url")
		}
		media := strings.Split(parts[0], ";")[0]
		return media, parts[1], nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("image fetch failed")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", "", err
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = http.DetectContentType(body)
	}
	media, _, _ := mime.ParseMediaType(ct)
	return media, base64.StdEncoding.EncodeToString(body), nil
}

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Stream      bool            `json:"stream"`
	MaxTokens   int64           `json:"max_tokens"`
	Temperature *float64        `json:"temperature"`
	TopP        *float64        `json:"top_p"`
}
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	ToolCallID string           `json:"tool_call_id"`
	ToolCalls  []openAIToolCall `json:"tool_calls"`
}

func (m openAIMessage) Text() string {
	if s, ok := m.Content.(string); ok {
		return s
	}
	b, _ := json.Marshal(m.Content)
	return string(b)
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
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
	Type      string           `json:"type"`
	Text      string           `json:"text,omitempty"`
	Source    *anthropicSource `json:"source,omitempty"`
	ID        string           `json:"id,omitempty"`
	Name      string           `json:"name,omitempty"`
	Input     json.RawMessage  `json:"input,omitempty"`
	ToolUseID string           `json:"tool_use_id,omitempty"`
	Content   string           `json:"content,omitempty"`
}
type anthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}
type anthropicResponse struct {
	Content []contentBlock `json:"content"`
	Usage   anthropicUsage `json:"usage"`
}
type anthropicUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}
