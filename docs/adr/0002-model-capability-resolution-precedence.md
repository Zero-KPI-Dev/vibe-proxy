# ADR-0002: Resolve model capabilities through explicit precedence

## Status

Accepted

## Decision date

2026-07-20

## Recorded date

2026-08-02 (retrospective, clarified for all consumers)

## Context

Provider model-list endpoints usually return identifiers but not reliable input-modality metadata. An adapter's ability to encode an image only describes the transport protocol; it does not prove that every model behind that adapter accepts images. Private MaaS endpoints, renamed models, aliases, and provider-side capability restrictions also make model-name heuristics unsafe.

vibe-proxy therefore needs a single effective capability decision that combines local knowledge with models.dev catalog metadata. The absence of evidence must remain distinct from evidence that a model is unsupported.

At the time this ADR was recorded, the runtime resolver already implemented the source precedence, but the OCR fallback control plane and static validation still required an explicit local `supported` value. That partial reimplementation prevented catalog-resolved vision models from appearing as fallback choices. The remediation is tracked separately as a conformance fix to this decision.

## Decision

Represent image-input capability as the three-state value `supported`, `unsupported`, or `unknown`.

Resolve the effective capability through this precedence, stopping at the first source that provides a value:

1. per-model manual correction;
2. provider-level default;
3. unambiguous models.dev catalog metadata;
4. `unknown` when no source resolves the capability.

An explicit local `unknown` is a manual correction and therefore prevents lower-priority catalog inference. An unambiguous catalog result may be an exact match or a consensus match whose candidates agree on the relevant capability. Missing, ambiguous, or conflicting catalog data resolves to `unknown`.

All runtime routing, configuration validation, admin API responses, and UI candidate lists must consume the same effective resolution. A consumer must not rebuild a subset of the precedence from raw configuration fields.

Only an effective `supported` result is eligible as a Vision fallback target. Effective `unsupported` and unresolved `unknown` models do not appear in fallback candidates and cannot pass fallback validation. `Unknown` is not automatically converted to `unsupported`; outside fallback selection, existing compatibility behavior may preserve the request path.

The models.dev catalog is advisory seed metadata. It never overrides local corrections, never changes routing or authentication by itself, and is not fetched in the data-plane request path.

## Consequences

### Positive

- Public multimodal models normally work without repetitive manual capability declarations.
- Local users can correct catalog errors, private models, renamed models, and restricted intermediary endpoints.
- `Unknown` preserves uncertainty instead of silently triggering OCR or sending fallback traffic to an unverified target.
- A shared resolver keeps runtime, save-time validation, admin APIs, and UI choices consistent.
- Catalog failures do not block service startup or ordinary LLM requests.

### Negative

- Effective capability can change after a catalog refresh when no higher-priority local value exists.
- The project must maintain provider/model matching rules, cache behavior, and provenance reporting.
- Control-plane operations that validate catalog-backed choices need access to the same immutable catalog snapshot as runtime resolution.
- A wrong but unambiguous external catalog entry can influence behavior until a local correction is recorded.

### Neutral

- The UI should expose provenance so users can distinguish manual, provider-default, catalog, and unknown results.
- A manual correction remains stored separately from catalog data; refreshing models.dev never rewrites user configuration.

## Alternatives considered

### Require manual declarations for every model

This is deterministic but was rejected as the default because it discards the value of the catalog, creates repetitive setup work, and leaves imported public models incomplete until users edit each one.

### Trust models.dev exclusively

This was rejected because models.dev is an external community catalog rather than a provider guarantee. It cannot describe private names, intermediary restrictions, or every temporary catalog error.

### Infer capability from model names

Patterns such as `vision`, `vl`, or `4o` were rejected because private aliases and renamed or versioned models produce both false positives and false negatives.

### Treat unknown as supported

This was rejected for fallback targets because it can route original images and potentially sensitive content to a model that has not been verified to accept them.

### Treat unknown as unsupported

This was rejected because upgrading an existing configuration with no capability metadata would unexpectedly trigger degradation or rejection for models that may already accept images.

## Security and operational considerations

- Catalog input is data, not executable code, and must be parsed under response-size and schema limits.
- Official catalog retrieval uses HTTPS, conditional requests, timeouts, and an on-disk stale cache. A refresh failure leaves the last valid immutable snapshot in use.
- Ambiguous or conflicting matches fail to `unknown`; fuzzy name similarity never controls the data plane.
- Resolution provenance and catalog match status should be observable without exposing proxy credentials, provider secrets, or the complete raw catalog in routine admin responses.
- A local correction can immediately disable a catalog-backed capability without waiting for an external catalog update.
- The models.dev remediation must validate saved Vision fallback choices through this resolver before persisting configuration.

## References

- [Architecture — Model Registry and Resolver](../architecture.md)
- [Configuration Schema — Model Capability Catalog](../config-schema.md)
- [OCR Image Fallback Design](../ocr-fallback-design.md)
- [`788e74b` — add the multimodal capability pipeline](https://github.com/a448582655/vibe-proxy/commit/788e74b)
- [`277a31a` — design catalog-backed Vision fallback remediation](https://github.com/a448582655/vibe-proxy/blob/277a31a/docs/superpowers/specs/2026-08-02-models-dev-vision-fallback-design.md)
