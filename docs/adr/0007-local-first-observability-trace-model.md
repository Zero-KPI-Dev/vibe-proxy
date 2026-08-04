# ADR-0007: Adopt a local-first observability trace and session model

## Status

Accepted

## Decision date

2026-08-03

## Recorded date

2026-08-02

## Context

vibe-proxy currently persists one flat summary per completed LLM request. The
summary supports recent requests, aggregate metrics, and multimodal routing
diagnostics, but it cannot represent distributed trace context, internal gateway
stages, multi-request sessions, calling-agent identity, request/response payload
snapshots, or changes between conversation turns.

The product is local-first and agent-first. Richer observability must therefore
work in the single-process desktop and CLI deployment without requiring an
external service. At the same time, LLM inputs and outputs can contain credentials,
private source code, personal data, image bytes, and proprietary prompts. Adding
payload capture changes both the persisted data model and the security boundary.

Open-source LLM observability systems commonly distinguish observations, traces,
and sessions. Gateways also accept W3C trace context and explicit session metadata.
OpenTelemetry GenAI semantic conventions define conversation and Agent attributes
while treating prompt and response content as opt-in.

## Decision

Adopt a layered, local-first observability model stored in the existing embedded
SQLite database.

- Keep `request_id` as the identity of one inbound LLM HTTP request.
- Add W3C-compatible `trace_id`, `span_id`, and `parent_span_id`. Honor valid
  incoming trace context and generate a new trace when it is absent.
- Add an optional explicit `session_id` for conversations, tasks, and workflows
  that span requests, plus an optional bounded `session_path` for logical workflow
  steps. Do not automatically group requests merely because they share an API key
  or User-Agent.
- Separate the authenticated principal from the reported/detected Agent identity,
  project, and session. Diagnostic identity fields are untrusted and do not grant
  authorization or change routing.
- Represent gateway-owned work such as parsing, model resolution, preprocessing,
  OCR, fallback, upstream invocation, and streaming as bounded child observations.
- Retain the existing `request_logs` summary table and compatibility APIs while
  adding typed identity/trace columns, numbered schema migrations, an observation
  table, and a separately retained payload-snapshot table.
- Keep SQLite as the Local Mode source of truth. OpenTelemetry/Langfuse export may
  be added later as optional sinks and must not become a runtime requirement.
- Make prompt/response capture opt-in. Metadata-only recording remains the
  default. Payload capture has mandatory redaction, binary omission, per-snapshot
  size limits, independent retention, and a disk quota.
- Preserve summaries when content expires, is dropped, or is deleted. The capture
  state must remain visible to operators.
- Use a bounded asynchronous recorder. Request summaries have priority over stage
  observations, which have priority over payloads. Telemetry failure must not fail
  an otherwise valid model request.
- Derive sessions from indexed request rows and store only operator annotations
  separately. Compute request diffs on demand from versioned canonical comparison
  shapes rather than persisting a second authoritative conversation history.
- Keep high-cardinality request, trace, session, project, and Agent identifiers out
  of Prometheus labels.

## Consequences

### Positive

- Local users can debug an Agent workflow without deploying an external
  observability stack.
- Session replay, request-body inspection, and context-change analysis share one
  coherent identity model.
- Explicit principal/Agent separation avoids attributing all calls made with one
  credential to the same application.
- W3C and OpenTelemetry-compatible fields make future export possible without
  rewriting stored semantics.
- Independent payload retention and payload-first eviction control disk growth and
  reduce privacy exposure.
- Stable observations can show OCR, fallback, upstream, and streaming behavior on
  one timeline.

### Negative

- The SQLite schema, migration system, telemetry interfaces, Admin API, and UI all
  become more complex.
- Asynchronous recording can lose the newest queued telemetry on a process crash.
- Detailed capture can store sensitive prompt or response text even after
  redaction; users must opt in knowingly.
- Repeated full conversation histories can consume significant disk space. Quotas
  and short content retention reduce, but do not eliminate, this duplication.
- Agent detection from User-Agent remains heuristic and requires maintained
  signatures.
- A gateway cannot observe tool/retrieval work that never crosses its boundary.

### Neutral

- The existing unencrypted SQLite trust boundary remains unchanged; this decision
  does not add database encryption.
- Existing recent-request and metrics responses remain supported while richer
  endpoints are introduced.
- Sessions may contain multiple agents and concurrent requests; chronological
  order is not automatically treated as causality.

## Alternatives considered

### Extend the flat request row with large JSON bodies

Rejected because it couples payload retention to summary retention, bloats common
list queries, and cannot model child stages or multiple upstream attempts well.

### Require an external observability backend

Rejected because Langfuse, Helicone, or an OpenTelemetry backend would add
services, credentials, networking, availability, and privacy concerns to the
default local workflow. Optional export remains compatible with this decision.

### Infer sessions automatically from API keys or prompt similarity

Rejected because shared keys and concurrent agents would mix unrelated work.
Explicit session identifiers and an honest unclassified state are more reliable.

### Store all content and wire frames by default

Rejected because prompts, responses, tool arguments, images, and headers can
contain sensitive information and unbounded data. Content capture remains opt-in,
bounded, sanitized, and separately retained.

### Use Prometheus labels for Agent and session analysis

Rejected because unconstrained Agent/session identifiers create high cardinality.
SQLite-backed queries provide these dimensions; Prometheus remains operational and
low-cardinality.

## Security and operational considerations

- Content capture must default to metadata-only and show an explicit warning before
  it is enabled.
- Authorization headers, cookies, admin/provider credentials, binary image/file
  payloads, OCR text, and unallowlisted headers must not be stored in metadata mode.
- Structured and raw capture require size limits, truncation markers, mandatory
  redaction, independent expiry, disk quotas, and exact-ID deletion.
- Agent, project, session, and baggage values are bounded untrusted strings and are
  never authorization claims.
- Admin detail endpoints retain the existing admin authentication and same-origin
  desktop protections, return `Cache-Control: no-store`, and render JSON as escaped
  text.
- Database migrations are numbered and transactional. Startup fails safely if a
  migration cannot complete.
- Recorder queue saturation and SQLite failures are counted by the recorder.
  Payloads are dropped before summaries, and telemetry failure does not block
  provider traffic. Those process-local counters can be connected to a future
  operational metric without adding request identifiers as labels.
- Graceful shutdown attempts a bounded queue flush; crash recovery relies on
  SQLite WAL guarantees for committed rows.
- OpenTelemetry or other external export, if added, is disabled by default and has
  an independent content-export opt-in.

## References

- [Local Observability, Tracing, and Session Replay Design](../superpowers/specs/2026-08-02-local-observability-tracing-design.md)
- [Architecture](../architecture.md)
- [ADR-0001: Canonical IR adapter boundary](0001-canonical-ir-adapter-boundary.md)
- [ADR-0004: Pure-Go SQLite storage](0004-pure-go-sqlite-storage.md)
- [Pull request #15](https://github.com/a448582655/vibe-proxy/pull/15)
- [Langfuse observability data model](https://langfuse.com/docs/observability/data-model)
- [Helicone sessions](https://docs.helicone.ai/features/sessions)
- [Helicone omit logs](https://docs.helicone.ai/features/advanced-usage/omit-logs)
- [Portkey tracing](https://portkey.ai/docs/product/observability/traces)
- [OpenTelemetry GenAI semantic conventions](https://github.com/open-telemetry/semantic-conventions/blob/main/model/gen-ai/spans.yaml)
