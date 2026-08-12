package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
)

func TestBuildOpenAICompatibleRequest(t *testing.T) {
	max := 64
	req := &ir.Request{Stream: true, MaxTokens: &max, Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: "hello"}}}}, Tools: []ir.Tool{{Name: "lookup", Parameters: json.RawMessage(`{"type":"object"}`)}}, ToolChoice: &ir.ToolChoice{Type: "tool", Name: "lookup"}, ResponseFormat: &ir.ResponseFormat{Type: "json_schema", JSONSchema: json.RawMessage(`{"type":"json_schema","name":"answer","schema":{"type":"object"}}`)}}
	hreq, err := Provider{}.BuildRequest(context.Background(), req, modelresolver.Target{BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat"})
	if err != nil {
		t.Fatal(err)
	}
	if hreq.URL.String() != "https://api.deepseek.com/v1/chat/completions" {
		t.Fatalf("unexpected url: %s", hreq.URL.String())
	}
	body, _ := io.ReadAll(hreq.Body)
	if !strings.Contains(string(body), `"model":"deepseek-chat"`) || !strings.Contains(string(body), `"content":"hello"`) || !strings.Contains(string(body), `"name":"lookup"`) || !strings.Contains(string(body), `"tool_choice":{"function":{"name":"lookup"},"type":"function"}`) || !strings.Contains(string(body), `"json_schema":{"name":"answer","schema":{"type":"object"}}`) || !strings.Contains(string(body), `"stream_options":{"include_usage":true}`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestParseOpenAICompatibleUnary(t *testing.T) {
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_1","model":"deepseek-chat","choices":[{"message":{"role":"assistant","content":"hi","reasoning_content":"think","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"vibe\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13,"prompt_tokens_details":{"cached_tokens":8}}}`))}
	out, err := Provider{}.ParseUnary(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "chatcmpl_1" || out.Messages[0].Content[0].Text != "think" || out.Messages[0].Content[1].Text != "hi" {
		t.Fatalf("unexpected response: %+v", out)
	}
	if out.Usage.TotalTokens != 13 || !out.Usage.CacheMetricsReported || out.Usage.CacheReadTokens != 8 || out.Usage.CacheHitRatio != 0.8 {
		t.Fatalf("usage not parsed: %+v", out.Usage)
	}
	tool := out.Messages[0].Content[2].ToolCall
	if tool == nil || tool.ID != "call_1" || tool.Name != "lookup" || string(tool.Arguments) != `{"q":"vibe"}` {
		t.Fatalf("tool call not parsed: %+v", out.Messages[0].Content)
	}
}

func TestParseOpenAICompatibleUnaryKeepsMissingCacheTelemetryUnknown(t *testing.T) {
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_1","model":"deepseek-chat","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))}
	out, err := Provider{}.ParseUnary(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}
	if out.Usage.CacheMetricsReported || out.Usage.CacheHitRatio != 0 {
		t.Fatalf("missing cache telemetry was treated as a reported zero: %+v", out.Usage)
	}
}

func TestParseOpenAICompatibleStream(t *testing.T) {
	sse := strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"he\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"llo\"}}]}\n\n",
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":5,\"total_tokens\":7}}\n\n",
		"data: [DONE]\n\n",
	}, "")
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(sse))}
	events, err := Provider{}.ParseStream(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	var sawUsage, sawDone bool
	for ev := range events {
		switch ev.Type {
		case ir.EventContentDelta:
			text += ev.Delta.Text
		case ir.EventUsageDelta:
			sawUsage = ev.Usage.TotalTokens == 7
		case ir.EventMessageDone:
			sawDone = true
		}
	}
	if text != "hello" || !sawUsage || !sawDone {
		t.Fatalf("unexpected stream text=%q usage=%v done=%v", text, sawUsage, sawDone)
	}
}

func TestParseOpenAICompatibleStreamReadsUsageAfterFinish(t *testing.T) {
	sse := strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":5,\"total_tokens\":7}}\n\n",
		"data: [DONE]\n\n",
	}, "")
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(sse))}
	events, err := Provider{}.ParseStream(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}
	var sawUsage, sawDone bool
	for ev := range events {
		switch ev.Type {
		case ir.EventUsageDelta:
			sawUsage = ev.Usage != nil && ev.Usage.TotalTokens == 7
		case ir.EventMessageDone:
			sawDone = true
		}
	}
	if !sawUsage || !sawDone {
		t.Fatalf("trailing usage was lost: usage=%v done=%v", sawUsage, sawDone)
	}
}

func TestParseOpenAICompatibleStreamStopsWhenConsumerCancels(t *testing.T) {
	var stream strings.Builder
	for i := 0; i < 64; i++ {
		stream.WriteString("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
	}
	body := &closeNotifyingBody{Reader: strings.NewReader(stream.String()), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	events, err := Provider{}.ParseStream(ctx, &http.Response{StatusCode: 200, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	_ = events
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-body.closed:
	case <-time.After(time.Second):
		t.Fatal("upstream response body was not closed after the consumer canceled")
	}
}

func TestParseOpenAICompatibleStreamReportsUnexpectedEOF(t *testing.T) {
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))}
	events, err := Provider{}.ParseStream(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}
	var sawInterrupted bool
	for event := range events {
		if event.Error != nil && event.Error.Code == "stream_interrupted" {
			sawInterrupted = true
		}
	}
	if !sawInterrupted {
		t.Fatal("truncated OpenAI stream was treated as successful")
	}
}

type closeNotifyingBody struct {
	*strings.Reader
	closed chan struct{}
}

func (b *closeNotifyingBody) Close() error {
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}
