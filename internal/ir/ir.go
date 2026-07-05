package ir

import (
	"encoding/json"
	"time"
)

type Protocol string

const (
	ProtocolOpenAIChat        Protocol = "openai_chat"
	ProtocolOpenAIResponses   Protocol = "openai_responses"
	ProtocolAnthropicMessages Protocol = "anthropic_messages"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ContentType string

const (
	ContentText       ContentType = "text"
	ContentImage      ContentType = "image"
	ContentFile       ContentType = "file"
	ContentToolCall   ContentType = "tool_call"
	ContentToolResult ContentType = "tool_result"
	ContentReasoning  ContentType = "reasoning"
)

type Request struct {
	ID               string            `json:"id"`
	ClientProtocol   Protocol          `json:"client_protocol"`
	AgentID          string            `json:"agent_id,omitempty"`
	ProjectID        string            `json:"project_id,omitempty"`
	RequestedModel   string            `json:"requested_model"`
	ResolvedProvider string            `json:"resolved_provider,omitempty"`
	ResolvedModel    string            `json:"resolved_model,omitempty"`
	Stream           bool              `json:"stream"`
	Messages         []Message         `json:"messages"`
	Tools            []Tool            `json:"tools,omitempty"`
	ToolChoice       *ToolChoice       `json:"tool_choice,omitempty"`
	ResponseFormat   *ResponseFormat   `json:"response_format,omitempty"`
	MaxTokens        *int              `json:"max_tokens,omitempty"`
	Temperature      *float64          `json:"temperature,omitempty"`
	TopP             *float64          `json:"top_p,omitempty"`
	Stop             []string          `json:"stop,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	VendorExtensions map[string]any    `json:"vendor_extensions,omitempty"`
}

type Message struct {
	Role             Role           `json:"role"`
	Content          []ContentBlock `json:"content"`
	Name             string         `json:"name,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type ContentBlock struct {
	Type             ContentType    `json:"type"`
	Text             string         `json:"text,omitempty"`
	Image            *ImageContent  `json:"image,omitempty"`
	File             *FileContent   `json:"file,omitempty"`
	ToolCall         *ToolCall      `json:"tool_call,omitempty"`
	ToolResult       *ToolResult    `json:"tool_result,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type ImageContent struct {
	URL       string `json:"url,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Base64    string `json:"base64,omitempty"`
}

type FileContent struct {
	URL       string `json:"url,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Base64    string `json:"base64,omitempty"`
	Name      string `json:"name,omitempty"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type ToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolResult struct {
	ToolCallID string         `json:"tool_call_id"`
	Content    []ContentBlock `json:"content"`
	IsError    bool           `json:"is_error"`
}

type ResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
}

type Response struct {
	ID               string         `json:"id"`
	Model            string         `json:"model"`
	Messages         []Message      `json:"messages"`
	StopReason       string         `json:"stop_reason,omitempty"`
	Usage            Usage          `json:"usage"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type Usage struct {
	PromptTokens     int64           `json:"prompt_tokens"`
	CompletionTokens int64           `json:"completion_tokens"`
	TotalTokens      int64           `json:"total_tokens"`
	ReasoningTokens  int64           `json:"reasoning_tokens"`
	CacheReadTokens  int64           `json:"cache_read_tokens"`
	CacheWriteTokens int64           `json:"cache_write_tokens"`
	CacheHitRatio    float64         `json:"cache_hit_ratio"`
	ProviderRaw      json.RawMessage `json:"provider_raw,omitempty"`
}

type StreamEventType string

const (
	EventMessageStart   StreamEventType = "message_start"
	EventContentDelta   StreamEventType = "content_delta"
	EventReasoningDelta StreamEventType = "reasoning_delta"
	EventToolCallStart  StreamEventType = "tool_call_start"
	EventToolCallDelta  StreamEventType = "tool_call_delta"
	EventToolCallDone   StreamEventType = "tool_call_done"
	EventUsageDelta     StreamEventType = "usage_delta"
	EventMessageDone    StreamEventType = "message_done"
	EventError          StreamEventType = "error"
)

type StreamEvent struct {
	Type      StreamEventType `json:"type"`
	Time      time.Time       `json:"time"`
	MessageID string          `json:"message_id,omitempty"`
	Index     int             `json:"index"`
	Delta     ContentBlock    `json:"delta,omitempty"`
	ToolCall  *ToolCall       `json:"tool_call,omitempty"`
	Usage     *Usage          `json:"usage,omitempty"`
	Error     *GatewayError   `json:"error,omitempty"`
	Raw       []byte          `json:"raw,omitempty"`
}

type GatewayError struct {
	StatusCode int    `json:"status_code"`
	Kind       string `json:"kind"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter string `json:"retry_after,omitempty"`
}

func (e GatewayError) Error() string { return e.Message }
