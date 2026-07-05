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
- listens on `127.0.0.1` by default
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
  -> Client Adapter
  -> Canonical IR
  -> Pipeline Hooks
  -> Model Resolver
  -> Channel Selector
  -> Upstream Auth Profile
  -> Provider Adapter
  -> Stream Engine
  -> Client Adapter Encoder
  -> Telemetry Sink
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

Secrets may be written through UI, but must not be returned in plaintext by API responses.

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

### 8. Telemetry Pipeline

Responsible for request observability.

v0.1 captures:

- request id
- client protocol
- detected agent
- requested model
- resolved provider/model
- stream/unary mode
- status/error code
- duration
- TTFT
- TPOT
- TPS
- token usage
- cache read/write tokens when available

v0.1 sinks:

- console logs
- SQLite
- optional Prometheus endpoint

Future sinks:

- OpenTelemetry
- Langfuse
- JSONL
- external analytics systems

### 9. Control Plane

Responsible for local UI, config management, validation, and hot reload.

v0.1:

- same process as data plane
- local dashboard
- config reload
- provider connectivity test
- recent request viewer

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
