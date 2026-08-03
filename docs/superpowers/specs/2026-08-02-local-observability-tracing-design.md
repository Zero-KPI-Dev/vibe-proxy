# Local Observability, Tracing, and Session Replay Design

## Summary

vibe-proxy currently records a useful request summary: request ID, authenticated
client-key name, requested and upstream models, provider, protocols, latency,
status, token usage, and multimodal transformation metadata. That is enough for
aggregate charts and a recent-request table, but it cannot answer the questions
that matter when an agent behaves unexpectedly:

- What did this caller actually send?
- Which application or agent initiated the request, independently of the API key
  used to authenticate it?
- Which requests belong to the same conversation or workflow?
- What routing, preprocessing, fallback, and upstream steps occurred?
- What changed between two requests in the same session?

This design adds a local-first trace model on top of the existing telemetry
pipeline. It keeps SQLite as the built-in source of truth, preserves the current
summary and metrics endpoints, makes content capture explicitly opt-in, and
leaves OpenTelemetry export as an optional extension rather than a runtime
dependency.

The core hierarchy is:

```text
Session (conversation, task, or workflow)
  -> Distributed trace (caller-provided or generated)
      -> Gateway request (one inbound LLM HTTP call)
          -> Observations (parse, resolve, preprocess, OCR, upstream, stream)
              -> Payload snapshots (optional, bounded, separately retained)
```

## Current State and Gaps

### Existing capabilities

- `telemetry.Event` records request, route, protocol, timing, status, usage, and
  a typed multimodal transformation summary.
- `RecentStore` retains active requests and a bounded in-memory history.
- SQLite persists finished request summaries in `request_logs` and aggregates
  time-bucketed metrics.
- Prometheus exports low-cardinality request, latency, throughput, and token
  metrics.
- Canonical IR already has `AgentID`, `ProjectID`, and `Metadata` fields.
- Client adapters already retain the parsed inbound JSON in a namespaced vendor
  extension for the lifetime of the request.
- The UI has aggregate charts, a recent-request table, and a multimodal flow
  dialog.

### Missing or misleading pieces

- `client_name` is the authenticated client-key name. It identifies a credential,
  not necessarily the calling agent.
- `AgentID`, `ProjectID`, and request metadata are not populated by the current
  client adapters.
- `agent_profiles` is documented and parsed into `SimpleConfig`, but it is not
  compiled into `RuntimeConfig`; it does not currently classify requests.
- There is no `session_id`, distributed `trace_id`, span relationship, or parent
  request relationship.
- Tracking starts after authentication, parsing, authorization, and model
  resolution. Failures in those stages are therefore absent from request history.
- SQLite receives only the final summary; request input, normalized output, and
  internal stage timings are not persisted.
- `/admin/requests/recent` is fixed to 50 rows and has no cursor, filtering, detail,
  session, or diff API.
- The dashboard cannot distinguish a model switch, prompt rewrite, tool-set
  change, context growth, context shrink, retry, or fallback across requests.

## Goals

- Show detailed, sanitized request and response snapshots when the operator has
  explicitly enabled content capture.
- Separate authenticated principal, detected agent, project, session, request,
  distributed trace, and internal observation identities.
- Group related LLM requests into sessions without pretending unrelated calls are
  connected.
- Provide a chronological session replay and a structural/content diff between
  requests.
- Show gateway-owned stages such as parsing, model resolution, OCR, fallback,
  upstream wait, first token, and completion on one timeline.
- Filter summaries and metrics by agent, principal, project, model, provider,
  protocol, status, and session.
- Keep the local data plane responsive when storage is slow or full.
- Keep content local by default and never expose it through Prometheus.
- Define field names that can map cleanly to OpenTelemetry GenAI conventions.

## Non-goals

- Reimplementing Langfuse, Helicone, or a general-purpose APM backend.
- Inferring an exact agent or session from arbitrary prompt text.
- Observing local tool executions, retrieval calls, or agent reasoning that never
  pass through vibe-proxy. Those require client-side instrumentation or a future
  external observation ingestion API.
- Storing image/file bytes by default.
- Adding Postgres, ClickHouse, Kafka, or another mandatory service to Local Mode.
- Using agent/session metadata for authorization or routing in this change.
- High-cardinality Prometheus labels for agents, sessions, projects, or request
  IDs.

## Reference Model

The design borrows semantics, not deployment architecture, from established
open-source systems:

