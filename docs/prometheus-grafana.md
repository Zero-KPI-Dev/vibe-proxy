# Prometheus and Grafana integration

vibe-proxy keeps its local observability UI lightweight and SQLite-backed. It
does **not** bundle or start Prometheus or Grafana. Operators who already use an
observability stack can scrape the low-cardinality `/metrics` surface and import
the versioned dashboard shipped in this repository.

## Network boundary

`/metrics` is served only by the loopback control-plane listener, which defaults
to `127.0.0.1:8081`. It is deliberately absent from the Agent-facing data-plane
listener, even when `server.listen` is bound to `0.0.0.0`.

The sample configuration therefore assumes Prometheus runs directly on the same
host as vibe-proxy. A Prometheus container usually cannot reach a service bound
to the host loopback interface. Do not expose the full control-plane listener to
work around that limitation. Use a same-host Prometheus or collector, or wait for
a future dedicated metrics listener.

Check the endpoint locally:

```bash
curl http://127.0.0.1:8081/metrics
```

Then start Prometheus with the repository sample:

```bash
prometheus --config.file=deploy/observability/prometheus.yml
```

If the configured `server.admin_listen` uses a different port, update the target
in the sample first.

## Metric contract

All labels are bounded operational dimensions. Request IDs, traces, sessions,
projects, Agent identities, client-key names, and client-key prefixes are never
exported as Prometheus labels.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `vibe_proxy_requests_total` | counter | `model`, `channel`, `status` | Completed gateway requests by exact HTTP status. |
| `vibe_proxy_ttft_seconds` | histogram | `model`, `channel` | Time from gateway ingress to first output token. |
| `vibe_proxy_tpot_seconds` | histogram | `model`, `channel` | Average time per generated output token after the first token. |
| `vibe_proxy_tps` | histogram | `model`, `channel` | Output generation throughput in tokens per second. |
| `vibe_proxy_tokens_total` | counter | `model`, `channel`, `kind` | Provider usage counters; `kind` is `prompt`, `completion`, `cache_read`, or `cache_write`. |
| `vibe_proxy_prompt_cache_requests_total` | counter | `model`, `channel`, `state` | Cache telemetry availability; `state` is `reported`, `not_reported`, or `read`. `read` is a subset of `reported`. |
| `vibe_proxy_prompt_cache_tokens_total` | counter | `model`, `channel`, `kind` | Cache-only denominator and volumes for requests that reported cache telemetry; `kind` is `eligible`, `read`, or `write`. |

A missing TTFT, TPOT, TPS, or provider cache field is not observed as zero. This
prevents unavailable telemetry from distorting latency and cache calculations.

### Prompt Cache Token hit ratio

The overview ratio is token weighted, not an average of request percentages:

```promql
sum(rate(vibe_proxy_prompt_cache_tokens_total{kind="read"}[$__rate_interval]))
/
clamp_min(sum(rate(vibe_proxy_prompt_cache_tokens_total{kind="eligible"}[$__rate_interval])), 1e-9)
```

`eligible` is the normalized provider input-token total only for requests whose
provider actually reported cache fields. This is intentionally different from
`cache_read / (cache_read + cache_write)`, which ignores uncached input.

### Cache telemetry coverage

Coverage answers whether providers supplied enough information to calculate the
ratio:

```promql
sum(rate(vibe_proxy_prompt_cache_requests_total{state="reported"}[$__rate_interval]))
/
clamp_min(sum(rate(vibe_proxy_prompt_cache_requests_total{state=~"reported|not_reported"}[$__rate_interval])), 1e-9)
```

It is a data-quality indicator, not a cache-efficiency KPI. The dashboard does
not promote “percentage of requests that touched cache” because one cached token
and one million cached tokens would otherwise be treated as equivalent.

## Grafana dashboard

Import [`deploy/observability/grafana/vibe-proxy-dashboard.json`](../deploy/observability/grafana/vibe-proxy-dashboard.json)
through **Dashboards → New → Import**, then select your Prometheus data source.
The dashboard contains:

- request and error rates;
- TTFT average and P50/P95/P99;
- TPOT average and P50/P95;
- TPS average and P50/P95;
- prompt and completion token throughput;
- cache read/write volume, token-weighted hit ratio, and telemetry coverage;
- model and provider-channel filters.

The dashboard is an optional integration artifact. The embedded vibe-proxy UI
remains the source for per-request Agent, client-key, Session, routing, OCR,
Vision, and sanitized payload details.
