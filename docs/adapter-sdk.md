# Adapter SDK

Adapters are the main extension point for protocol support.

There are two adapter types:

- Client Adapter: external client protocol -> Canonical IR, Canonical response -> external client protocol
- Provider Adapter: Canonical IR -> upstream provider request, upstream response -> Canonical response/events

## Client Adapter

```go
type ClientAdapter interface {
    Name() string
    Protocol() Protocol
    Detect(r *http.Request) bool
    ParseRequest(ctx context.Context, r *http.Request) (*ir.Request, error)
    EncodeUnary(ctx context.Context, w http.ResponseWriter, resp *ir.Response) error
    EncodeStream(ctx context.Context, w http.ResponseWriter, events <-chan ir.StreamEvent) error
    EncodeError(ctx context.Context, w http.ResponseWriter, err GatewayError) error
}
```

Responsibilities:

- detect supported requests
- parse request body and headers
- normalize request into Canonical IR
- encode successful responses into the client protocol
- encode normalized errors into the client protocol

Client adapters should not choose providers, apply upstream auth, or implement routing policy.

## Provider Adapter

```go
type ProviderAdapter interface {
    Name() string
    Protocol() Protocol
    BuildRequest(ctx context.Context, req *ir.Request, ch Channel) (*http.Request, error)
    ParseUnary(ctx context.Context, resp *http.Response) (*ir.Response, error)
    ParseStream(ctx context.Context, resp *http.Response) (<-chan ir.StreamEvent, error)
    NormalizeError(ctx context.Context, resp *http.Response) GatewayError
}
```

Responsibilities:

- build upstream requests from Canonical IR
- parse unary responses
- parse streaming responses into canonical stream events
- normalize upstream errors

Provider adapters should not encode client-facing responses.

## Adapter Contribution Rules

An adapter should include:

1. protocol capability declaration
2. request parsing tests
3. unary response tests
4. streaming response tests when applicable
5. error normalization tests
6. conformance fixtures

## Capability Declaration

Adapters should declare what they support:

```yaml
capabilities:
  streaming: true
  tools: true
  parallel_tool_calls: true
  vision: true
  files: false
  json_mode: true
  structured_output: false
  prompt_cache: false
  trailing_usage: true
```

The router and model resolver can use this information to reject unsupported combinations early.

## Lossy Transformations

If an adapter cannot preserve a field, it must report a transformation warning.

Examples:

- client requested JSON schema, provider only supports plain text
- client sent remote image URL, provider requires base64 and fetch failed
- provider emits reasoning tokens but client protocol has no equivalent field

These warnings should be visible in request telemetry.