| Project or standard | Useful idea | Application in vibe-proxy |
| --- | --- | --- |
| Langfuse | Observations form traces; sessions group multiple traces | Use distinct observation, request/trace, and session levels |
| Helicone | Explicit session ID/path/name and per-request content omission | Accept explicit session metadata and allow capture to be reduced per request |
| Portkey | W3C `traceparent`/`baggage`; retries and fallbacks share a timeline | Preserve trace context and represent gateway routing attempts as child observations |
| LiteLLM | Attribute usage and spend to virtual keys, projects, and users | Keep authenticated principal separate from project and agent dimensions |
| OpenTelemetry GenAI | `gen_ai.conversation.id`, Agent identity, opt-in prompt/response content | Use compatible internal names and keep payload capture opt-in |

## Identity Model

The following fields are deliberately independent:

| Identity | Meaning | Example |
| --- | --- | --- |
| `principal_name` | Client key that authenticated the request | `local-codex-key` |
| `agent_id` | Stable calling agent/application identity | `codex-desktop` |
| `agent_name` | Human-readable product or configured profile | `Codex Desktop` |
| `agent_version` | Reported client/agent version | `1.22.0` |
| `project_id` | Optional workspace/application dimension | `vibe-proxy` |
| `session_id` | Conversation, task, or workflow across requests | `019f...` |
| `session_path` | Optional logical step path inside a workflow | `/review/security` |
| `request_id` | One inbound LLM HTTP call | UUID |
| `trace_id` | W3C distributed trace; may span multiple calls | 32 hex chars |
| `span_id` | Gateway span for this request | 16 hex chars |
| `parent_span_id` | Calling span from `traceparent` | 16 hex chars |
| `parent_request_id` | Optional causal predecessor inside a session | UUID |

### Agent classification precedence

1. Explicit `X-Vibe-Agent-ID`, `X-Vibe-Agent-Name`, and
   `X-Vibe-Agent-Version` headers.
2. Allowlisted W3C baggage fields such as `gen_ai.agent.id`,
   `gen_ai.agent.name`, and `gen_ai.agent.version`.
3. Protocol-native metadata mapped by the client adapter.
4. A configured `agent_profiles` detector matching an allowlisted header or
   `User-Agent` pattern.
5. Built-in, versioned User-Agent signatures for well-known tools.
6. `unknown`.

Every classified request stores `agent_source` (`header`, `baggage`,
`protocol_metadata`, `profile`, `user_agent`, or `unknown`) and a confidence
level (`explicit`, `configured`, or `heuristic`). The authenticated key remains
`principal_name`; it is never silently relabeled as the agent.

Agent labels are diagnostic input. They must not grant permissions, bypass model
restrictions, or affect routing unless a later ADR creates a trusted policy
boundary.

### Session classification precedence

1. Explicit `X-Vibe-Session-ID` and optional `X-Vibe-Session-Name`,
   `X-Vibe-Session-Kind`, and `X-Vibe-Session-Path`.
2. Allowlisted baggage key `gen_ai.conversation.id`.
3. Protocol-native conversation/session metadata when the protocol provides a
   stable identifier.
4. No session.

The gateway must not group all calls sharing an API key or User-Agent into one
session. Unclassified calls appear under “No session”. `session_path` uses a
bounded slash-separated form such as `/review/security` to visualize nested or
parallel workflow steps; it does not replace trace/span parentage. A future opt-in
heuristic may suggest grouping, but it must be visibly marked as inferred and
never rewrite the recorded identity.

### Trace context

- Accept valid W3C `traceparent` and `tracestate` headers.
- Use their trace and parent span IDs, then generate a gateway span ID.
- Generate a new trace ID when no valid context exists.
- Accept `X-Vibe-Parent-Request-ID` for causal links between gateway calls.
- Do not forward baggage to an LLM provider by default because baggage may carry
  user or project identifiers.
- Upstream `traceparent` propagation is an explicit configuration option.

## High-Level Architecture

```text
Client request
  -> Trace envelope (request ID, trace context, principal, initial timestamp)
  -> Authentication and agent/session classification
  -> Client adapter parse + optional inbound snapshot
  -> Canonical request shape summary
  -> Model resolution
  -> Preprocessors (OCR / Vision fallback)
  -> Provider adapter + optional effective-upstream snapshot
  -> Upstream invocation and streaming
  -> Canonical response accumulator (bounded)
  -> Client encoding
  -> Finished request summary
       |-> RecentStore (live UI)
       |-> Prometheus (low-cardinality metrics only)
       `-> bounded recorder queue
             -> SQLite batch writer
                  |- request_logs
                  |- trace_observations
                  |- payload_snapshots
                  `- session_annotations
```

