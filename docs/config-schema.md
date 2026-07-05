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

This should be enough for most local users.

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
