# Canonical IR

Canonical IR is the internal protocol-neutral representation used by `vibe-proxy`.

It prevents adapter explosion:

```text
N client protocols + M provider protocols
instead of
N × M protocol-pair adapters
```

## Canonical Request

```go
type Request struct {
    ID              string
    ClientProtocol  Protocol
    AgentID         string
    ProjectID       string

    RequestedModel  string
    ResolvedProvider string
    ResolvedModel   string

    Stream          bool
    Messages        []Message
    Tools           []Tool
    ToolChoice      ToolChoice
    ResponseFormat  ResponseFormat

    MaxTokens       *int
    Temperature     *float64
    TopP            *float64
    Stop            []string

    Metadata         map[string]string
    VendorExtensions map[string]any
}
```

## Message

```go
type Message struct {
    Role    Role
    Content []ContentBlock
    Name    string
}
```

Roles:

```text
system
user
assistant
tool
```

## Content Blocks

```go
type ContentBlock struct {
    Type      ContentType
    Text      string
    Image     *ImageContent
    File      *FileContent
    ToolCall  *ToolCall
    ToolResult *ToolResult

    VendorExtensions map[string]any
}
```

Content types:

```text
text
image
file
tool_call
tool_result
reasoning
```

## Tool Call

```go
type ToolCall struct {
    ID        string
    Name      string
    Arguments json.RawMessage
}
```

## Tool Result

```go
type ToolResult struct {
    ToolCallID string
    Content    []ContentBlock
    IsError    bool
}
```

## Usage

```go
type Usage struct {
    PromptTokens       int64
    CompletionTokens   int64
    TotalTokens        int64
    ReasoningTokens    int64
    CacheReadTokens    int64
    CacheWriteTokens   int64
    CacheHitRatio      float64
    ProviderRaw        json.RawMessage
}
```

## Vendor Extensions

Canonical IR should preserve protocol-specific information when possible.

Examples:

```yaml
vendor_extensions:
  anthropic:
    cache_control: ephemeral
    thinking: enabled
  openai:
    reasoning_effort: medium
    response_format: json_schema
  gemini:
    safety_settings: ...
```

Rules:

1. Standard fields should be normalized into Canonical IR.
2. Useful protocol-specific fields should be preserved in `VendorExtensions`.
3. Unsupported fields should be recorded in telemetry, not silently ignored.
4. Lossy transformations should be explicit and observable.

## Canonical Stream Events

Provider adapters emit canonical stream events. Client adapters encode them.

```go
type StreamEvent struct {
    Type       StreamEventType
    MessageID  string
    Index      int
    Delta      ContentBlock
    ToolCall   *ToolCall
    Usage      *Usage
    Error      *GatewayError
    Raw        []byte
}
```

Event types:

```text
message_start
content_delta
reasoning_delta
tool_call_start
tool_call_delta
tool_call_done
usage_delta
message_done
error
```

This allows shared handling of:

- TTFT
- TPOT
- trailing usage
- stream-to-unary accumulation
- client disconnect
- backpressure
