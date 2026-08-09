# ADR-0011: Normalize per-request input-cache metrics across providers

## Status

Accepted

## Decision date

2026-08-09

## Recorded date

2026-08-09

## Context

Providers report prompt caching with incompatible usage shapes. OpenAI-style
responses commonly report cached tokens as a subset of total prompt tokens,
while Anthropic reports uncached input, cache-read input, and cache-creation
input as separate counters. A ratio such as `cache_read / (cache_read +
cache_write)` can report 100 percent even when most input tokens were uncached.

The UI and Prometheus integration need one honest per-request value without
turning an absent provider field into a zero-percent cache hit. Aggregating an
unweighted average of request percentages would also overrepresent small
requests.

## Decision

Define the provider-neutral usage contract as follows:

- `prompt_tokens` is the normalized total input-token count, including uncached,
  cache-read, and cache-created input where the provider reports those categories
  separately.
- `cache_read_tokens` is input served from a provider prompt cache.
- `cache_write_tokens` is input written to or created in a provider prompt cache.
- `cache_metrics_reported` distinguishes a provider-reported zero from absent
  cache telemetry.
- `cache_hit_ratio` is a per-request token ratio:

  ```text
  cache_read_tokens / prompt_tokens
  ```

  It is present only when cache metrics are reported and the normalized prompt
  token denominator is positive.

Provider adapters own normalization because only an adapter knows the upstream
usage semantics. OpenAI-compatible parsing treats cached input as a subset of
the reported prompt total. Anthropic parsing computes normalized prompt tokens
from uncached input plus cache-read and cache-creation input.

Time-range aggregation uses a weighted token ratio:

```text
sum(cache_read_tokens) / sum(prompt_tokens for cache-reporting requests)
```

The primary UI presents the per-request ratio. The aggregate weighted ratio,
cache-read/write token volume, and telemetry reporting coverage may appear in
overview charts. The fraction of requests with any cache read is not called a
cache hit rate; if shown at all, it is labeled "requests using cache" and remains
a secondary diagnostic.

OCR result caching and Vision evidence caching are separate gateway-owned cache
domains. They must not be combined with provider prompt-cache metrics.

## Consequences

### Positive

- A displayed percentage describes how much of one request's input was served
  from prompt cache.
- Cross-provider charts use an explicit normalized denominator.
- Missing telemetry is shown honestly instead of becoming a false zero.
- Weighted aggregates correspond to token and cost impact.

### Negative

- The usage contract gains an explicit reporting-state field.
- Existing Anthropic normalized prompt totals and cache ratios change to the new
  semantics.
- OpenAI-compatible MaaS implementations with nonstandard usage fields may need
  adapter-specific parsing or remain "not reported".

### Neutral

- Provider-native raw usage can still be retained in captured payloads when
  content capture is enabled.
- Request-count cache usage may still be derived for diagnostics but is not a
  primary optimization metric.

## Alternatives considered

### Cache read divided by cache read plus cache write

Rejected because it ignores uncached input and does not describe the proportion
of the request served from cache.

### Treat missing cache fields as zero

Rejected because zero usage and unavailable telemetry have different meanings.

### Average per-request percentages for an overview

Rejected because a tiny request and a million-token request would receive equal
weight.

## Security and operational considerations

- Cache counters are metadata and do not contain prompt content.
- Prometheus labels remain low-cardinality; request IDs, Client Keys, Agents, and
  sessions are not added as labels.
- Grafana queries derive ratios from monotonically increasing counters rather
  than exporting a mutable aggregate gauge.

## References

- [ADR-0001: Canonical IR adapter boundary](0001-canonical-ir-adapter-boundary.md)
- [ADR-0007: Local-first observability trace model](0007-local-first-observability-trace-model.md)
- [Live Observability V2 Design](../observability-live-v2-design.md)

