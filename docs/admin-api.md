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

## Discover Provider Models

```text
POST /admin/providers/models
```

Accepts the provider form payload, including unsaved URL and authentication values, and
queries the provider's model-list endpoint. The response contains the raw model IDs plus
best-effort capability metadata from the local models.dev catalog:

```json
{
  "models": ["gpt-4.1"],
  "model_details": {
    "gpt-4.1": {
      "capabilities": {
        "image_input": "supported",
        "tool_call": "supported"
      },
      "match": {
        "status": "exact_provider",
        "source": "models.dev"
      }
    }
  }
}
```

Unknown or ambiguous catalog matches remain explicit instead of guessing a capability.
Provider credentials are used only for the probe and are never returned.

## Model Capability Catalog

```text
GET  /admin/model-catalog/status
POST /admin/model-catalog/refresh
POST /admin/model-catalog/lookup
```

The catalog is downloaded from models.dev into a local cache. Data-plane requests never
depend on a live models.dev request. `refresh` honors HTTP validators and keeps the last
usable cache when the remote source is temporarily unavailable.

Lookup accepts a provider/model pair:

```json
{
  "provider_id": "openai",
  "catalog_provider": "openai",
  "base_url": "https://api.openai.com/v1",
  "model": "gpt-4.1"
}
```

`catalog_provider` is optional and overrides provider inference when a custom MaaS
endpoint exposes model IDs from a known catalog provider.

## Recent Requests

```text
GET /admin/requests/recent
```

Returns active and recent request telemetry for local debugging.

Image handling is reported as a structured `transformation` summary. It contains the
multimodal route, original/effective targets, capability source, image count, OCR provider,
latency, confidence and cache hits. It never contains image bytes, image URLs, OCR text, or
OCR credentials.

Successful OCR responses also include:

```text
X-Vibe-Proxy-Image-Fallback: ocr
X-Vibe-Proxy-OCR-Images: 2
Warning: 299 vibe-proxy "Image input was degraded to OCR text"
```

Vision fallback responses use `X-Vibe-Proxy-Image-Fallback: vision`.
