# Configuration Schema

`vibe-proxy` configuration should be simple by default and progressively expose advanced features.

## Versioning

Every config file should include a schema version:

```yaml
version: vibeproxy.io/v1alpha1
```

Future CLI/UI commands should support:

```text
vibe-proxy config validate
vibe-proxy config migrate
vibe-proxy config diff
vibe-proxy config apply
```

## Simple Local Config

This is the default user-facing shape.

```yaml
version: vibeproxy.io/v1alpha1

server:
  listen: 127.0.0.1:8080
  admin_listen: 127.0.0.1:8081

# Optional. Leave empty to use HTTP_PROXY / HTTPS_PROXY from the process.
model_catalog:
  proxy_url: http://user:password@proxy.example.com:8080

providers:
  anthropic:
    type: anthropic
    api_key: env:ANTHROPIC_API_KEY

  deepseek:
    type: openai-compatible
    base_url: https://api.deepseek.com/v1
    api_key: env:DEEPSEEK_API_KEY

  company-maas:
    type: openai-compatible
    base_url: https://maas.example.com/v1
    api_key: env:COMPANY_MAAS_API_KEY
    catalog_provider: deepseek

models:
  default: vibe-coder
  allow_raw: true
  aliases:
    vibe-coder: anthropic/claude-3-5-sonnet-20241022
    vibe-fast: deepseek/deepseek-chat
```

This should be enough for most local users.

`server.listen` is the Agent-facing data plane and may use a LAN or wildcard
address when other machines need access. `server.admin_listen` serves the UI,
Admin API, authentication, desktop bootstrap, and metrics. It defaults to
`127.0.0.1:8081` and must remain a literal loopback address. The two non-zero
ports must differ, and changing either listener requires an application restart.

## Model Capability Catalog

Provider model IDs discovered through `/v1/models` do not normally include whether the
model accepts images, supports tools, or performs reasoning. `vibe-proxy` enriches those
IDs from a cached models.dev catalog and preserves `unknown` when a match is ambiguous.

For first-party endpoints, the catalog provider is inferred from the provider ID, model
prefix, or base URL. A custom MaaS endpoint can declare the upstream model family:

```yaml
providers:
  internal-maas:
    type: openai-compatible
    base_url: https://maas.internal.example/v1
    catalog_provider: anthropic
    api_key: env:INTERNAL_MAAS_TOKEN
```

`catalog_provider` is metadata only. It does not change request routing, protocol
selection, or authentication. Effective image capability uses the fixed precedence
`model_override > provider_default > models_dev > unknown`. An explicit local value,
including `unknown`, is authoritative; models.dev fills only an otherwise absent value.
Not-found and ambiguous catalog matches remain `unknown`. Catalog data comes from the
local snapshot and is never fetched over the network in the data-plane hot path or
copied into provider configuration.

