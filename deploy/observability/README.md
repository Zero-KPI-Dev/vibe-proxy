# Optional external observability

This directory contains integration artifacts, not runtime dependencies:

- `prometheus.yml` — same-host scrape configuration for the loopback metrics endpoint;
- `grafana/vibe-proxy-dashboard.json` — importable Grafana dashboard with a selectable Prometheus data source.

See [`docs/prometheus-grafana.md`](../../docs/prometheus-grafana.md) for the
network boundary, metric semantics, PromQL formulas, and setup steps.
