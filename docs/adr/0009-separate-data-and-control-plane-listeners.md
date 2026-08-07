# ADR-0009: Separate public data-plane and loopback control-plane listeners

## Status

Accepted

## Decision date

2026-08-06

## Recorded date

2026-08-06

## Context

vibe-proxy is local-first, but a user may intentionally expose its LLM-compatible
endpoint to other devices by binding `server.listen` to `0.0.0.0` or a LAN
address. The current HTTP server also serves the embedded UI, authentication
setup, Admin API, desktop bootstrap routes, and Prometheus metrics from that same
listener. Changing the bind address therefore expands the control plane's
network reach even though the user's intent is only to share the data plane.

Admin bearer authentication is useful defense in depth, but it is not an
adequate network boundary. A missing authorization check, a future unauthenticated
bootstrap route, or a reverse proxy that changes the apparent remote address
could expose local configuration, diagnostics, or secrets. Filtering requests by
`RemoteAddr` would leave all management handlers attached to the public server
and would make the security property depend on proxy topology.

The desktop host also needs a stable local address for its WebView while client
agents need the independently configured data-plane address. These are different
responsibilities and must no longer be represented by one listener.

## Decision

vibe-proxy will run two HTTP listeners in the same process:

- `server.listen` is the **data-plane listener**. It may bind loopback, a LAN
  address, or a wildcard address and exposes only supported LLM protocol routes
  plus a minimal health endpoint.
- `server.admin_listen` is the **control-plane listener**. It defaults to
  `127.0.0.1:8081`, must bind an IP loopback address, and exposes the embedded
  UI, authentication setup/login, Admin API, desktop bootstrap routes,
  Prometheus metrics, and a health endpoint.

The required routing boundary is:

| Surface | Data listener | Control listener |
| --- | --- | --- |
| `/v1/models` and supported LLM inference paths | Yes | No |
| `/healthz` | Yes | Yes |
| `/admin/*` | No | Yes |
| `/auth/*` | No | Yes |
| `/desktop/bootstrap/*` | No | Yes |
| `/metrics` | No | Yes |
| Embedded SPA and client-side routes | No | Yes |

Additional invariants:

1. `admin_listen` must use a literal loopback IP (`127.0.0.0/8` or `::1`) and a
   valid port. Wildcard addresses, hostnames, and non-loopback addresses are
   rejected during configuration validation.
2. The two listeners must use different configured non-zero ports. This
   deliberately avoids platform-dependent wildcard/specific-address bind
   overlap. Port `0` remains valid for tests and embedding, where the operating
   system assigns distinct ports, and the application reports both actual bound
   addresses.
3. Both listeners share one runtime, configuration snapshot, SQLite store,
   telemetry recorder, and retention worker. This is listener isolation, not a
   separate control-plane process.
4. Startup is transactional: the application becomes ready only after both
   listeners bind. If either bind fails, the other listener and all owned
   resources are closed.
5. An unexpected terminal failure of either HTTP server terminates the whole
   application. Graceful shutdown drains both listeners before closing shared
   resources.
6. `App.Address()` remains the data-plane address for source compatibility;
   `App.AdminAddress()` exposes the control-plane address. Desktop navigation
   always uses the latter, while API URL copy/display uses the former.
7. Listener addresses are startup-only settings. A configuration write or hot
   reload may persist new values, but rebinding requires an application restart.
8. Admin authentication remains enabled on the loopback listener as defense in
   depth. Data-plane client-key authentication is unchanged.

Existing configurations that omit `admin_listen` receive the safe default
`127.0.0.1:8081`. The LLM endpoint remains on the existing `server.listen`
address, while browser-only users must open the new local control-plane address.
This control-plane URL change is accepted during the release-candidate period in
exchange for a deterministic security boundary.

## Consequences

### Positive

- Binding the data plane to `0.0.0.0` no longer publishes management routes.
- The security boundary is enforced by the operating-system listener rather
  than request metadata or reverse-proxy behavior.
- Desktop navigation and externally shared API URLs have explicit, independent
  semantics.
- Data and control routing can be tested independently and extended without
  accidental cross-exposure.

### Negative

- The process owns two sockets and the lifecycle implementation must coordinate
  partial startup, unexpected failure, shutdown, and handler draining.
- The default control plane moves from port `8080` to `8081`; browser-only users
  and local automation that called Admin APIs on the data port must migrate.
- A second configurable port can conflict with another local process.
- The built-in server still provides plain HTTP. Safe access across untrusted
  networks requires an external TLS reverse proxy in front of the data listener.

### Neutral

- The control plane remains in the same executable and process.
- Provider networking, model routing, OCR/Vision processing, SQLite, and
  observability semantics do not change.
- Loopback data-plane use remains supported; it simply uses a separate local
  control-plane port.

## Alternatives considered

### Filter management routes by the request's remote address

Rejected because reverse proxies can make external traffic appear local and all
management handlers would remain mounted on the public HTTP server.

### Keep one listener and rely only on admin authentication

Rejected because authentication is not a substitute for minimizing network
exposure, especially for bootstrap and future management routes.

### Add a separate control-plane process

Rejected for now because it complicates installation, process supervision,
configuration ownership, and desktop lifecycle without improving the immediate
local-first use case enough to justify the cost.

### Make the legacy combined listener an automatic compatibility mode

Rejected because the effective security boundary would then depend on an
omitted field and old configurations could remain unintentionally exposed.

## Security and operational considerations

- Configuration validation must reject any non-loopback control-plane bind;
  runtime startup must not silently weaken this constraint.
- Public routers use an explicit allowlist and do not mount the SPA catch-all,
  authentication, metrics, desktop, or Admin handlers.
- The control-plane address may be shown locally, but it is not an externally
  usable API endpoint and should not be advertised as one.
- Operators exposing the data listener beyond a trusted LAN must use client
  keys, host firewall rules, and a TLS-capable reverse proxy.
- Failure messages and diagnostics identify whether the data or control bind
  failed without including credentials or configuration secrets.

## References

- [Data-plane and Local Control-plane Listener Design](../data-control-listener-design.md)
- [Architecture](../architecture.md)
- [Configuration Schema](../config-schema.md)
- [ADR-0005: Wails desktop gateway lifecycle](0005-wails-desktop-gateway-lifecycle.md)