The trace envelope must be created after protocol detection and before
authentication so authentication, parsing, model authorization, and resolution
errors can be recorded. It must not read a body before the selected adapter does;
the adapter remains responsible for protocol parsing and body limits.

## Telemetry Event Model

### Request summary

Extend the existing event rather than creating a parallel request model. New
summary fields include:

- trace, span, session/path, parent request, principal, agent, project, and source;
- HTTP method/path and safe User-Agent classification metadata;
- total duration in addition to TTFT, TPOT, and TPS;
- input message/block/tool/image counts and aggregate text characters;
- response text/tool-call/reasoning counts;
- requested, initially resolved, and effective provider/model;
- finish reason, upstream request ID when returned, and retry/fallback count;
- capture mode/status, truncation flags, and redaction count;
- context-shape summary used when payload content is unavailable.

The existing JSON fields remain valid for compatibility. `client_name` remains
readable during migration but is documented as the legacy alias of
`principal_name`.

### Observations

An observation is a timed gateway-owned step with an ID, parent ID, type, name,
status, timestamps, and bounded JSON attributes. Initial observation names are:

- `gateway.request`
- `client.authenticate`
- `client.parse`
- `model.resolve`
- `preprocess.multimodal`
- `ocr.invoke`
- `route.fallback`
- `provider.build_request`
- `provider.invoke`
- `stream.first_token`
- `client.encode`

Very small stages may be represented as events on the gateway observation rather
than independent duration spans. Stable names matter more than recording every
function call. Provider retries or fallbacks each receive a separate
`provider.invoke` child so failures and the final success are visible in order.

## Payload Capture and Privacy

### Capture modes

```yaml
observability:
  capture:
    mode: metadata       # metadata | structured | raw
    max_snapshot_bytes: 262144
    capture_response: true
    capture_reasoning: false
    image_payloads: metadata
    header_allowlist: [user-agent, traceparent]
  retention:
    summaries_days: 14
    content_days: 3
    max_content_storage_mb: 512
```

- `metadata` is the default and preserves the current privacy posture. It stores
  counts, timings, route decisions, and structural context changes but no prompt,
  response, tool arguments, OCR text, image URL, or file content.
- `structured` stores sanitized JSON snapshots and normalized text/tool content.
  Images and files become descriptors containing media type, size, dimensions
  when available, and a digest; bytes and data URLs are omitted.
- `raw` is an advanced, explicitly warned mode that stores bounded wire JSON
  after mandatory secret/header redaction. Binary payloads are still omitted.
- A request header may reduce capture (`X-Vibe-Capture: metadata|off`) but cannot
  elevate it beyond the administrator’s configured maximum.

No mode stores `Authorization`, cookies, provider credentials, admin tokens, or
unallowlisted headers. Sanitizers walk both known protocol fields and generic
JSON keys. Configurable JSON-pointer redaction rules supplement built-ins such as
`api_key`, `token`, `password`, and `secret`, but the UI must clearly state that
arbitrary prompt text can still contain sensitive data.

### Snapshot stages

When enabled, keep separate snapshots rather than one ambiguous “body”:

1. `client_request`: sanitized original client JSON;
2. `canonical_request`: normalized IR before preprocessing;
3. `ocr_request` and `ocr_response`: normalized OCR invocation metadata and result;
4. `vision_request` and `vision_response`: bounded Vision helper wire request and canonical result when assist runs;
5. `effective_canonical_request`: IR after OCR or Vision evidence replacement;
6. `upstream_request`: provider JSON for the model that will answer;
7. `canonical_response`: accumulated normalized output.

Streaming responses are accumulated from canonical stream events under the same
size budget; raw SSE frames are not persisted in the first release. Every
snapshot records original size, stored size, media type, schema version,
truncation reason, redaction count, and SHA-256 integrity digest.

Payload retention and quota are independent of summary retention. On quota
pressure, evict the oldest payloads first and preserve request summaries. The UI
shows `not_captured`, `captured`, `redacted`, `truncated`, `expired`, or
`dropped` rather than displaying an unexplained empty body.

## Storage Design

SQLite remains the local source of truth. Introduce numbered schema migrations
before expanding the schema; the current ad-hoc `ALTER TABLE` pattern should not
carry a multi-table observability model.

