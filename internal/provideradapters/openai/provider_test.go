package openai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
)

func TestBuildOpenAICompatibleRequest(t *testing.T) {
	max := 64
	req := &ir.Request{Stream: true, MaxTokens: &max, Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: "hello"}}}}}
	hreq, err := Provider{}.BuildRequest(context.Background(), req, modelresolver.Target{BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat"})
	if err != nil {
		t.Fatal(err)
	}
	if hreq.URL.String() != "https://api.deepseek.com/v1/chat/completions" {
		t.Fatalf("unexpected url: %s", hreq.URL.String())
	}
	body, _ := io.ReadAll(hreq.Body)
	if !strings.Contains(string(body), `"model":"deepseek-chat"`) || !strings.Contains(string(body), `"content":"hello"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestParseOpenAICompatibleUnary(t *testing.T) {
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_1","model":"deepseek-chat","choices":[{"message":{"role":"assistant","content":"hi","reasoning_content":"think"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))}
	out, err := Provider{}.ParseUnary(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "chatcmpl_1" || out.Messages[0].Content[0].Text != "think" || out.Messages[0].Content[1].Text != "hi" {
		t.Fatalf("unexpected response: %+v", out)
	}
	if out.Usage.TotalTokens != 5 {
		t.Fatalf("usage not parsed: %+v", out.Usage)
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
