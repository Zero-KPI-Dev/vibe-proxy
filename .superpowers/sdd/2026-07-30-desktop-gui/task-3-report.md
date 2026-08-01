# Task 3 Report: Allow-Listed Desktop Control Contract

## Status

Implemented the four authenticated desktop-control routes without adding frontend or native desktop behavior.

## Behavior matrix

| Route | Method | Authenticated behavior | CLI / no controller | Desktop controller | Errors |
| --- | --- | --- | --- | --- | --- |
| `/admin/desktop` | `GET` | Uses `s.adminAuthorize`; bearer and desktop-session authorization remain centralized there. | `200 {"available":false}` | Returns `Controller.Snapshot()` unchanged. | Wrong method: JSON `405`. |
| `/admin/desktop/preferences` | `PUT` | Uses `s.adminAuthorize`. | JSON `409`. | Accepts only `ask`, `tray`, or `quit`; calls `SetCloseBehavior` once and returns the persisted enum. | Invalid JSON/enum: JSON `400`; controller error: generic JSON `500`. |
| `/admin/desktop/open-data-dir` | `POST` | Uses `s.adminAuthorize`. | JSON `409`. | Calls `OpenDataDir` once and returns `{"opened":true}`. | Controller error: generic JSON `500`. |
| `/admin/desktop/import-config` | `POST` | Uses `s.adminAuthorize`. | JSON `409`. | Calls `ImportConfig` once. A successful import reloads through the same load-and-validate path as `/admin/config/reload`, then returns import data plus `loaded_at`. | Controller error: generic JSON `500`; reload/load/validation errors retain the existing JSON `400` behavior. |

Routes are registered unconditionally in `Server.Routes`, so CLI clients can detect that desktop controls are unavailable. There is no route that accepts an arbitrary operation name or native method.

## Files

- Modified `internal/runtime/server.go`
  - Registered all four routes.
  - Extracted `reloadRuntimeConfig`, the shared validated snapshot replacement used by both config reload and import.
- Modified `internal/runtime/desktop_handlers.go`
  - Added exact-method desktop handlers, controller availability handling, enum validation, generic controller-error mapping, and import reload integration.
- Modified `internal/runtime/desktop_handlers_test.go`
  - Added a recording fake controller and contract tests.
- Reviewed, unchanged: `internal/desktopbridge/controller.go`, `internal/runtime/options.go`, and `internal/gatewayapp/options.go`; Task 2 already supplied the controller seam and option propagation.

## RED evidence

Before adding the handlers, ran the required Go 1.26.5 focused command:

```sh
docker run --rm -v "$PWD":/src -w /src \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./internal/runtime -run "DesktopController|DesktopPreferences|DesktopImport" -v'
```

It failed as expected: all four unregistered `/admin/desktop...` paths fell through to the SPA (`200` HTML), the fake controller was not invoked, and the CLI snapshot was not `{"available":false}`.

## GREEN evidence

After the minimal implementation, the same focused Go 1.26.5 command passed. It covers:

- CLI availability snapshot and `409` native actions.
- Authentication and exact method guards for all four routes.
- Fake snapshot serialization.
- Rejection of `delete-files` and acceptance/persistence of `ask`, `tray`, and `quit` only.
- Single `OpenDataDir` and `ImportConfig` calls.
- Import-triggered validated runtime reload and returned `loaded_at`.
- Generic JSON `500` controller errors that do not expose a local file path.

## Full verification and vet

Executed in Go 1.26.5 Docker with `CGO_ENABLED=0`:

```sh
/usr/local/go/bin/go test ./internal/runtime ./internal/gatewayapp && \
/usr/local/go/bin/go test ./... && \
/usr/local/go/bin/go vet ./...
```

All commands exited `0`.

## Self-review

- Each new valid-operation handler calls `s.adminAuthorize`; no alternate bearer or cookie authorization path was added.
- All four routes are registered in both CLI and desktop modes.
- The only accepted close behavior values are the three `desktopbridge.CloseBehavior` constants.
- No controller error text is returned to clients.
- Import does not grant the controller runtime mutation access; runtime replacement happens only after `ImportConfig` returns through `reloadRuntimeConfig`.
- The shared reload helper preserves the existing failed-load/failed-validation behavior: the current snapshot is retained unless validation succeeds.
- Diff scope contains only runtime route/handler/test files; no Wails/native or frontend files changed.
- `git diff --check` was clean before commit.

## Commit

```sh
git add internal/desktopbridge internal/runtime internal/gatewayapp \
  .superpowers/sdd/2026-07-30-desktop-gui/task-3-report.md
git commit -m "feat: expose allow-listed desktop controls"
```

## Concerns

No unresolved implementation concerns. Native file selection/copying and desktop-native controller implementation remain intentionally deferred to Task 6.
