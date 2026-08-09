# Live Observability V2 Design

## Goal

Turn the existing local request history into a usable traffic cockpit without
coupling the data plane to the browser, SQLite latency, or an external
observability stack.

The operator should be able to answer, in one place:

- Did a request just enter vibe-proxy and where is it now?
- Which Agent and Client Key initiated it?
- Which virtual model, provider, and upstream model handled it?
- Did OCR, Vision assistance, fallback, or prompt caching take effect?
- What were total latency, TTFT, TPOT, throughput, and token usage?

## Module boundaries

```text
runtime request pipeline
        |
        | typed tracker checkpoints
        v
telemetry.EventSink / RequestUpdateSink
        |
        +--> AsyncRecorder --> SQLite --> history/query APIs
        +--> Prometheus --> /metrics
        +--> RecentStore --> compatibility recent API
        +--> LiveBroker --> authenticated control-plane SSE
                                  |
                                  v
                            React live view
```

Rules:

- Runtime emits domain lifecycle events; it never writes SSE frames.
- The live broker never queries or writes SQLite.
- The frontend merges by `request_id`; it does not treat the event stream as
  durable history.
- Prometheus receives low-cardinality operational dimensions only.
- Client Key and Agent drill-down remains in the SQLite-backed local UI.

## Request lifecycle

| Live kind | Phase | Required fields available |
| --- | --- | --- |
| `started` | `received` | request/trace IDs, HTTP path, protocol, start time |
| `updated` | `authenticated` | principal type/name and safe Client Key prefix |
| `updated` | `parsed` | Agent/session/project, requested model, request shape |
| `updated` | `routed` | initial provider/model and output protocol |
| `updated` | `preprocessing` | OCR/Vision decision and effective target |
| `updated` | `upstream_started` | effective provider/model |
| `first_token` | `streaming` | TTFT and first-token timestamp |
| `progress` | `streaming` | throttled output-token progress and elapsed time |
| `finished` | `completed` | status, duration, usage, TTFT/TPOT/TPS |
| `failed` | `completed` | status, duration, failure code and last known phase |

The first event can precede authentication. The same row is enriched by later
events rather than being replaced by unrelated log lines.

## Identity presentation

Persist and return independent identity dimensions:

- `principal_type`: `client_key`, `internal`, or `unknown`;
- `principal_name`: configured Client Key name or internal caller identifier;
- `client_key_prefix`: safe prefix captured at authentication time;
- Agent fields from the existing observability identity model.

The Admin Playground uses a trusted internal invocation context:

```text
principal_type   internal
principal_name   admin-playground
agent_id         vibe-proxy-playground
agent_name       Vibe Proxy Playground
agent_source     internal
agent_confidence explicit
```

UI wording uses "Call source"/"调用来源" rather than exposing
`principal_name` as unexplained protocol terminology.

## SSE protocol

Endpoint:

```text
GET /admin/observability/live
Authorization: Bearer <admin token>
Accept: text/event-stream
Last-Event-ID: <process-local sequence>
```

Frame:

```text
id: 42
event: request.updated
data: {"id":42,"kind":"updated","phase":"routed","emitted_at":"...","request":{...}}
```

The server sends heartbeat comments, replays events still present after
`Last-Event-ID`, and disconnects slow subscribers. The browser reconnects with
exponential backoff capped at a few seconds. Authorization failures trigger the
existing login flow.

## Cache and performance semantics

The authoritative cache definitions are in ADR-0011. The request list and detail
view show:

- normalized input, output, cache-read, and cache-write tokens;
- per-request cache ratio or "not reported";
- total duration, TTFT, TPOT, and TPS with unavailable values shown as a dash;
- OCR and Vision cache state separately from provider prompt cache.

Overview aggregation adds time-bucketed cache-read/write token volume and the
weighted cache ratio. TPOT receives its own chart instead of being collected but
hidden behind TTFT.

## Prometheus and Grafana

vibe-proxy does not embed Prometheus or Grafana. It keeps the lightweight local
SQLite dashboard and publishes low-cardinality metrics for optional external
scraping.

Repository integration artifacts include:

- a documented `/metrics` contract;
- a sample Prometheus scrape configuration for a same-host deployment;
- a versioned Grafana dashboard JSON;
- an optional development Compose example, not a runtime dependency.

The metrics endpoint remains on the loopback control-plane listener. Remote
scraping requires a future dedicated metrics listener or a local collector; the
Admin listener will not be exposed to make scraping convenient.

## Frontend interaction

The Requests page gains a live mode enabled by default:

- connection state, pause/resume, and auto-follow controls;
- in-flight requests pinned above completed history;
- compact primary filters with an advanced-filter section;
- direct filtering by Agent, call source, model, provider, protocol, and status;
- performance and cache columns that remain readable at desktop widths;
- completed live rows reconcile with durable history by `request_id`.

No captured content is placed in list events. Request details continue to fetch
payloads through the authenticated detail API.

## Delivery sequence

1. Add ADRs, typed live broker, lifecycle checkpoint contract, and broker tests.
2. Add SSE endpoint, authentication/replay tests, and runtime checkpoints.
3. Normalize identity and cache usage with migrations and adapter tests.
4. Add frontend live state, request-table UX, and performance/cache panels.
5. Extend Prometheus metrics and ship Grafana integration artifacts.
6. Replace platform icon assets after small-size visual verification.
7. Run Go unit/race tests, frontend contract/build tests, and local end-to-end
   validation before merge.