### `request_logs`

Retain the table and existing columns. Add identity, trace, duration, route,
shape, and capture-status columns. Required indexes:

```text
(started_at DESC, request_id)
(session_id, started_at, request_id)
(agent_id, started_at DESC)
(project_id, started_at DESC)
(trace_id, started_at, request_id)
(status_code, started_at DESC)
```

### `trace_observations`

```text
observation_id PRIMARY KEY
request_id
trace_id
span_id
parent_span_id
type
name
started_at
completed_at
status
error_code
attributes_json
```

Attributes are bounded and schema-versioned. Common filter dimensions stay in
typed `request_logs` columns rather than being hidden in JSON.

### `payload_snapshots`

```text
request_id + stage PRIMARY KEY
schema_version
capture_mode
media_type
content_encoding
body_blob
original_bytes
stored_bytes
truncated
truncation_reason
redaction_count
sha256
created_at
expires_at
```

Snapshot bodies may be compressed by the application before insertion. Do not
introduce cross-request content-addressed deduplication initially: it complicates
secure deletion and reference accounting. Separate retention and a hard quota
bound the repeated-history cost.

### `session_annotations`

Sessions are derived from `request_logs.session_id`. A small optional table stores
operator-authored name, kind, tags, bookmark, and notes without duplicating
aggregates. Request count, token totals, error count, first seen, and last seen
are queried from indexed request rows.

## Session Replay and Request Diff

### Choosing the comparison base

1. Use `parent_request_id` when it references a request in the same session.
2. Otherwise choose the latest request in the same session that completed before
   the current request started.
3. If requests overlap, label the default comparison as chronological, not
   causal, and allow the operator to select another base.

### Normalization

Build a versioned comparison shape from Canonical IR:

- ordered messages with role and ordered content-block types;
- text, reasoning, image/file descriptors, tool calls, and tool results;
- system instructions;
- tool names and schema fingerprints;
- model, stream mode, temperature, top-p, max tokens, stop sequences, response
  format, and relevant vendor extensions;
- resolved route and preprocessing decisions.

In metadata mode, compare counts, roles, block types, tool names, parameter
values, and lengths only. Do not persist content hashes when content capture is
disabled. In structured/raw mode, compute message fingerprints and use a
sequence diff to identify retained, added, removed, or modified messages.

### Diff result

The API returns machine-readable changes plus a short summary, for example:

```text
+ 1 user message, + 1 tool result
24 previous messages retained
system instruction changed
tools changed: 12 -> 14
context text changed: 18,420 -> 7,210 chars
requested model unchanged; effective provider changed
```

A large context reduction can be labeled “possible compaction” in the UI, but
the persisted fact is only `context_shrink`; the gateway must not claim to know
the caller’s intent. Text diffs are computed on demand and bounded by message
count and bytes. Large blocks fall back to digest/size summaries.

## Admin API

Keep existing endpoints for compatibility and add a cursor-based API:

```text
GET    /admin/observability/requests
GET    /admin/observability/requests/{request_id}
GET    /admin/observability/requests/{request_id}/diff?base={request_id|previous}
GET    /admin/observability/sessions
GET    /admin/observability/sessions/{session_id}
PATCH  /admin/observability/sessions/{session_id}
DELETE /admin/observability/requests/{request_id}/content
GET    /admin/observability/live
GET    /admin/observability/settings
PUT    /admin/observability/settings
```

List filters include time range, principal, agent, project, session, model,
provider, protocol, status/error, streaming, multimodal route, and capture state.
Pagination uses `(started_at, request_id)` cursors instead of offset. Detail
responses are `Cache-Control: no-store`; payload endpoints use the existing
Admin authentication boundary.

`/admin/observability/live` is Server-Sent Events. It emits bounded lifecycle
updates for active requests and invalidates list/session queries; it does not
stream prompt or completion content. The existing five-second polling path
remains a fallback.

## User Interface

### Overview

Keep the current summary charts and add shared filters. Add cards for error
rate, p95 total duration, active requests, sessions, and fallback count. Agent,
project, and session dimensions are queried from SQLite and are not Prometheus
labels.

### Requests

Replace the fixed recent table with a searchable, paginated table:

```text
Time | Agent | Principal | Session | Requested -> Effective model | Provider |
Status | TTFT / Total | Tokens | Capture
```

Opening a request navigates to a detail view with tabs:

