# vibe-proxy

`vibe-proxy` is a **local-first, agent-first, gateway-ready multi-protocol LLM proxy**.

It lets different AI agents, IDEs, CLIs, and local tools connect to one stable local endpoint while `vibe-proxy` handles protocol translation, provider authentication, model mapping, hot switching, and request observability.

The default goal is not to be a large hosted gateway. The default goal is:

> Configure providers once, point every local agent at vibe-proxy, then switch and debug models in one place.

## Current Status

This repository currently contains an initial Go backend skeleton and architecture documents.

Implemented in the first backend pass:

- Go `net/http` data plane
- OpenAI-compatible `/v1/chat/completions` entrypoint
- OpenAI Chat request transformation to Anthropic Messages API
- Anthropic event-stream to OpenAI SSE conversion
- TTFT / TPOT / TPS tracking
- SQLite request log persistence
- Prometheus `/metrics`
- hashed client key authentication
- provider key isolation through env vars or encrypted config values
- atomic hot reload snapshot pattern
- minimal dark local dashboard landing page

The next development step is to refactor the backend around the documented architecture:

- Canonical IR
- Client Adapters
- Provider Adapters
- Upstream Auth Profiles
- Model Resolver
- Stream Engine

## Architecture Documents

Start here:

- [`docs/architecture.md`](docs/architecture.md) — overall architecture and module boundaries
- [`docs/canonical-ir.md`](docs/canonical-ir.md) — internal protocol-neutral representation
- [`docs/adapter-sdk.md`](docs/adapter-sdk.md) — adapter extension model
- [`docs/config-schema.md`](docs/config-schema.md) — progressive configuration design
- [`docs/streaming-engine.md`](docs/streaming-engine.md) — streaming conversion model
- [`docs/mvp-roadmap.md`](docs/mvp-roadmap.md) — v0.1 scope and phases
- [`docs/admin-api.md`](docs/admin-api.md) — local admin API
- [`docs/development.md`](docs/development.md) — local development and test commands
- [`docs/contributing-architecture.md`](docs/contributing-architecture.md) — how contributors should extend the system

## Intended v0.1 Scope

Client protocols:

- OpenAI Chat Completions
- OpenAI Responses API
- Anthropic Messages API

Provider protocols:

- OpenAI-compatible
- Anthropic Messages

Core local features:

- raw model passthrough
- virtual model aliases
- custom provider auth headers/query params
- env and encrypted local secrets
- hot reload
- recent request log
- local dashboard

## Example Future Simple Config

```yaml
version: vibeproxy.io/v1alpha1

server:
  listen: 127.0.0.1:8080

providers:
  anthropic:
    type: anthropic
    api_key: env:ANTHROPIC_API_KEY

  deepseek:
    type: openai-compatible
    base_url: https://api.deepseek.com/v1
    api_key: env:DEEPSEEK_API_KEY

models:
  default: vibe-coder
  allow_raw: true
  aliases:
    vibe-coder: anthropic/claude-3-5-sonnet-20241022
    vibe-fast: deepseek/deepseek-chat
```

## Run Current Skeleton

Install Go 1.25+, then:

```bash
export VIBE_PROXY_ADMIN_TOKEN='change-me-admin-token'
export ANTHROPIC_API_KEY='sk-ant-your-key'
go mod tidy
go run ./cmd/vibe-proxy -config configs/bootstrap.yaml
```

The sample local client key in `configs/config.yaml` is documented as:

```text
vibe-local-dev-key
```

Example request:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer vibe-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{
    "model":"gpt-4o",
    "stream":true,
    "messages":[{"role":"user","content":"hello"}]
  }'
```

## Product Principle

> Powerful core, simple surface.

Internally, `vibe-proxy` should be designed like a small extensible gateway. Externally, the default local user experience should remain simple, fast, and hard to misconfigure.


## Bootstrap Mode

You can start the local control plane before configuring providers:

```bash
export VIBE_PROXY_ADMIN_TOKEN='change-me-admin-token'
go run ./cmd/vibe-proxy -config configs/bootstrap.yaml
```

Then open:

```text
http://127.0.0.1:8080/
```

Provider editing in the UI is still evolving. For now, use the admin APIs and config files; the server can run without providers so the control plane remains reachable.


## No Local Go?

Use Docker:

```bash
VIBE_PROXY_ADMIN_TOKEN='admin-token' ./scripts/dev-docker.sh configs/bootstrap.yaml
```

Open `http://127.0.0.1:8080/`.
