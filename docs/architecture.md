# vibe-proxy Architecture

## Product Positioning

`vibe-proxy` is a **local-first, agent-first, gateway-ready multi-protocol LLM proxy**.

Its primary job is to let different AI agents, IDEs, CLIs, and local tools connect to one stable local endpoint while `vibe-proxy` handles:

- client protocol compatibility
- provider protocol transformation
- provider authentication
- model aliasing and raw model passthrough
- runtime switching
- request observability
- debugging of agent/provider behavior

The first-class user is an individual developer running `vibe-proxy` locally. The architecture should leave room for future open-source gateway usage, but the default experience must remain simple.

## Design Principle

> Powerful core, simple surface.

This means:

- Internally, the system has clear boundaries for protocols, model resolution, provider auth, telemetry, policy, and control plane.
- Externally, the default config and UI should expose only what a local developer needs.
- Advanced gateway capabilities should be opt-in, not visible by default.

## Modes

### Local Mode

The default mode.

Characteristics:

- single user
- single process
- local SQLite storage
- local config file plus lightweight UI
- exposes separate data and control listeners, both on `127.0.0.1` by default
- provider secrets from env vars or local encrypted store
- simple client token or local-only access
- focused on agent compatibility and debugging

### Gateway Mode

A future optional mode.

Characteristics:

- team/shared deployment
- stronger admin auth
- multiple client keys
- quotas and budgets
- Postgres or external storage
- more advanced policy engine
- higher concurrency and HA features

Gateway Mode must not complicate Local Mode.

## High-Level Flow

```text
HTTP Request
  -> Listener / Protocol Detection
  -> Trace Envelope / Agent and Session Classification
  -> Client Authentication and Authorization
  -> Client Adapter
  -> Canonical IR
  -> Model Resolver
  -> Request Preprocessor Pipeline
  -> Channel Selector
  -> Upstream Auth Profile
  -> Provider Adapter
  -> Stream Engine
  -> Client Adapter Encoder
  -> Bounded Telemetry Recorder / Sinks
```

## Core Modules

### 1. Protocol Layer

Responsible for translating between external protocols and internal canonical structures.

Components:

- Client Adapter
- Provider Adapter
- Canonical IR
- Canonical Stream Event

Supported client protocols for v0.1:

- OpenAI Chat Completions
- OpenAI Responses API
- Anthropic Messages API

Supported provider protocols for v0.1:

- OpenAI-compatible
- Anthropic Messages

### 2. Stream Engine

Responsible for incremental streaming transformation.

It must support:

- SSE parsing and encoding
- event-by-event protocol conversion
- TTFT tracking
- token delta tracking
- trailing usage interception
- stream-to-unary accumulation
- unary-to-stream synthetic streaming where reasonable
- client disconnect handling
- upstream stream error normalization

Streaming logic should not be duplicated in every adapter. Provider adapters parse upstream streams into canonical events; client adapters encode canonical events into client protocol frames.

### 3. Model Registry and Resolver

Responsible for resolving requested model names.

It supports both:

- raw model passthrough
- virtual model aliases

Resolution order:

1. exact virtual alias match
2. explicit route match
3. raw model match against provider model lists when `allow_raw_models` is enabled
4. fallback to configured default model when allowed by the client protocol/profile
5. standardized `model_not_found` error

The registry also resolves model capabilities independently from routing. Image input
uses a three-state value (`supported`, `unsupported`, `unknown`) with this precedence:

1. model-level local override
2. provider default
3. unambiguous models.dev metadata
4. unknown

Adapter `Vision` capability means that the protocol adapter can encode image blocks; it
does not claim that every model behind that adapter accepts images.

### 4. Channel Manager

Responsible for selecting and protecting upstream channels.

v0.1:

- simple priority order
- optional fallback list
- per-channel timeout
- per-channel concurrency limit

Future:

- weighted round-robin
- least latency
- circuit breaker
- active/passive health checks
- cooldown after repeated 429/5xx