- Summary: identities, route, usage, error, and capture state;
- Timeline: ordered observations and durations;
- Request: original, canonical, and effective-upstream JSON views;
- Response: normalized output and tool calls when captured;
- Changes: default or operator-selected comparison;
- Metadata: allowlisted headers, protocol metadata, and transformation details.

Secrets and removed binary data appear as explicit placeholders. Copy/export is
an intentional button action, never automatic clipboard access.

### Sessions

The Sessions view groups requests by explicit session ID and supports Agent,
project, kind, status, and time filters. A session detail page includes:

- chronological request cards, optional session-path tree, and causal links when
  available;
- model/provider switches, fallbacks, errors, and recoveries;
- prompt-token, context-size, and latency evolution charts;
- markers for new/removed messages, tool-set changes, system-prompt changes,
  images, and context shrink;
- two-request selection for manual comparison;
- session name, tags, bookmark, and operator notes.

Multi-agent sessions show Agent swimlanes instead of collapsing all calls into a
single identity.

## Metrics

SQLite-backed dashboards may aggregate by principal, agent, project, protocol,
requested/effective model, provider, status, and fallback route. Initial metrics
include request count, error rate, total latency, TTFT, TPOT, TPS, prompt/output/
cache tokens, context growth, and fallback/OCR counts.

Prometheus remains deliberately low cardinality. It may add stage-duration and
recorder-drop counters, but must not use `request_id`, `trace_id`, `session_id`,
`project_id`, or unconstrained `agent_id` as labels.

Exact monetary cost is deferred until the gateway has a versioned pricing source
and correct handling for cached, reasoning, batch, and provider-specific token
classes. Estimated cost must never be presented as invoice truth.

## Performance, Reliability, and Failure Handling

- Metadata-only tracing target: less than 1 ms p95 gateway CPU overhead excluding
  the existing SQLite write.
- Structured capture target: less than 3 ms p95 for bodies within the configured
  snapshot cap, measured independently from provider latency.
- Request list/session queries target: less than 200 ms p95 for 100,000 retained
  summaries on a developer laptop.
- Use a bounded recorder queue and a single SQLite writer that batches related
  rows in one transaction.
- Summary events have priority over observations; observations have priority over
  payloads. Under pressure, drop payloads first and mark their state.
- Queue saturation, write errors, cleanup failures, and dropped records are
  counted and visible in health/Prometheus metrics without recursively logging
  more telemetry.
- Flush the queue during graceful shutdown with a bounded deadline. A crash may
  lose the newest queued events; it must not corrupt committed rows.
- Run retention on startup and periodically. Content expiry or quota eviction
  never deletes the request summary.
- If a migration fails, startup fails at the database stage and leaves the prior
  schema/data intact through transactional migration.
- If detailed capture fails, the model request continues and the summary records
  `capture_status=dropped|error`.

## Security Considerations

- Detailed prompt/response capture is off by default and requires a clear warning
  in both configuration documentation and the UI.
- The SQLite database is not encrypted by this proposal. Operators must rely on
  OS file permissions and should avoid raw capture on shared machines.
- Content is never sent externally unless a later exporter is explicitly enabled;
  even then payload export remains a separate opt-in.
- Image/file bytes, remote URLs, OCR text, credentials, auth headers, cookies, and
  unallowlisted headers remain excluded from metadata mode.
- Agent, project, and session identifiers are untrusted labels. Bound length and
  character sets, reject control characters, and cap metadata count/size.
- Admin detail APIs use existing Admin authentication, same-origin protection for
  desktop sessions, `no-store`, and escaped rendering.
- JSON viewers render data as text and must not interpret embedded HTML/Markdown.
- Deletion and retention operate on exact request/session IDs, never on unchecked
  filesystem paths.

## Rollout Plan

### Phase 1: Trace envelope and queryable summaries

- Add numbered SQLite migrations.
- Move request tracking before authentication/parsing.
- Add trace, session, principal, agent, project, duration, and request-shape fields.
- Compile `agent_profiles` and implement explicit header/baggage detection.
- Add cursor/filter request and session APIs while preserving old endpoints.

### Phase 2: Bounded payload capture and detail view

- Implement capture configuration, mandatory sanitizer, snapshot storage,
  retention, quota, and delete-content API.
- Capture original/canonical/effective request stages and bounded canonical output.
- Add request detail tabs and capture-state UX.

### Phase 3: Session replay and diffs

