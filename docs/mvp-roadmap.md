# MVP Roadmap

## v0.1 Goal

Build a local-first multi-protocol LLM protocol switcher for agent tools.

The user should be able to:

1. run `vibe-proxy` locally
2. configure Anthropic and OpenAI-compatible providers
3. point multiple agents at the same local endpoint
4. use OpenAI Chat, OpenAI Responses, and Anthropic Messages clients
5. use both virtual model aliases and raw provider model names
6. see recent requests, errors, latency, and token usage
7. hot reload provider/model config without changing agent configs

## v0.1 Must Have

### Protocols

Client protocols:

- OpenAI Chat Completions
- OpenAI Responses API
- Anthropic Messages API

Provider protocols:

- OpenAI-compatible
- Anthropic Messages

### Model Resolution

- alias model support
- raw model passthrough
- default model
- fallback target list

### Provider Auth

- none
- bearer token
- API key header
- custom headers
- custom query params
- env secret refs
- local encrypted secret refs

### Runtime

- single process
- local config file
- hot reload through API/UI
- immutable runtime snapshot for data-plane reads

### Observability

- recent request log
- status/error code
- requested model
- resolved provider/model
- protocol in/out
- TTFT for streaming
- duration
- token usage when available
- SQLite persistence

### UI

Minimal local dashboard:

- endpoint copy panel
- provider status
- aliases/routes table
- recent requests
- config reload
- provider test

## v0.1 Should Not Have

- billing
- multi-user registration
- complex RBAC
- hosted SaaS assumptions
- required Postgres
- distributed deployment
- plugin marketplace
- large provider catalog

## Development Phases

### Phase 1: Architecture Refactor

- add `internal/ir`
- add client adapter interfaces
- add provider adapter interfaces
- add upstream auth profile package
- add model resolver package
- update config loader to support simple schema

### Phase 2: Minimal Data Plane

- OpenAI Chat client adapter
- Anthropic provider adapter
- OpenAI-compatible provider adapter
- raw model + alias resolution
- env-based provider secrets
- streaming event pipeline

### Phase 3: Multi-Protocol Client Support

- Anthropic Messages client adapter
- OpenAI Responses client adapter
- tool call/tool result mapping
- stream-to-unary and unary-to-stream behavior

### Phase 4: Local Dashboard

- provider list
- alias editor
- recent requests
- reload/test actions
- secret write-only form

### Phase 5: Conformance Tests

- fixtures and golden tests
- adapter contribution guide
- CI checks

## Success Criteria

A developer can configure this once:

```text
OpenAI base URL: http://127.0.0.1:8080/v1
Anthropic base URL: http://127.0.0.1:8080/anthropic
API key: local vibe-proxy key
```

Then switch provider/model behavior inside `vibe-proxy` without editing each agent's config.
