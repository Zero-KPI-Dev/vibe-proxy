# Admin API

In CLI/server deployments, Admin APIs are protected by a separate bearer token
configured through `VIBE_PROXY_ADMIN_TOKEN` or the configured
`security.admin_bearer_token_env`. The native desktop application instead uses
a management password and same-origin browser sessions; it does not ask the
user to discover or copy its internal admin token.

## Desktop Authentication and Controls

On the first desktop launch, the native window asks the user to create a
management password. Only an Argon2id password hash is stored in `auth.json`
under the application data directory. Provider credentials and data-plane
client keys remain separate and are managed from their existing control-plane
pages.

The desktop process also creates a short-lived, single-use bootstrap nonce and
navigates its native WebView to:

```text
GET /desktop/bootstrap/{nonce}
```

The response sets a process-local, same-origin, HttpOnly
`vibe_desktop_session` cookie and redirects to `/`. The nonce cannot be reused
and neither it nor the generated internal admin token is written to disk. This
lets the native window authenticate automatically on each launch without
placing a credential in a URL, browser storage, config file, or log.

After first-run setup, a regular browser—including **Open in Browser** from the
tray/menu bar—shows the management-password login screen and receives its own
HttpOnly session after a successful login. If the native WebView fails before
the first-run password has been created, **Open in Browser** may use one
single-use bootstrap session so setup is still recoverable. Opening the bare
listen address can never claim an uninitialized control plane.

The password/session endpoints are:

```text
GET  /auth/status
POST /auth/setup
POST /auth/login
POST /auth/logout
```

`/auth/setup` requires both a native bootstrap session and a same-origin
request. `/auth/login` is enabled only after setup. State-changing Admin API
requests authenticated by a desktop cookie also require a matching `Origin`
header. CLI Bearer-token authentication remains valid and does not use these
password routes.

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
GET  /admin/model-catalog/settings
PUT  /admin/model-catalog/settings
POST /admin/model-catalog/refresh
POST /admin/model-catalog/import
POST /admin/model-catalog/lookup
```

The catalog is downloaded from models.dev into a local cache. Data-plane requests never
depend on a live models.dev request. `refresh` honors HTTP validators and keeps the last
usable cache when the remote source is temporarily unavailable.

`PUT /admin/model-catalog/settings` accepts
`{"proxy_url":"http://user:password@proxy.example.com:8080"}` and applies it
without restarting vibe-proxy. Send an empty `proxy_url` to clear the explicit
proxy and return to `HTTP_PROXY` / `HTTPS_PROXY` environment handling. Status
responses expose only whether a proxy is configured and its credential-free
origin; usernames and passwords are never returned.

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

## Multimodal Configuration

```text
GET /admin/multimodal
PUT /admin/multimodal
```

`GET /admin/multimodal` reports `provider: builtin|http`. Built-in OCR is the
default; an external endpoint and its authentication fields are only used when
the provider is `http`. Its snapshot also contains backend-resolved Vision fallback
candidates:

```json
{
  "enabled": true,
  "provider": "builtin",
  "vision_fallback_model": "gateway/kimi-k2.6",
  "vision_fallback_models": [
    {
      "target": "gateway/kimi-k2.6",
      "provider_id": "gateway",
      "model": "kimi-k2.6",
      "source": "models_dev"
    }
  ]
}
```

The array includes only configured models whose effective `image_input` capability is
`supported` and whose provider adapter can transport images. `source` is one of
`model_override`, `provider_default`, or `models_dev`, following that precedence. Unknown,
not-found, ambiguous, unsupported, and transport-incompatible models are omitted. Catalog
results are read from the active local snapshot and are not persisted into provider YAML.

`PUT /admin/multimodal` returns the same candidate array in its `multimodal` snapshot.
Before writing the configuration, it resolves a non-empty `vision_fallback_model` through
the same capability path used by requests. An invalid selection returns HTTP 400 with
`error: vision_fallback_invalid` and leaves the configuration file unchanged.

`POST /admin/multimodal/ocr/test` runs the selected
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