### 5. Upstream Auth Profiles

Responsible for applying provider-side authentication.

This must not assume all providers use API keys.

v0.1 supports:

- none
- bearer token
- API key header
- custom headers
- custom query params

Future profiles:

- basic auth
- mTLS
- HMAC signatures
- OAuth2 client credentials
- cloud-provider signing schemes

### 6. Secret Manager

Responsible for storing and resolving secrets.

v0.1 secret references:

- `env:NAME`
- `literal:VALUE` for local-only development
- `encrypted:...` using a local master key

Future secret references:

- file secrets
- OS keychain
- Vault-compatible external stores

Provider credentials may be written through the UI, but routine snapshots and
list APIs never return them in plaintext. Local data-plane Client Keys are an
explicit exception: keys created by the control plane retain a recoverable copy
in the private local config and return it only from the authenticated per-key
reveal endpoint. Authentication continues to use the stored bcrypt hash.

### 7. Pipeline Hooks

A lightweight internal middleware system for cross-cutting behavior.

Initial lifecycle points:

```text
OnRequestReceived
OnClientAuthenticated
OnCanonicalRequestParsed
OnModelResolved
OnBeforeUpstream
OnCanonicalStreamEvent
OnResponseComplete
OnError
```

v0.1 can keep hooks internal. A public plugin SDK can come later.

Resolved requests also pass through a small, ordered `RequestPreprocessor` pipeline
before provider concurrency is acquired and before the upstream request is encoded.
Preprocessors receive Canonical IR, the resolved target, provider configuration, and
adapter transport capabilities. The first implementation is the disabled-by-default
multimodal fallback skeleton. This keeps OCR, future document extraction, and similar
transformations out of protocol adapters and out of the main runtime handler.

When OCR fallback is enabled, the multimodal preprocessor performs the following before
LLM provider concurrency is acquired:

1. resolve the selected model's image capability;
2. decode and validate embedded images under fixed limits;
3. call the embedded OCR provider by default, or the external HTTP provider
   when the user explicitly configures one, with a dedicated timeout and auth
   profile;
4. reuse successful in-memory results by image hash with per-key singleflight;
5. replace image blocks in-place with escaped, explicitly untrusted OCR text;
6. return a protocol-native error before the LLM call when OCR is unsafe or unusable.

When a separately configured Vision fallback exists, unusable OCR switches the effective
target without re-running the preprocessor. The fallback must be different from the
original target, explicitly support image input, and use an adapter that can transport
images. The original image-bearing Canonical IR is retained for that call.

The original Canonical IR is not mutated. Remote image fetching is not part of the first
OCR release.

The built-in provider embeds a compact Simplified Chinese and English Tesseract
model and executes it through WASM in a short-lived isolated invocation of the
same vibe-proxy executable. The worker is serialized, receives only the image
payload, does not inherit provider credentials, and exits after recognition so
the WASM memory is returned to the operating system. It does not require a
second container, Python, ONNX Runtime, a system OCR package, or runtime model
downloads. The HTTP provider remains an explicit extension point for higher
accuracy, additional languages, private OCR services, and accelerated
deployments.

Telemetry stores a structured transformation summary with routing and aggregate OCR
metadata. It intentionally excludes images, image locations, OCR text, and secrets. Local
SQLite migration adds a JSON summary column while Recent Requests exposes the same typed
object to the control plane.

### 8. Telemetry Pipeline

Responsible for local request tracing without requiring an external service.

A trace envelope is created after protocol detection and before authentication,
body parsing, model authorization, or routing. Consequently, rejected requests
still finish with a bounded metadata summary. One inbound HTTP call has one
`request_id`, W3C-compatible `trace_id`/`span_id` fields, and optional explicit
Session, project, parent-request, and Agent identity.

The authenticated principal is an authorization fact. Agent identity is
diagnostic, untrusted metadata and never changes permissions or routing. Identity
classification uses this precedence:

