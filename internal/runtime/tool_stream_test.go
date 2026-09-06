package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clientopenai "github.com/a448582655/vibe-proxy/internal/clientadapters/openai"
	"github.com/a448582655/vibe-proxy/internal/ir"
	provideranthropic "github.com/a448582655/vibe-proxy/internal/provideradapters/anthropic"
)

func TestChatStreamKeepsInterleavedToolCallsSeparate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan ir.StreamEvent, 7)
	// Anthropic content indices can include earlier text or thinking blocks.
	events <- ir.StreamEvent{Type: ir.EventToolCallStart, Index: 2, ToolCall: &ir.ToolCall{ID: "call_a", Name: "read"}}
	events <- ir.StreamEvent{Type: ir.EventToolCallStart, Index: 4, ToolCall: &ir.ToolCall{ID: "call_b", Name: "search"}}
	events <- ir.StreamEvent{Type: ir.EventToolCallDelta, Index: 2, ToolCall: &ir.ToolCall{Arguments: json.RawMessage(`{"path":`)}}
	events <- ir.StreamEvent{Type: ir.EventToolCallDelta, Index: 4, ToolCall: &ir.ToolCall{Arguments: json.RawMessage(`{"query":`)}}
	events <- ir.StreamEvent{Type: ir.EventToolCallDelta, Index: 2, ToolCall: &ir.ToolCall{Arguments: json.RawMessage(`"a"}`)}}
	events <- ir.StreamEvent{Type: ir.EventToolCallDelta, Index: 4, ToolCall: &ir.ToolCall{Arguments: json.RawMessage(`"b"}`)}}
	events <- ir.StreamEvent{Type: ir.EventMessageDone}
	close(events)
	w := httptest.NewRecorder()
	if err := (clientopenai.ChatAdapter{}).EncodeStream(ctx, w, events); err != nil {
		t.Fatal(err)
	}
	calls := assembleChatTools(t, w.Body.String())
	if len(calls) != 2 || calls[0].ID != "call_a" || calls[0].Name != "read" || string(calls[0].Arguments) != `{"path":"a"}` || calls[1].ID != "call_b" || calls[1].Name != "search" || string(calls[1].Arguments) != `{"query":"b"}` {
		t.Fatalf("tool streams were merged or corrupted: %+v", calls)
	}
}

func TestAnthropicToChatToolDeltasDoNotRepeatIdentity(t *testing.T) {
	source := `event: content_block_start
data: {"index":0,"content_block":{"type":"tool_use","id":"call_a","name":"read","input":{}}}

event: content_block_delta
data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_delta
data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"\"a\"}"}}

event: content_block_stop
data: {"index":0}

event: message_stop
data: {"type":"message_stop"}

`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := (provideranthropic.Provider{}).ParseStream(ctx, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(source))})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := (clientopenai.ChatAdapter{}).EncodeStream(ctx, w, events); err != nil {
		t.Fatal(err)
	}
	calls := assembleChatTools(t, w.Body.String())
	if len(calls) != 1 || calls[0].ID != "call_a" || calls[0].Name != "read" || string(calls[0].Arguments) != `{"path":"a"}` {
		t.Fatalf("SDK-style delta accumulation produced an invalid tool call: %+v", calls)
	}
}

// Model how SDKs concatenate fields for each tool index across SSE deltas.
func assembleChatTools(t *testing.T, body string) map[int]ir.ToolCall {
	t.Helper()
	calls := map[int]ir.ToolCall{}
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}
		var frame struct {
			Choices []struct {
				Delta struct {
					Tools []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame); err != nil {
			t.Fatal(err)
		}
		for _, choice := range frame.Choices {
			for _, tool := range choice.Delta.Tools {
				call := calls[tool.Index]
				call.ID += tool.ID
				call.Name += tool.Function.Name
				call.Arguments = append(call.Arguments, tool.Function.Arguments...)
				calls[tool.Index] = call
			}
		}
	}
	return calls
}
