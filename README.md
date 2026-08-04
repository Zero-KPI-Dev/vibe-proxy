# vibe-proxy

`vibe-proxy` is a **local-first, agent-first, gateway-ready multi-protocol LLM proxy**.

It lets different AI agents, IDEs, CLIs, and local tools connect to one stable local endpoint while `vibe-proxy` handles protocol translation, provider authentication, model mapping, hot switching, and request observability.

The default goal is not to be a large hosted gateway. The default goal is:

> Configure providers once, point every local agent at vibe-proxy, then switch and debug models in one place.

## Current Status

The repository contains a working local gateway, embedded browser control plane,
and Wails desktop host. Current capabilities include:

- OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages client APIs
- OpenAI-compatible and Anthropic provider adapters over a Canonical IR
- unary and streaming protocol conversion
- raw provider models, virtual aliases, and a default model
- provider CRUD, literal/environment credentials, and custom auth headers
- local models.dev capability catalog with proxy, cache, and offline import support
- OCR-to-text and Vision fallback for image requests sent to non-Vision models
- hashed data-plane client keys and a separate desktop management password
- atomic configuration reloads
- local SQLite request summaries, traces, Sessions, optional sanitized payload
  capture, replay, and structural request diffs
- TTFT, TPOT, TPS, token usage, recent-request telemetry, and Prometheus metrics
- installer-oriented Windows and macOS desktop builds plus a Linux CLI package

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

## v0.1 Scope

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
- recent request log and searchable request traces
- explicit Agent/Session identity and Session replay
- bounded opt-in request/response capture and structural diffs
- local browser and native desktop control planes

## Example Simple Config

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

## Local Observability

The gateway records metadata-only request summaries and trace observations by
default. Open **Observability > Requests** or **Observability > Sessions** in the
control plane to search requests, inspect an OCR/fallback/upstream timeline, and
replay requests that share an explicit Session ID.

Detailed prompt and response capture is optional:

```yaml
observability:
  capture:
    mode: structured
    capture_response: true
  retention:
    summaries_days: 14
    content_days: 3
    max_content_storage_mb: 512
```

Structured and raw capture are bounded and sanitized, but the resulting content
is still stored in the local **unencrypted SQLite database**. Enable it only on a
trusted machine. The UI can delete the captured content for one exact request
while preserving its summary and timeline.

Clients may group related calls explicitly and report their diagnostic Agent
identity:

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer vibe-local-dev-key' \
  -H 'Content-Type: application/json' \
  -H 'X-Vibe-Session-ID: task-42' \
  -H 'X-Vibe-Agent-ID: codex' \
  -d '{"model":"vibe-coder","messages":[{"role":"user","content":"hello"}]}'
```

Requests without `X-Vibe-Session-ID` remain unclassified; vibe-proxy does not
guess a Session from an API key or User-Agent. Request diffs compare available
canonical structures and do not infer semantic equivalence. External OTLP or
hosted observability export is not required and, if added later, will remain an
explicit opt-in.

## Download a Release

Windows and macOS users can choose the **Vibe Proxy Desktop** application. It
starts the gateway, opens the embedded control plane in a native window, remains
available from the system tray/menu bar, and stores its state in the normal
per-user application-data directory.

Linux uses the CLI installed from a DEB plus the browser-based control plane at
`http://127.0.0.1:8080/`; a native Linux GUI is not required.

Download the matching Windows setup EXE, macOS desktop DMG, or Linux DEB from
[GitHub Releases](https://github.com/a448582655/vibe-proxy/releases).
Desktop builds are initially published as unsigned release candidates for
cross-platform acceptance rather than as stable releases.

See [`docs/release.md`](docs/release.md) for architecture selection, checksum
verification, platform-specific startup commands, and the release-candidate
test checklist.

## Run from Source

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

The server can start without providers so the control plane remains reachable.
Use **Providers** in the UI to add a backend, fetch and select its models, and
configure credentials or capability overrides.


## No Local Go?

Use Docker:

```bash
VIBE_PROXY_ADMIN_TOKEN='admin-token' ./scripts/dev-docker.sh configs/bootstrap.yaml
```

Open `http://127.0.0.1:8080/`.
