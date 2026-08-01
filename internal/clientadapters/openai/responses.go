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

	"github.com/a448582655/vibe-proxy/internal/ir"
)

type ResponsesAdapter struct{}

func (a ResponsesAdapter) Name() string                { return "openai-responses" }
func (a ResponsesAdapter) Protocol() ir.Protocol       { return ir.ProtocolOpenAIResponses }
func (a ResponsesAdapter) Detect(r *http.Request) bool { return r.URL.Path == "/v1/responses" }

func (a ResponsesAdapter) ParseRequest(ctx context.Context, r *http.Request) (*ir.Request, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		return nil, ir.GatewayError{StatusCode: 400, Kind: "invalid_request_error", Code: "body_read_failed", Message: "Could not read request body."}
	}
	var in responsesRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, ir.GatewayError{StatusCode: 400, Kind: "invalid_request_error", Code: "invalid_json", Message: "Request body is not valid JSON."}
	}
	if in.Model == "" {
		return nil, ir.GatewayError{StatusCode: 400, Kind: "invalid_request_error", Code: "missing_model", Message: "Request JSON must include a model."}
	}
	req := &ir.Request{ID: uuid.NewString(), ClientProtocol: ir.ProtocolOpenAIResponses, RequestedModel: in.Model, Stream: in.Stream, MaxTokens: in.MaxOutputTokens, Temperature: in.Temperature, TopP: in.TopP, VendorExtensions: map[string]any{"openai_responses_raw": json.RawMessage(body)}}
	req.Messages = parseResponsesInput(in.Input)
	for _, t := range in.Tools {
		req.Tools = append(req.Tools, ir.Tool{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	req.ToolChoice = parseOpenAIToolChoice(in.ToolChoice)
	if in.Text.Format.Type != "" {
		raw, _ := json.Marshal(in.Text.Format)
		req.ResponseFormat = &ir.ResponseFormat{Type: in.Text.Format.Type, JSONSchema: raw}
	}
	return req, nil
}

func (a ResponsesAdapter) EncodeUnary(ctx context.Context, w http.ResponseWriter, resp *ir.Response) error {
	text := ""
	var blocks []ir.ContentBlock
	if len(resp.Messages) > 0 {
		blocks = resp.Messages[len(resp.Messages)-1].Content
		text = contentText(blocks)
	}
	reasoning := reasoningText(blocks)
	id := resp.ID
	if id == "" {
		id = "resp_" + uuid.NewString()
	}
	output := []any{}
	if reasoning != "" {
		output = append(output, map[string]any{
			"id":     "rs_" + uuid.NewString(),
			"type":   "reasoning",
			"status": "completed",
			"summary": []any{
				map[string]any{"type": "summary_text", "text": reasoning},
			},
		})
	}
	toolCalls := responseToolCalls(blocks)
	if text != "" || len(toolCalls) == 0 {
		output = append(output, map[string]any{"id": "msg_" + uuid.NewString(), "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}})
	}
	output = append(output, toolCalls...)
	out := map[string]any{"id": id, "object": "response", "created_at": time.Now().Unix(), "status": "completed", "model": resp.Model, "output": output, "usage": map[string]any{"input_tokens": resp.Usage.PromptTokens, "output_tokens": resp.Usage.CompletionTokens, "total_tokens": resp.Usage.TotalTokens}}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(out)
}

func (a ResponsesAdapter) EncodeStream(ctx context.Context, w http.ResponseWriter, events <-chan ir.StreamEvent) error {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	responseID := "resp_" + uuid.NewString()
	itemID := "msg_" + uuid.NewString()
	writeResponsesEvent(w, "response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": responseID, "object": "response", "status": "in_progress"}})
	flushOpenAI(flusher)
	messageAdded := false
	messageIndex := -1
	reasoningAdded := false
	reasoningIndex := -1
	reasoningID := "rs_" + uuid.NewString()
	var reasoning strings.Builder
	var usage ir.Usage
	nextOutputIndex := 0
	type functionItem struct {
		OutputIndex int
		ItemID      string
		CallID      string
		Name        string
		Arguments   strings.Builder
	}
	functionItems := map[string]*functionItem{}
	functionOrder := []*functionItem{}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				writeResponsesDone(w, responseID, usage)
				flushOpenAI(flusher)
				return nil
			}
			if ev.Error != nil {
				return *ev.Error
			}
			switch ev.Type {
			case ir.EventReasoningDelta:
				if ev.Delta.Text == "" {
					continue
				}
				if !reasoningAdded {
					reasoningIndex = nextOutputIndex
					nextOutputIndex++
					reasoningAdded = true
					writeResponsesEvent(w, "response.output_item.added", map[string]any{
						"type":         "response.output_item.added",
						"output_index": reasoningIndex,
						"item": map[string]any{
							"id": reasoningID, "type": "reasoning", "status": "in_progress", "summary": []any{},
						},
					})
				}
				reasoning.WriteString(ev.Delta.Text)
				writeResponsesEvent(w, "response.reasoning_summary_text.delta", map[string]any{
					"type": "response.reasoning_summary_text.delta", "item_id": reasoningID,
					"output_index": reasoningIndex, "summary_index": 0, "delta": ev.Delta.Text,
				})
				flushOpenAI(flusher)
			case ir.EventContentDelta:
				if ev.Delta.Text == "" {
					continue
				}
				if !messageAdded {
					messageIndex = nextOutputIndex
					nextOutputIndex++
					messageAdded = true
					writeResponsesEvent(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": messageIndex, "item": map[string]any{"id": itemID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}})
				}
				writeResponsesEvent(w, "response.output_text.delta", map[string]any{"type": "response.output_text.delta", "item_id": itemID, "output_index": messageIndex, "content_index": 0, "delta": ev.Delta.Text})
				flushOpenAI(flusher)
			case ir.EventToolCallDelta, ir.EventToolCallStart:
				if ev.ToolCall != nil {
					indexKey := fmt.Sprintf("index:%d", ev.Index)
					key := ev.ToolCall.ID
					if key == "" {
						key = indexKey
					}
					item := functionItems[key]
					if item == nil {
						item = functionItems[indexKey]
					}
					if item == nil {
						callID := ev.ToolCall.ID
						if callID == "" {
							callID = "call_" + uuid.NewString()
						}
						item = &functionItem{OutputIndex: nextOutputIndex, ItemID: "fc_" + uuid.NewString(), CallID: callID, Name: ev.ToolCall.Name}
						nextOutputIndex++
						functionItems[key] = item
						functionItems[indexKey] = item
						functionOrder = append(functionOrder, item)
						writeResponsesEvent(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": item.OutputIndex, "item": map[string]any{"id": item.ItemID, "type": "function_call", "status": "in_progress", "call_id": item.CallID, "name": item.Name, "arguments": ""}})
					}
					arguments := string(ev.ToolCall.Arguments)
					if arguments != "" && !(ev.Type == ir.EventToolCallStart && arguments == "{}") {
						item.Arguments.WriteString(arguments)
						writeResponsesEvent(w, "response.function_call_arguments.delta", map[string]any{"type": "response.function_call_arguments.delta", "item_id": item.ItemID, "output_index": item.OutputIndex, "delta": arguments})
					}
					flushOpenAI(flusher)
				}
			case ir.EventUsageDelta:
				if ev.Usage != nil {
					usage = mergeStreamUsage(usage, *ev.Usage)
				}
			case ir.EventMessageDone:
				if !messageAdded && !reasoningAdded && len(functionOrder) == 0 {
					messageIndex = nextOutputIndex
					messageAdded = true
					writeResponsesEvent(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": messageIndex, "item": map[string]any{"id": itemID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}})
				}
				if reasoningAdded {
					writeResponsesEvent(w, "response.output_item.done", map[string]any{
						"type": "response.output_item.done", "output_index": reasoningIndex,
						"item": map[string]any{
							"id": reasoningID, "type": "reasoning", "status": "completed",
							"summary": []any{map[string]any{"type": "summary_text", "text": reasoning.String()}},
						},
					})
				}
				if messageAdded {
					writeResponsesEvent(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": messageIndex, "item": map[string]any{"id": itemID, "type": "message", "status": "completed", "role": "assistant"}})
				}
				for _, item := range functionOrder {
					writeResponsesEvent(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": item.OutputIndex, "item": map[string]any{"id": item.ItemID, "type": "function_call", "status": "completed", "call_id": item.CallID, "name": item.Name, "arguments": item.Arguments.String()}})
				}
				writeResponsesDone(w, responseID, usage)
				flushOpenAI(flusher)
				return nil
			}
		}
	}
}

func responseToolCalls(blocks []ir.ContentBlock) []any {
	out := []any{}
	for _, block := range blocks {
		if block.Type != ir.ContentToolCall || block.ToolCall == nil {
			continue
		}
		callID := block.ToolCall.ID
		if callID == "" {
			callID = "call_" + uuid.NewString()
		}
		out = append(out, map[string]any{
			"id":        "fc_" + uuid.NewString(),
			"type":      "function_call",
			"status":    "completed",
			"call_id":   callID,
			"name":      block.ToolCall.Name,
			"arguments": string(block.ToolCall.Arguments),
		})
	}
	return out
}

func (a ResponsesAdapter) EncodeError(ctx context.Context, w http.ResponseWriter, err ir.GatewayError) error {
	return ChatAdapter{}.EncodeError(ctx, w, err)
}

func parseResponsesInput(input any) []ir.Message {
	switch v := input.(type) {
	case string:
		return []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: v}}}}
	case []any:
		messages := []ir.Message{}
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			role, _ := m["role"].(string)
			if role == "" {
				role = "user"
			}
			switch t {
			case "message", "":
				messages = append(messages, ir.Message{Role: ir.Role(role), Content: responsesContent(m["content"])})
			case "function_call_output":
				callID, _ := m["call_id"].(string)
				output, _ := m["output"].(string)
				messages = append(messages, ir.Message{Role: ir.RoleTool, Content: []ir.ContentBlock{{Type: ir.ContentToolResult, ToolResult: &ir.ToolResult{ToolCallID: callID, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: output}}}}}})
			}
		}
		return messages
	default:
		return nil
	}
}