- Add comparison shapes, previous/parent selection, structural and content diffs.
- Add session list, session timeline, Agent swimlanes, and context-evolution charts.
- Add annotations/bookmarks.

### Phase 4: Rich stage tracing and live updates

- Record stable internal observations for parse, route, OCR, upstream, and stream.
- Add SSE lifecycle updates and timeline visualization.
- Represent future retries and route attempts as sibling upstream observations.

### Phase 5: Optional interoperability

- Add an OTLP exporter with OpenTelemetry GenAI field mapping.
- Add an authenticated observation ingestion endpoint only if users need tool,
  retrieval, or agent spans that the gateway cannot observe.
- Keep content export independently disabled by default.

Each phase must ship with current-state architecture, Admin API, configuration,
and user documentation updates. Phase 1 is governed by the proposed ADR for the
new persistence, identity, and privacy boundary.

## Testing Strategy

- Unit tests for identity precedence, invalid trace context, length limits, and
  the rule that diagnostic labels never alter authorization.
- Adapter contract tests for protocol metadata extraction and unchanged protocol
  round trips.
- Sanitizer fixtures containing nested secrets, tool arguments, images, files,
  data URLs, malicious keys, huge strings, and invalid JSON.
- Migration tests from the current database fixture, including WAL recovery.
- Recorder tests for ordering, batching, queue saturation, payload-first drops,
  shutdown flush, and injected SQLite failures.
- Query tests for cursor stability, filters, overlapping requests, missing
  payloads, expired payloads, and session aggregates.
- Diff tests for append-only turns, message rewrite, tool-call/result additions,
  system changes, context shrink, model/provider switches, concurrent requests,
  truncation, and metadata-only mode.
- Frontend tests for no-content states, redaction/truncation badges, JSON escaping,
  session filtering, Agent swimlanes, and manual base selection.
- Benchmarks for 100,000 summaries, large sessions, bounded snapshots, and the
  data-plane overhead targets.

## Alternatives Considered

### Add JSON columns to the existing request row only

This is the fastest implementation, but large payloads would bloat list queries,
payload retention could not differ from summary retention, and a flat row cannot
represent routing/OCR/upstream timelines cleanly.

### Export everything to Langfuse or another external backend

This offers a mature UI but violates the default local, self-contained product
experience and introduces credentials, network availability, versioning, and
privacy obligations. Optional export remains valuable after the local model is
stable.

### Adopt a full OpenTelemetry collector and backend locally

This maximizes standards reuse but adds several processes and a heavy operational
surface for a desktop/local gateway. Internal semantics should map to OTel without
requiring an OTel deployment.

### Infer sessions from repeated prompts or client keys

This reduces client setup but creates false groupings, especially with concurrent
agents sharing a key. Explicit IDs and an honest “No session” state are safer.

### Store every raw streaming frame

This can reproduce wire behavior exactly but has high disk cost, leaks more
provider-specific content, and complicates useful inspection. Bounded canonical
output plus timing observations is sufficient for the first version.

## Open Questions for Review

The architecture is not blocked by these choices, but they should be confirmed
before implementation:

1. Should the desktop UI offer a one-click “structured capture for 24 hours”
   temporary mode, or only a persistent setting?
2. Is `raw` capture necessary in the first release, or should the first release
   stop at sanitized structured snapshots?
3. Which built-in Agent signatures are worth maintaining initially beyond Codex,
   Claude Code, Cursor, OpenCode, Cline, and Aider?

## References

- [vibe-proxy Architecture](../../architecture.md)
- [vibe-proxy Canonical IR](../../canonical-ir.md)
- [vibe-proxy Admin API](../../admin-api.md)
- [ADR-0001: Canonical IR adapter boundary](../../adr/0001-canonical-ir-adapter-boundary.md)
- [ADR-0004: Pure-Go SQLite storage](../../adr/0004-pure-go-sqlite-storage.md)
- [Langfuse observability data model](https://langfuse.com/docs/observability/data-model)
- [Langfuse tracing best practices](https://langfuse.com/docs/observability/best-practices)
- [Helicone sessions](https://docs.helicone.ai/features/sessions)
- [Helicone omit logs](https://docs.helicone.ai/features/advanced-usage/omit-logs)
- [Portkey tracing](https://portkey.ai/docs/product/observability/traces)
- [LiteLLM](https://docs.litellm.ai/)
- [OpenTelemetry GenAI semantic conventions](https://github.com/open-telemetry/semantic-conventions/blob/main/model/gen-ai/spans.yaml)
