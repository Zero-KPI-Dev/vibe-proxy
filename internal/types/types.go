package types

import "time"

type Protocol string

const (
	ProtocolOpenAIChat       Protocol = "openai_chat"
	ProtocolOpenAIResponses  Protocol = "openai_responses"
	ProtocolAnthropicMessage Protocol = "anthropic_messages"
)

type RequestKind string

const (
	RequestKindUnary  RequestKind = "unary"
	RequestKindStream RequestKind = "stream"
)

type GatewayError struct {
	StatusCode int
	Type       string
	Code       string
	Message    string
	RetryAfter string
}

func (e GatewayError) Error() string { return e.Message }

type Usage struct {
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	CacheHitRatio    float64 `json:"cache_hit_ratio"`
	ProviderRaw      string  `json:"provider_raw,omitempty"`
}

type StreamStats struct {
	RequestID          string
	StartedAt          time.Time
	FirstTokenAt       time.Time
	CompletedAt        time.Time
	OutputTokenCount   int64
	InterTokenDuration time.Duration
	Usage              Usage
}
