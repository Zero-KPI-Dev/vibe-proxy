package anthropic

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

func TestBuildAnthropicRequest(t *testing.T) {
	max := 128
	req := &ir.Request{ResolvedModel: "claude-test", Stream: true, MaxTokens: &max, Stop: []string{"END"}, Messages: []ir.Message{{Role: ir.RoleSystem, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: "sys"}}}, {Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: "hello"}}}}, Tools: []ir.Tool{{Name: "lookup", Description: "Lookup data", Parameters: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)}}, ToolChoice: &ir.ToolChoice{Type: "tool", Name: "lookup"}}
	hreq, err := Provider{}.BuildRequest(context.Background(), req, modelresolver.Target{BaseURL: "https://api.anthropic.com", Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	if hreq.URL.String() != "https://api.anthropic.com/v1/messages" {
		t.Fatalf("unexpected url: %s", hreq.URL.String())
	}
	if hreq.Header.Get("anthropic-version") == "" {
		t.Fatal("missing anthropic-version")
	}
	body, _ := io.ReadAll(hreq.Body)
	if !strings.Contains(string(body), `"model":"claude-test"`) || !strings.Contains(string(body), `"text":"hello"`) || !strings.Contains(string(body), `"name":"lookup"`) || !strings.Contains(string(body), `"stop_sequences":["END"]`) || !strings.Contains(string(body), `"tool_choice":{"type":"tool","name":"lookup"}`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestParseAnthropicUnary(t *testing.T) {
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","model":"claude-test","stop_reason":"tool_use","content":[{"type":"thinking","thinking":"think"},{"type":"text","text":"hi"},{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"q":"vibe"}}],"usage":{"input_tokens":3,"output_tokens":4,"cache_read_input_tokens":1,"cache_creation_input_tokens":2}}`))}
	out, err := Provider{}.ParseUnary(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "msg_1" || out.Messages[0].Content[0].Text != "think" || out.Messages[0].Content[1].Text != "hi" {
		t.Fatalf("unexpected response: %+v", out)
	}
	if out.Usage.PromptTokens != 3 || out.Usage.CompletionTokens != 4 || out.Usage.CacheReadTokens != 1 || out.Usage.CacheWriteTokens != 2 {
		t.Fatalf("usage not parsed: %+v", out.Usage)
	}
	tool := out.Messages[0].Content[2].ToolCall
	if tool == nil || tool.ID != "toolu_1" || tool.Name != "lookup" || string(tool.Arguments) != `{"q":"vibe"}` {
		t.Fatalf("tool use not parsed: %+v", out.Messages[0].Content)
	}
}

func TestParseAnthropicStream(t *testing.T) {
	sse := strings.Join([]string{
		"event: message_start\n",
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\n",
		"event: content_block_delta\n",
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"he\"}}\n\n",
		"event: content_block_delta\n",
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"llo\"}}\n\n",
		"event: content_block_start\n",
		"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"lookup\",\"input\":{}}}\n\n",
		"event: content_block_delta\n",
		"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"q\\\":\\\"vibe\\\"}\"}}\n\n",
		"event: content_block_stop\n",
		"data: {\"type\":\"content_block_stop\",\"index\":1}\n\n",
		"event: message_delta\n",
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n",
		"event: message_stop\n",
		"data: {\"type\":\"message_stop\"}\n\n",
	}, "")
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(sse))}
	events, err := Provider{}.ParseStream(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	var sawUsage, sawDone, sawToolStart, sawToolDelta, sawToolDone bool
	for ev := range events {
		switch ev.Type {
		case ir.EventContentDelta:
			text += ev.Delta.Text
		case ir.EventUsageDelta:
			sawUsage = ev.Usage.CompletionTokens == 5
		case ir.EventMessageDone:
			sawDone = true
		case ir.EventToolCallStart:
			sawToolStart = ev.ToolCall != nil && ev.ToolCall.ID == "toolu_1" && ev.ToolCall.Name == "lookup"
		case ir.EventToolCallDelta:
			sawToolDelta = ev.ToolCall != nil && string(ev.ToolCall.Arguments) == `{"q":"vibe"}`
		case ir.EventToolCallDone:
			sawToolDone = ev.ToolCall != nil && ev.ToolCall.ID == "toolu_1"
		}
	}
	if text != "hello" || !sawUsage || !sawDone || !sawToolStart || !sawToolDelta || !sawToolDone {
		t.Fatalf("unexpected stream text=%q usage=%v done=%v tools=%v/%v/%v", text, sawUsage, sawDone, sawToolStart, sawToolDelta, sawToolDone)
	}
}

func TestParseAnthropicStreamStopsWhenConsumerCancels(t *testing.T) {
	var stream strings.Builder
	for i := 0; i < 64; i++ {
		stream.WriteString("event: content_block_delta\n")
		stream.WriteString("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\n\n")
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

func TestParseAnthropicStreamReportsUnexpectedEOF(t *testing.T) {
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n"))}
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
		t.Fatal("truncated Anthropic stream was treated as successful")
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