The rationale for this precedence is recorded in
[ADR-0002 in PR #13](https://github.com/a448582655/vibe-proxy/pull/13/files#diff-24c00a4894285b400b47b8d2bd412d20362e55a5278ead64044e6db68dd5d6a3).

Desktop applications do not necessarily inherit proxy environment variables
from a terminal. In restricted networks, configure the models.dev proxy in
**Settings → Model capability catalog**, or set it in YAML:

```yaml
model_catalog:
  proxy_url: http://user:password@proxy.example.com:8080
```

Only `http://` and `https://` proxy URLs are accepted. Leaving `proxy_url`
empty restores the standard `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY`
environment behavior. Proxy credentials are local secrets and are not echoed
by the model-catalog status or settings endpoints.

Provider-wide defaults and per-model corrections use an explicit three-state value:

```yaml
providers:
  internal-maas:
    default_capabilities:
      image_input: unsupported
    model_capabilities:
      qwen-vl:
        image_input: supported
      renamed-private-model:
        image_input: unknown
```

Valid values are `supported`, `unsupported`, and `unknown`. Missing metadata also means
unknown, but an explicit local value records the user's intended override. Editing a
provider through the basic control plane preserves advanced capability overrides already
present in YAML.

## Provider Auth

Provider auth must support non-key and custom-header deployments.

Short forms:

```yaml
api_key: env:OPENAI_API_KEY
```

Equivalent expanded form:

```yaml
auth:
  type: bearer
  token: env:OPENAI_API_KEY
```

API key header:

```yaml
auth:
  type: api_key_header
  header: x-api-key
  value: env:ANTHROPIC_API_KEY
```

Custom headers:

```yaml
auth:
  type: custom_headers
  headers:
    X-Tenant-ID: literal:team-a
    X-Api-Token: env:PRIVATE_MODEL_TOKEN
```

Custom query params:

```yaml
auth:
  type: custom_query
  query:
    api-version: literal:2026-01-01
    token: env:PRIVATE_QUERY_TOKEN
```

No auth:

```yaml
auth:
  type: none
```

## Raw Models and Aliases

`vibe-proxy` must support both raw provider model names and virtual aliases.

```yaml
models:
  allow_raw: true
  aliases:
    vibe-coder: anthropic/claude-3-5-sonnet-20241022
    vibe-fast: deepseek/deepseek-chat
```

If a client requests `vibe-coder`, the alias is used.

If a client requests `deepseek-chat`, the resolver may route it directly to the `deepseek` provider when `allow_raw` is enabled and the provider declares that model.

## Advanced Routes

Optional advanced routing:

```yaml
routes:
  - match:
      model: vibe-coder
    targets:
      - provider: anthropic
        model: claude-3-5-sonnet-20241022
      - provider: deepseek
        model: deepseek-chat
        fallback: true
```

## OCR Image Fallback

OCR fallback is opt-in. When enabled, a model resolved as `unsupported` for image input
can receive extracted text instead of the original image:

```yaml
multimodal:
  enabled: true
  strategy: ocr_then_vision
  ocr:
    provider: builtin
    timeout: 15s
    min_confidence: 0.55
    min_text_chars: 4
    max_images: 4
    max_image_bytes: 5242880
    max_total_image_bytes: 12582912
    max_text_chars_per_image: 8000
    max_text_chars_total: 16000
    remote_images: false
    cache:
      enabled: true
      max_entries: 256
      ttl: 24h
  vision_fallback_model: ""
  vision_fallback_strategy: assist  # assist (default) | takeover | reject
  vision_assist:
    max_prompt_chars: 4000
    max_output_tokens: 1024
    cache:
      enabled: true
      max_entries: 256
      ttl: 24h
```

`builtin` is the default when `ocr.provider` is omitted and no endpoint is
present. It embeds a compact Simplified Chinese and English Tesseract model and
runs it through WASM in a short-lived isolated copy of the vibe-proxy
executable. This returns the WASM memory to the operating system after a cache
miss without starting another service, calling the network, downloading a
model, or requiring a system Tesseract installation.

An explicitly configured external service overrides the built-in provider:

```yaml
multimodal:
  enabled: true
  ocr:
    provider: http
    endpoint: http://127.0.0.1:32180/v1/ocr
    auth:
      type: none
```

The external HTTP OCR endpoint receives:

```json
{
  "images": [
    {"index": 0, "media_type": "image/png", "data_base64": "..."}
  ]
}
```

and returns:

```json
{
  "results": [
    {"index": 0, "text": "recognized text", "confidence": 0.93, "language": "en"}
  ]
}
```

The first release accepts embedded base64 and base64 data URLs. It deliberately does not
download remote image URLs in the OCR path. OCR text replaces each image at its original
position, is escaped and marked as untrusted user data, and is never included in ordinary
logs. Provider authentication uses the same auth profile and secret-reference forms as
LLM providers.

If `vision_fallback_model` is configured, OCR errors, empty text, or confidence below the
threshold apply `vision_fallback_strategy`. `assist` is the default: vibe-proxy sends only
the image and bounded text from the latest image-bearing user message to the Vision model,
replaces those images with untrusted visual evidence, and lets the original model answer
with the full conversation. Images in older messages are not sent to the helper; they become
explicit `historical_image_not_analyzed` text markers so a text-only primary model never
receives raw images or evidence attributed to the wrong turn. This avoids moving a long
context to a smaller Vision model and keeps the historical prefix stable for upstream
prompt-cache reuse.

`takeover` preserves the original direct fallback: the untouched image request is routed to
the Vision target and that model answers. When models.dev provides an input/context limit,
clearly oversized takeover requests are rejected before the upstream call. `reject` does not
invoke Vision after unusable OCR. The fallback target must resolve to a different
provider/model, its effective capability must be `image_input: supported`, and its provider
adapter must be able to transport images.
Effective support may come from a model override, a provider default, or an unambiguous
models.dev match, in that order. Explicit `unknown` and `unsupported`, as well as absent,
ambiguous, or not-found catalog data, are rejected.

Static file validation cannot require a loaded catalog. When no explicit capability is
present it reports the warning `vision_fallback_unverified`; the runtime validates the
resolved target against the active catalog before saving or using it. An explicit
`unknown` or `unsupported` remains the error `vision_fallback_invalid`. The client key is
still authorized against the originally requested public model.

## Local Observability and Content Capture

Request summaries, trace observations, and Session identity are recorded locally in
SQLite. Content capture is configured separately and defaults to metadata only:

```yaml
observability:
  capture:
    mode: metadata
    max_snapshot_bytes: 262144
    capture_response: false
    capture_reasoning: false
    image_payloads: metadata
    header_allowlist: [user-agent, traceparent]
  retention:
    summaries_days: 14
    content_days: 3
    max_content_storage_mb: 512
```

`mode` accepts:

- `metadata`: store request identity, routing, shape, status, timing, usage, and
  observations without request or response bodies. This is the default.
- `structured`: store bounded sanitized JSON snapshots for supported stages.
- `raw`: preserve the supported wire JSON shape after the same mandatory sanitizer.
  This is the highest-risk mode and produces a validation warning.

`raw` does not mean unredacted. All detailed modes decode JSON before sanitizing it.
Authorization and cookie headers, credential-like JSON keys, reasoning content unless
`capture_reasoning` is enabled, image/file bytes, base64 payloads, and data URLs are
always omitted or replaced. Only explicitly allowlisted non-sensitive headers may be
stored. `image_payloads` currently must remain `metadata`.

`max_snapshot_bytes` is also the shared per-request content budget across client,
Canonical IR, upstream, and response snapshots. The valid range is 1 KiB through 4 MiB.
When the budget is exhausted, later payloads report `dropped`; oversized JSON reports
`truncated` rather than silently appearing complete.

`capture_response` opts into canonical response capture. `capture_reasoning` is a
separate high-sensitivity opt-in and should remain false unless the local operator has a
specific debugging need. A request may send:

```text
X-Vibe-Capture: metadata
X-Vibe-Capture: off
```

to reduce the configured capture level for that request. It can never elevate
`metadata` to `structured` or `raw`.

Summary retention remains longer than or equal to content retention. Content is pruned
independently by age and `max_content_storage_mb`; payloads are evicted before summaries.
Deleting one request's content leaves its summary and observations available with an
explicit `expired` state. The embedded SQLite database is not encrypted by this feature,
so detailed capture should be enabled only on a trusted local machine with appropriate
filesystem permissions.

Agent and Session identities are request metadata, not secrets or authorization claims.
They are supplied by bounded `X-Vibe-Agent-*`, `X-Vibe-Session-*`,
`X-Vibe-Project-ID`, and `X-Vibe-Parent-Request-ID` values, allowlisted baggage, or
supported protocol metadata. No config option groups requests by API key or User-Agent.

## Agent Profiles

Agent profiles are optional. They help with local multi-agent workflows.

```yaml
agent_profiles:
  claude-code:
    detect:
      user_agent: "*claude-code*"
    default_model: vibe-coder

  cursor:
    detect:
      user_agent: "*cursor*"
    default_model: vibe-fast
```

## Expert Gateway Fields

These are not part of the default UI in v0.1 but should be schema-compatible later.

```yaml
channels:
  - id: anthropic-primary
    provider: anthropic
    weight: 100
    max_concurrency: 64
    timeout: 120s
    health_check:
      enabled: true
      interval: 30s
    circuit_breaker:
      error_rate: 0.5
      window: 60s
```

## UX Rule

The UI must not expose the full schema at once.

Default UI should show:

1. local endpoint
2. provider connection status
3. model aliases
4. recent requests
5. reload/test buttons

Advanced configuration should be hidden behind explicit advanced sections.
