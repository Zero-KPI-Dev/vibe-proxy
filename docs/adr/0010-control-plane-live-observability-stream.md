# ADR-0010: Deliver live request lifecycle events over control-plane SSE

## Status

Accepted

## Decision date

2026-08-09

## Recorded date

2026-08-09

## Context

The local observability store is intentionally durable and asynchronous. It
persists a request summary after the request finishes, which is appropriate for
history, filtering, retention, and crash-safe queries. It cannot by itself give
an operator immediate feedback that a request has arrived, authenticated,
routed, reached its upstream, or produced its first token.

Polling the durable request list would delay updates, repeatedly query SQLite,
and still fail to represent in-flight lifecycle transitions. WebSocket would
support bidirectional messages that this feature does not need. The live surface
must also remain independent from the LLM response stream and must never add
storage or browser backpressure to the data-plane request path.

## Decision

Add an in-memory, bounded live-event broker in the telemetry package and expose
it through an authenticated Server-Sent Events endpoint on the loopback
control-plane listener.

The required boundaries and invariants are:

1. The broker is an `EventSink` companion. Runtime request code emits typed
   lifecycle checkpoints through the tracker; it does not know about HTTP, SSE,
   React, or subscriber state.
2. Lifecycle events use a small stable vocabulary: `started`, `updated`,
   `first_token`, `progress`, `finished`, and `failed`. A separate phase identifies
   `received`, `authenticated`, `parsed`, `routed`, `preprocessing`,
   `upstream_started`, `streaming`, or `completed`.
3. The SSE endpoint is mounted only under `/admin/observability/live` on the
   control-plane router and uses the existing Admin authorization boundary.
4. Live events contain request summary metadata only. Prompt/response bodies,
   image bytes, captured payloads, authorization headers, and provider secrets
   are never published through the live broker.
5. Publishing is non-blocking. Every subscriber has a bounded queue. A slow
   subscriber is disconnected and can resume from the broker's bounded replay
   window using its last event ID.
6. The broker maintains only a short process-local replay ring. SQLite remains
   the source of truth for completed history; the broker is not a second
   persistence layer.
7. High-frequency token updates are coalesced. The data plane does not emit one
   browser event per model token.
8. SSE is independent from provider and client streaming. A failure or disconnect
   of the observability stream cannot alter an LLM response.

The frontend consumes SSE with authenticated `fetch` and `ReadableStream`
rather than native `EventSource`, because Admin bearer authentication may need an
`Authorization` header. It merges live events by `request_id` with paginated
SQLite history and reconnects with the last received event ID.

## Consequences

### Positive

- Requests become visible as soon as they enter the gateway.
- Operators can distinguish authentication, routing, upstream wait, first-token,
  and completion time without polling SQLite.
- One-way SSE matches the actual communication pattern and works in browsers and
  the desktop WebView.
- The bounded broker preserves the existing rule that telemetry cannot slow the
  model request path.

### Negative

- The process owns subscriber goroutines, replay memory, heartbeat timers, and
  reconnect semantics.
- Live state is lost on process restart and must be reconciled with SQLite
  history.
- A disconnected slow client may miss events outside the replay window.

### Neutral

- Existing request-history and metrics APIs remain valid.
- The live stream is a local control-plane feature, not a public data-plane API.

## Alternatives considered

### Poll the request-history API every second

Rejected because it creates avoidable SQLite load, delays updates, and cannot
faithfully show lifecycle transitions before the final summary is persisted.

### Use WebSocket

Rejected because live observability requires only server-to-client delivery.
WebSocket would add a bidirectional protocol, message framing, and lifecycle
surface without a corresponding product need.

### Persist every lifecycle transition before displaying it

Rejected because synchronous persistence would couple observability latency and
failure to the data plane. Durable bounded observations remain available through
the existing asynchronous recorder.

## Security and operational considerations

- The endpoint inherits Admin authentication and loopback-only listener
  isolation from ADR-0009.
- Responses use `Cache-Control: no-store, no-transform` and disable proxy
  buffering.
- Subscriber count, queue size, replay size, heartbeat frequency, and progress
  frequency are bounded constants.
- Event IDs are process-local sequence numbers and are not authorization or
  globally durable identifiers.

## References

- [ADR-0007: Local-first observability trace model](0007-local-first-observability-trace-model.md)
- [ADR-0009: Separate data and control-plane listeners](0009-separate-data-and-control-plane-listeners.md)
- [Live Observability V2 Design](../observability-live-v2-design.md)