func responsesContent(content any) []ir.ContentBlock {
	switch c := content.(type) {
	case string:
		return []ir.ContentBlock{{Type: ir.ContentText, Text: c}}
	case []any:
		blocks := []ir.ContentBlock{}
		for _, it := range c {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			switch t {
			case "input_text", "output_text", "text":
				txt, _ := m["text"].(string)
				blocks = append(blocks, ir.ContentBlock{Type: ir.ContentText, Text: txt})
			case "input_image":
				url, _ := m["image_url"].(string)
				blocks = append(blocks, ir.ContentBlock{Type: ir.ContentImage, Image: &ir.ImageContent{URL: url}})
			}
		}
		return blocks
	default:
		return nil
	}
}
func writeResponsesEvent(w http.ResponseWriter, event string, payload any) {
	b, _ := json.Marshal(payload)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}
func writeResponsesDone(w http.ResponseWriter, id string, usage ir.Usage) {
	response := map[string]any{"id": id, "status": "completed"}
	if usage.TotalTokens > 0 || usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		response["usage"] = map[string]any{"input_tokens": usage.PromptTokens, "output_tokens": usage.CompletionTokens, "total_tokens": usage.TotalTokens}
	}
	writeResponsesEvent(w, "response.completed", map[string]any{"type": "response.completed", "response": response})
	fmt.Fprint(w, "data: [DONE]\n\n")
}
func flushOpenAI(f http.Flusher) {
	if f != nil {
		f.Flush()
	}
}

type responsesRequest struct {
	Model           string          `json:"model"`
	Input           any             `json:"input"`
	Stream          bool            `json:"stream"`
	MaxOutputTokens *int            `json:"max_output_tokens"`
	Temperature     *float64        `json:"temperature"`
	TopP            *float64        `json:"top_p"`
	Tools           []responsesTool `json:"tools"`
	ToolChoice      any             `json:"tool_choice"`
	Text            struct {
		Format struct {
			Type   string          `json:"type"`
			Name   string          `json:"name,omitempty"`
			Schema json.RawMessage `json:"schema,omitempty"`
		} `json:"format"`
	} `json:"text"`
}
type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

var _ = strings.Builder{}
