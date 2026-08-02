# models.dev Vision Fallback Capability Design

## Problem

The data-plane capability resolver already uses the intended precedence:

1. per-model manual override;
2. provider-level manual default;
3. models.dev catalog result;
4. unknown.

The OCR settings page and multimodal configuration validator do not use that
effective result. The UI only lists models explicitly persisted as
`image_input: supported`, and static validation rejects a catalog-only Vision
fallback. As a result, a model such as `kimi-k2.6` can be shown as supporting
images by models.dev while remaining unavailable in the OCR fallback selector.

## Goals

- Treat an unambiguous models.dev result as the default model capability.
- Keep per-model and provider-level manual configuration authoritative.
- List catalog-supported models in the OCR Vision fallback selector without
  requiring the user to persist a redundant override.
- Exclude `unknown`, ambiguous, and unsupported models from selectable Vision
  fallback candidates.
- Use one backend capability decision for UI candidates, save-time validation,
  and request-time routing.
- Preserve safe behavior when the catalog is unavailable: an unknown model is
  never sent an image request as a Vision fallback.

## Non-goals

- Persisting models.dev results into `config.yaml`.
- Trusting ambiguous catalog matches.
- Removing manual capability controls.
- Changing OCR thresholds, OCR providers, or image transport formats.

## Documentation Decision

The ADR governance and retrospective decision log are tracked separately in
[PR #13](https://github.com/a448582655/vibe-proxy/pull/13). This change is
governed by ADR-0002, `docs/adr/0002-model-capability-resolution-precedence.md`,
which records the shared effective-capability precedence and requires runtime,
validation, Admin API, and UI consumers to use it consistently.

This fix restores conformance to that accepted decision, so it does not create
a second ADR. It still updates the affected current-state configuration, Admin
API, and OCR design documentation.

## Effective Capability Rules

The existing `multimodal.ResolveCapabilities` precedence remains the source of
truth:

| Per-model override | Provider default | models.dev | Effective result |
| --- | --- | --- | --- |
| set | any | any | per-model override |
| unset | set | any | provider default |
| unset | unset | exact or consensus match | models.dev result |
| unset | unset | unavailable, not found, or ambiguous | unknown |

An explicit `unknown` is an override and therefore suppresses the catalog
result. Only an effective `supported` result is eligible for Vision fallback.

## Backend Design

### Candidate generation

The runtime server will enumerate the configured models of every provider and
resolve each model with `multimodal.ResolveCapabilities`, using the current
models.dev snapshot. It will return only effective `supported` models whose
provider adapter can transport images.

`GET /admin/multimodal` will include a `vision_fallback_models` array alongside
the existing OCR configuration. Each entry contains:

```json
{
  "target": "provider-id/model-id",
  "provider_id": "provider-id",
  "model": "model-id",
  "source": "models_dev"
}
```

The source is one of `model_override`, `provider_default`, or `models_dev`.
Candidates are sorted by target for stable UI behavior. The same enriched
snapshot is returned after a successful `PUT /admin/multimodal`.

### Save-time validation

Before persisting a non-empty Vision fallback target, the runtime handler will:

1. resolve the target through the configured model resolver;
2. reject missing providers and targets that cannot be resolved;
3. calculate the effective image capability through the shared resolver;
4. require the effective result to be `supported`;
5. require the selected provider adapter to support Vision transport.

This validation happens before `config.yaml` is written. A rejected update must
leave the existing file and in-memory configuration unchanged.

### Static configuration validation

Static configuration loading cannot consult the runtime catalog service.
Therefore:

- an explicit `supported` capability remains valid;
- an explicit `unsupported` or `unknown` capability remains an error for a
  configured Vision fallback;
- absence of any explicit capability becomes a non-fatal
  `vision_fallback_unverified` warning because models.dev may supply the
  effective capability after the catalog cache is loaded.

Request-time routing still re-resolves capability through the shared resolver.
If a previously selected catalog-backed model becomes unknown, no image is sent
to that model and the request reports the existing invalid-fallback error.

## Frontend Design

The OCR settings component will stop deriving candidates from raw provider
configuration. It will render `vision_fallback_models` returned by the backend.

For each candidate, the UI displays the target and a short capability-source
label. A catalog-backed `kimi-k2.6` therefore appears as
`provider-id/kimi-k2.6` without a manual override.

If an already configured target is no longer present in the eligible candidate
array, the form preserves its value, marks it as currently unavailable, and
allows the user to clear it. It is not presented as an eligible candidate for a
new selection.

## Error Handling

- Catalog unavailable, not found, or ambiguous: effective capability is
  `unknown`; model is excluded.
- Catalog says supported, manual override says unsupported or unknown: manual
  override wins; model is excluded.
- Catalog says unsupported, manual override says supported: manual override
  wins; model is included.
- Save request selects a stale or invalid candidate: return HTTP 400 with
  `vision_fallback_invalid` semantics and do not mutate configuration.
- Existing catalog-backed selection loses catalog support: keep configuration
  visible, do not forward images to it, and surface the invalid state.

## Testing

Backend tests will cover:

- catalog-supported models are returned without explicit capability fields;
- per-model and provider defaults override catalog results;
- unknown, ambiguous, and unsupported models are excluded;
- candidate ordering and source metadata are stable;
- save accepts a catalog-supported model;
- save rejects unknown or manually unsupported models without overwriting the
  configuration;
- static validation warns, rather than fails, when capability must be resolved
  from the catalog;
- request-time resolution continues to block unknown targets.

Frontend contract tests will cover:

- the OCR selector consumes backend candidates rather than raw provider
  capabilities;
- catalog-backed targets are selectable;
- an unavailable existing selection remains visible and clearable;
- translated labels explain catalog, manual, and unavailable sources.

Final verification includes the focused Go tests, the full Go test suite,
frontend contract tests, TypeScript build, and production frontend build.

Documentation updates reference ADR-0002 and update the affected configuration,
Admin API, and OCR fallback contracts.
