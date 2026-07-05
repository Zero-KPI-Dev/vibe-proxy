package anthropic

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
)

func TestBuildAnthropicRequest(t *testing.T) {
	max := 128
	req := &ir.Request{ResolvedModel: "claude-test", Stream: true, MaxTokens: &max, Messages: []ir.Message{{Role: ir.RoleSystem, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: "sys"}}}, {Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: "hello"}}}}}
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
	if !strings.Contains(string(body), `"model":"claude-test"`) || !strings.Contains(string(body), `"text":"hello"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestParseAnthropicUnary(t *testing.T) {
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","model":"claude-test","stop_reason":"end_turn","content":[{"type":"thinking","thinking":"think"},{"type":"text","text":"hi"}],"usage":{"input_tokens":3,"output_tokens":4,"cache_read_input_tokens":1,"cache_creation_input_tokens":2}}`))}
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
}

func TestParseAnthropicStream(t *testing.T) {
	sse := strings.Join([]string{
		"event: message_start\n",
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\n",
		"event: content_block_delta\n",
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"he\"}}\n\n",
		"event: content_block_delta\n",
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"llo\"}}\n\n",
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
	var sawUsage, sawDone bool
	for ev := range events {
		switch ev.Type {
		case ir.EventContentDelta:
			text += ev.Delta.Text
		case ir.EventUsageDelta:
			sawUsage = ev.Usage.CompletionTokens == 5
		case ir.EventMessageDone:
			sawDone = true
		}
	}
	if text != "hello" || !sawUsage || !sawDone {
		t.Fatalf("unexpected stream text=%q usage=%v done=%v", text, sawUsage, sawDone)
	}
}