1. explicit bounded `X-Vibe-*` headers;
2. allowlisted W3C baggage;
3. protocol metadata;
4. configured Agent profiles;
5. built-in User-Agent signatures.

No Session is fabricated from an API key, principal, User-Agent, or prompt
similarity. Session views are derived from explicitly identified request rows and
may contain multiple Agents or concurrent requests. Chronological order is useful
for replay but is not automatically treated as causality.

SQLite stores three independently useful layers:

- request summaries with identity, initial/effective route, request shape,
  status, finish reason, upstream request ID, duration, TTFT/TPOT/TPS, and token
  usage;
- ordered gateway observations for parsing, preprocessing, OCR/Vision fallback,
  upstream calls, and streaming stages;
- separately retained payload snapshots for client, Canonical IR, upstream, and
  canonical-response stages.

Payload capture defaults to `metadata`. `structured` and `raw` are explicit
opt-ins and both pass through decoded-JSON sanitization. Authorization, cookies,
credential-like keys, reasoning unless separately enabled, image/file bytes, and
data URLs cannot bypass mandatory omission or redaction. Streaming stores the
accumulated Canonical IR response rather than raw SSE frames. Every stage reports
an explicit state: `not_captured`, `captured`, `redacted`, `truncated`, `expired`,
`dropped`, or `missing`.

Payloads share a per-request byte budget and have a shorter retention period and
independent disk quota. Payload-first quota eviction and exact request-content
deletion preserve request summaries and observations. Request diffs are computed
on demand from available canonical request snapshots; they describe structural
message/tool/route changes and do not claim semantic equivalence.

The gateway writes durable telemetry through a bounded asynchronous recorder.
Summaries have priority over observations, which have priority over payloads.
Queue pressure evicts payloads first. Recorder and SQLite failures are contained
and cannot change an otherwise valid model response; shutdown attempts a bounded
flush before closing SQLite.

Current sinks and query surfaces:

- embedded SQLite as the Local Mode source of truth;
- an in-memory recent-request view;
- an authenticated, bounded in-memory SSE lifecycle stream for live UI updates;
- low-cardinality Prometheus metrics;
- authenticated Requests, Sessions, detail, timeline, content deletion, and diff
  Admin APIs and UI.

High-cardinality request, trace, Session, project, and Agent identifiers are not
Prometheus labels. Future OpenTelemetry, Langfuse, JSONL, or analytics export must
remain optional, disabled by default, and require an independent content-export
opt-in rather than weakening local capture policy.

Prometheus and Grafana are optional integrations rather than bundled runtime
components. The metrics contract, same-host scrape example, and versioned
dashboard are documented in [`prometheus-grafana.md`](prometheus-grafana.md).

### 9. Control Plane

Responsible for local UI, config management, validation, and hot reload.

v0.1:

- same process as data plane
- separate HTTP listener restricted to a loopback IP
- explicit route allowlists keep UI, Admin API, auth, desktop bootstrap, and
  metrics off the Agent-facing data listener
- local dashboard
- config reload
- provider connectivity test
- recent request viewer
- searchable Requests and Sessions views
- request summary, timeline, sanitized payload, metadata, and diff inspection

Future:

- separate control plane process
- multi-instance config distribution
- richer admin API

### 10. Conformance Suite

A test suite for adapter contributors.

It should include fixtures for:

- OpenAI Chat unary
- OpenAI Chat streaming
- OpenAI Chat tool calls
- OpenAI Responses input/output items
- Anthropic Messages streaming
- tool use/tool result mapping
- trailing usage frames
- upstream error normalization
- multimodal content

This is important for open-source contribution quality.

## Non-Goals for v0.1

v0.1 intentionally does not include:

- multi-user registration
- billing system
- complex RBAC
- Postgres requirement
- cluster deployment
- distributed control plane
- plugin marketplace
- enterprise-grade quota system
- full provider catalog

These may be added later without changing the core architecture.
