# ADR-0005: Embed and own the local gateway lifecycle in the Wails desktop host

## Status

Accepted

## Decision date

2026-07-30

## Recorded date

2026-08-02 (retrospective, including later authentication and startup hardening)

## Context

The CLI and browser control plane serve local and headless users, but Windows and macOS users also need a launchable desktop application, native window and tray behavior, application-data ownership, and visible startup recovery. Rebuilding the control plane as native widgets would duplicate the React UI, while supervising a separate gateway process would add inter-process startup, authentication, update, and shutdown failure modes.

The desktop shell must reuse the production gateway rather than creating a second backend, and exposing the loopback management UI to ordinary browsers requires a stronger distinction between process-owned bootstrap sessions and durable user authentication.

## Decision

Use Wails v3 with the operating-system WebView as the Windows and macOS desktop host. Keep Wails in a nested Go module at an exactly tested version so the root CLI module and ordinary server tests do not compile desktop dependencies.

The desktop process embeds and owns the same reusable `internal/gatewayapp` lifecycle used by the CLI:

- resolve platform application-data paths and create or validate the first-run configuration;
- create a process-local random internal admin token;
- load the desktop management-password store;
- bind the configured loopback listener before reporting readiness;
- start the gateway, SQLite, routing, OCR, telemetry, and existing HTTP control plane in-process;
- create a short-lived, single-use bootstrap nonce and navigate the WebView to the loopback bootstrap route;
- show the window and tray only after the gateway is ready;
- shut down only the gateway instance owned by the desktop host, revoke sessions, and close resources in an ordered lifecycle.

The WebView loads the same React control plane and HTTP admin API used by browsers. No duplicate frontend bundle or Wails-only administration transport is introduced.

Authentication is divided by caller:

- The process-owned native window exchanges its one-time nonce for an ephemeral `HttpOnly`, `SameSite=Strict` session cookie. The raw internal token is not placed in URLs or local storage.
- Regular browsers authenticate with the user-created management password and receive an `HttpOnly`, same-origin session. Only the Argon2id password hash is persisted in `auth.json`.
- CLI/server installations continue to use the configured admin bearer token.
- Cookie-authenticated state-changing requests enforce same-origin checks.

Desktop state lives under the platform application-data directory rather than the current working directory. Startup errors, port conflicts, cleanup failures, and WebView failures are surfaced through native dialogs, logs, or tray recovery actions. The host does not silently select another port and does not claim or stop an externally started gateway.

## Consequences

### Positive

- Desktop and browser users share the same tested control plane, streaming behavior, OCR tooling, provider administration, and observability.
- The Go gateway stays in the same process, avoiding a sidecar protocol and separate process supervisor.
- Wails reuses the existing Go and React stack while providing native windows, tray/menu-bar behavior, dialogs, close interception, and packaging.
- The reusable gateway lifecycle improves CLI shutdown behavior and makes resource ownership explicit.
- Platform application-data paths make installed applications independent of launcher working directories.
- Browser authentication does not expose the internal admin token in URLs or persistent browser storage.

### Negative

- Wails v3 is a pre-release dependency that requires exact pinning and native Windows/macOS build validation before upgrades.
- The application depends on the operating-system WebView and must handle missing or broken WebView runtimes.
- One process now coordinates GUI, HTTP server, database, tray, sessions, and graceful shutdown, making lifecycle race testing essential.
- Native packaging, startup diagnostics, and OS-specific acceptance tests add maintenance cost beyond the CLI.
- Loopback HTTP remains a local network boundary rather than an in-process-only UI channel.

### Neutral

- Linux remains a supported CLI plus browser-control-plane platform; this decision does not add a Linux desktop GUI.
- The desktop nested module may evolve independently from the root Go module, but both consume the same repository gateway code.
- Signing, notarization, auto-update, and application-store distribution remain separate future decisions.

## Alternatives considered

### Tauri v2 with a Go sidecar

Tauri offers mature packaging and a lightweight WebView host, but it would add a Rust backend toolchain around the Go gateway, a sidecar process, supervision, and cross-process update coordination.

### Electron

Electron would reuse React directly but bundle Chromium and Node.js. Its package size, memory footprint, and multiprocess runtime conflict with the project's lightweight local-tool positioning.

### Native Windows and macOS controls

Native UI toolkits would avoid a WebView but duplicate the existing control plane, split features and tests across platforms, and require two native UI implementations.

### Launch the gateway as an external child process

This was rejected because readiness, port ownership, crash recovery, authentication bootstrap, log collection, and version compatibility would need a new IPC and supervision contract.

### Keep only the browser UI

The CLI/browser workflow remains supported, but it does not provide native launch, tray behavior, installer integration, data-directory discovery, or visible recovery expected from a Windows/macOS desktop application.

## Security and operational considerations

- The management listener is bound to a configured loopback address. Loopback reduces exposure but does not replace authentication because other local processes and ordinary browsers can reach it.
- Bootstrap nonces are cryptographically random, expire quickly, are consumed once, and are revoked with all desktop sessions when the process exits.
- Browser sessions use `HttpOnly`, same-origin cookies; cookie-authenticated mutations require an origin check.
- The desktop management password is stored only as an Argon2id hash. Deleting `auth.json` resets authentication without deleting configuration, providers, keys, or request history.
- The internal admin token remains process-local and is not persisted, placed in URLs, or written to browser storage.
- Startup does not report readiness before the listener is bound. Failed startup and shutdown paths retain ownership until cleanup succeeds or a visible recovery path is provided.
- Port conflicts do not trigger automatic reassignment because clients depend on a stable endpoint. The host never shuts down a gateway it does not own.
- A WebView navigation failure leaves a healthy gateway and tray available for browser and log recovery.
- Wails upgrades occur in isolated dependency changes and require Windows and macOS package builds.

## References

- [Desktop GUI Design](../superpowers/specs/2026-07-30-desktop-gui-design.md)
- [Release and First-Run Security](../release.md)
- [`baceb95` — design the desktop application shell](https://github.com/a448582655/vibe-proxy/commit/baceb95)
- [`15ec5be` — implement the desktop host lifecycle](https://github.com/a448582655/vibe-proxy/commit/15ec5be)
- [`a652ab0` — add the Wails desktop application shell](https://github.com/a448582655/vibe-proxy/commit/a652ab0)
- [`dfd982a` — authenticate desktop browser sessions](https://github.com/a448582655/vibe-proxy/commit/dfd982a)
- [`c62553d` — add desktop management-password login](https://github.com/a448582655/vibe-proxy/commit/c62553d)
- [`a71b380` — surface Windows desktop startup failures](https://github.com/a448582655/vibe-proxy/commit/a71b380)
- [`d0d9036` — make the desktop startup guard portable](https://github.com/a448582655/vibe-proxy/commit/d0d9036)
