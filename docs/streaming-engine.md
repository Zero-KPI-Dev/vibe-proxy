# Streaming Engine

The streaming engine is responsible for low-latency protocol conversion.

## Goals

- preserve minimal TTFT
- avoid full-stream buffering
- normalize provider streaming events
- capture trailing token usage
- support stream-to-unary accumulation
- support unary-to-stream synthetic output where useful
- handle client disconnects and upstream interruptions safely

## Architecture

```text
Provider HTTP stream
  -> Provider Adapter stream parser
  -> Canonical Stream Events
  -> Stream Engine
  -> Telemetry Tracker
  -> Client Adapter stream encoder
  -> Client HTTP stream
```

## Canonical Events

The stream engine operates on canonical stream events:

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

## TTFT

TTFT is measured from request acceptance to the first output event observed by
the stream engine. This includes reasoning and tool-call starts. It does not
measure when the client application consumes the bytes.

Not every upstream event counts as first token. For example:

- metadata-only event: does not count
- role-only event: does not count
- empty delta: does not count
- content text delta: counts
- reasoning text delta: counts
- tool call start with an ID or name: counts
- tool call argument delta: counts

## Trailing Usage

Some providers emit usage only in the final frame.

The stream engine must:

1. parse final usage frames
2. update canonical usage
3. record telemetry
4. encode usage to the client protocol if the client supports it
5. never delay ordinary content deltas while waiting for final usage

## Stream-to-Unary

When the upstream provider streams but the client requested unary, the engine may accumulate canonical events and return a single canonical response.

This is useful for:

- providers that only support streaming for certain features
- future compatibility shims

## Unary-to-Stream

When the upstream provider is unary but the client requested streaming, the engine may emit a synthetic stream.

This should be used carefully because it cannot provide real provider TTFT.

Telemetry should mark it as synthetic streaming.

## Error Handling

Errors must be normalized into internal error taxonomy first, then encoded by the client adapter.

Raw upstream errors must not leak to clients by default.

Telemetry may store sanitized upstream details for local debugging.

## Time Limits and Recovery

Provider configuration supports three independent budgets:

```yaml
providers:
  example:
    type: openai-compatible
    base_url: http://127.0.0.1:3000/v1
    auth: {type: none}
    models: [example-model]
    max_concurrency: 8
    timeout: 5m
    first_token_timeout: 90s
    stream_idle_timeout: 60s
```

`timeout` remains the total upstream request limit (default 120 seconds).
Streaming requests additionally default to 60 seconds before their first output
and 60 seconds between output events. Omitted or zero progress timeouts use the
defaults; negative values are rejected. The total limit still wins when it is
shorter. Increase both the total and first-token limits for models that need a
long prefill; increasing only the total limit does not disable stall detection.
An explicitly configured `server.write_timeout` additionally caps handler time;
per-write idle deadlines cannot extend that budget.
Editing a provider through the form preserves these advanced YAML settings.

HTTP headers, role-only frames and SSE keepalives cannot extend the first-token
budget. Nonempty content, reasoning and tool-call events reset the idle budget.
The watchdog starts before dispatch and cancels the HTTP request on expiration.

Client encoders use `httpstream.Writer`, which limits each write/flush to 30
seconds and interrupts a blocked socket write when the request is cancelled.
It retains both write and flush errors. The response middleware exposes
`Unwrap`/`FlushError` so these controls reach the real HTTP connection on all
platforms. Cancellation callbacks finish before the handler returns, preventing
a late deadline update from affecting a reused connection.

Only an explicit terminal IR event represents successful completion. An
unexpected channel close is an interrupted stream. Upstream error frames,
malformed JSON and SSE lines over 1 MiB fail explicitly instead of being ignored
or consuming unbounded memory. Parser cancellation also closes the response
body, including when it is blocked in a read.

Before response headers are committed, failures use the protocol's JSON error
response. Afterwards, recoverable upstream failures emit a protocol-specific
SSE error and close without a success terminator. The HTTP status may already
be 200; the persisted request summary records the actual failure status/code.
Broken downstream connections are closed without attempting further writes.

| Error code | Meaning |
| --- | --- |
| `upstream_first_token_timeout` | No model output within the first-token budget |
| `upstream_stream_idle_timeout` | Output stopped for longer than the idle budget |
| `upstream_timeout` | Total provider request deadline expired |
| `upstream_stream_error` | Provider sent an error inside its stream |
| `invalid_upstream_stream` | Wrong response type or malformed stream frame |
| `stream_interrupted` | Stream ended without valid completion, or reading failed |
| `client_write_timeout` | Client stopped consuming the response |
| `client_write_failed` / `client_cancelled` | Downstream connection failed or request was cancelled |
| `provider_busy` | Provider concurrency limit reached; retry after the advertised delay |
| `authentication_busy` | Cold key verification capacity reached; retry shortly |

Provider admission rejects overload immediately rather than maintaining an
unbounded queue. A live counter applies changed concurrency limits to new
requests while accounting for requests already in flight. Failed/cancelled
requests stop their producers and finish telemetry before releasing their slot.
