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

TTFT is measured from request acceptance to the first non-empty content or tool delta flushed to the client.

Not every upstream event counts as first token. For example:

- metadata-only event: does not count
- role-only event: does not count
- empty delta: does not count
- content text delta: counts
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
