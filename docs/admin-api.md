# Admin API

Admin APIs are protected by a separate bearer token configured through `VIBE_PROXY_ADMIN_TOKEN` or the configured `security.admin_bearer_token_env`.

## Validate Config

```text
GET /admin/config/validate
```

Returns:

```json
{
  "valid": true,
  "issues": []
}
```

## Runtime Snapshot

```text
GET /admin/config/snapshot
```

Returns sanitized provider and model resolver state. Secrets are not returned.

## Hot Reload

```text
POST /admin/config/reload
```

Reloads the config file and atomically swaps the runtime snapshot.

## Provider Connectivity Test

```text
POST /admin/providers/test?id=anthropic
```

Performs a lightweight connectivity check using the provider's configured auth profile.

## Recent Requests

```text
GET /admin/requests/recent
```

Returns active and recent request telemetry for local debugging.
