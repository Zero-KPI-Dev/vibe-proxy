# Admin API

Admin APIs are protected by a separate bearer token configured through `VIBE_PROXY_ADMIN_TOKEN` or the configured `security.admin_bearer_token_env`.

## Desktop Authentication and Controls

The desktop process creates a short-lived, single-use bootstrap nonce and
navigates its WebView to:

```text
GET /desktop/bootstrap/{nonce}
```

The response sets a process-local, same-origin `vibe_desktop_session` cookie and
redirects to `/`. The nonce cannot be reused and neither it nor the generated
admin token is written to disk. Existing browser and CLI Bearer authentication
continues to work.

The following routes are available only when the native desktop controller is
attached:

```text
GET  /admin/desktop
PUT  /admin/desktop/preferences
POST /admin/desktop/open-data-dir
POST /admin/desktop/import-config
```

They expose the platform, close behavior, listen address and safe native
actions. In CLI mode the snapshot reports desktop controls as unavailable and
mutating routes return a conflict response.

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
POST /admin/model-catalog/import
POST /admin/model-catalog/lookup
```

The catalog is downloaded from models.dev into a local cache. Data-plane requests never
depend on a live models.dev request. `refresh` honors HTTP validators and keeps the last
usable cache when the remote source is temporarily unavailable.

For restricted or fully offline networks, download `https://models.dev/api.json` on
another machine and import it from Settings, or upload it directly:

```bash
curl -H "Authorization: Bearer $VIBE_PROXY_ADMIN_TOKEN" \
  -F "catalog=@api.json;type=application/json" \
  http://127.0.0.1:8080/admin/model-catalog/import
```

The file is limited to 10 MB and is parsed before it replaces the active catalog. An
invalid upload leaves the previous usable catalog unchanged. A later online refresh still
uses the normal models.dev URL.

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

`GET /admin/multimodal` reports `provider: builtin|http`. Built-in OCR is the
default; an external endpoint and its authentication fields are only used when
the provider is `http`. `POST /admin/multimodal/ocr/test` runs the selected
provider against an embedded deterministic Chinese and English fixture. Its
response separately reports whether the submitted form enabled fallback and
whether the current runtime is active; a successful engine test can therefore
return `warning: multimodal_disabled|multimodal_not_active`. This avoids treating
an isolated OCR engine test as proof that image requests currently use OCR. The
response also returns provider, engine (for built-in), latency, result count, and
aggregate confidence without returning recognized text.

Successful OCR responses also include:

```text
X-Vibe-Proxy-Image-Fallback: ocr
X-Vibe-Proxy-OCR-Images: 2
Warning: 299 vibe-proxy "Image input was degraded to OCR text"
```

Vision fallback responses use `X-Vibe-Proxy-Image-Fallback: vision`.
