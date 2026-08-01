# Desktop GUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add tested Windows and macOS desktop applications that host the existing React control plane and run the Go gateway in-process, while preserving the CLI and Linux Web UI.

**Architecture:** Move gateway startup and shutdown into a reusable root-module lifecycle package, then add an isolated `desktop/` Go module pinned to Wails v3. The desktop process starts the gateway on loopback, authenticates its WebView through a one-time `HttpOnly` session bootstrap, and exposes only allow-listed native operations to the existing control plane.

**Tech Stack:** Go 1.26.5 build toolchain with the existing Go 1.25 module language level, Wails `v3.0.0-alpha2.117`, React 19.2.8, TypeScript 5.7, WebView2, WKWebView, NSIS, macOS application bundles and DMGs, GitHub Actions.

## Global Constraints

- Keep `github.com/wailsapp/wails/v3` and all native desktop dependencies out of the root `go.mod`.
- Pin Wails exactly to `v3.0.0-alpha2.117`; build and release automation must not use an unpinned Wails version.
- Preserve the CLI arguments, built-in OCR worker argument, embedded Web UI, Linux behavior, and current data-plane URLs.
- Bind the listener before reporting readiness; startup errors must be returned as structured errors rather than terminating inside a library package.
- Keep the desktop host and gateway in one operating-system process.
- Do not store the desktop admin token in a URL, browser storage, configuration file, preference file, or log.
- Keep existing Bearer-token admin authentication valid in browser and CLI deployments.
- Require a same-origin `Origin` header for cookie-authenticated state-changing admin requests.
- Store desktop data under `%LOCALAPPDATA%\vibe-proxy` on Windows and `~/Library/Application Support/vibe-proxy` on macOS.
- The first user close asks whether to minimise to tray, exit, or cancel; the first two choices persist and remain resettable.
- Keep the application usable through the tray and external-browser fallback when the WebView window is hidden or cannot render.
- Do not silently change the configured gateway port when binding fails.
- Preserve user configuration and SQLite data during application uninstall.
- Keep automatic updates, start-at-login, store distribution, and Linux desktop packaging outside this implementation.
- Complete every task with its targeted tests and commit before starting the next task.

---

## File and Module Map

### Root Go module

- `internal/gatewayapp/options.go`: reusable startup inputs and listener injection.
- `internal/gatewayapp/app.go`: config, SQLite, runtime, listener, HTTP server, readiness, wait, and graceful shutdown lifecycle.
- `internal/gatewayapp/status.go`: immutable lifecycle status and structured startup errors.
- `internal/gatewayapp/app_test.go`: lifecycle, binding, shutdown, database-release, and integration coverage.
- `internal/desktopbridge/controller.go`: desktop-neutral controller types shared by runtime and the nested desktop module.
- `internal/auth/desktop_session.go`: one-time bootstrap nonce and process-local admin-session storage.
- `internal/auth/desktop_session_test.go`: expiry, single-use, session, and constant-time lookup coverage.
- `internal/runtime/options.go`: optional desktop authentication and controller dependencies.
- `internal/runtime/desktop_handlers.go`: bootstrap and allow-listed desktop admin routes.
- `internal/runtime/desktop_handlers_test.go`: HTTP authentication, origin, and controller route coverage.
- `cmd/vibe-proxy/main.go`: thin CLI entrypoint using `gatewayapp`.

### Frontend

- `frontend/src/lib/types.ts`: desktop snapshot and preference response types.
- `frontend/src/lib/api.ts`: cookie-aware requests and desktop admin operations.
- `frontend/src/components/desktop-settings.tsx`: desktop-only close behavior, data directory, and config import controls.
- `frontend/src/pages/settings.tsx`: desktop detection, desktop-auth presentation, and component composition.
- `frontend/src/locales/en.json`: English desktop strings.
- `frontend/src/locales/zh-CN.json`: Simplified Chinese desktop strings.
- `frontend/scripts/check-desktop-settings.mjs`: static frontend contract checks.
- `frontend/package.json`: include the desktop-settings check in `npm test`.
- `internal/runtime/web/dist/`: rebuilt embedded frontend.

### Nested desktop Go module

- `desktop/go.mod`, `desktop/go.sum`: isolated root replacement and exact Wails pin.
- `desktop/tools.go`: build-tagged Wails CLI dependency pin retained by `go mod tidy`.
- `desktop/main.go`: OCR worker, version handling, native application creation, and process exit.
- `desktop/app/paths.go`, `paths_darwin.go`, `paths_windows.go`: application-data path resolution.
- `desktop/app/preferences.go`: close behavior and window preference persistence.
- `desktop/app/preferences_test.go`: defaults, atomic save, corrupt-file backup, and unknown-field coverage.
- `desktop/app/close.go`: platform-neutral close decision state machine.
- `desktop/app/close_test.go`: ask, tray, quit, cancel, reset, and programmatic-quit coverage.
- `desktop/app/controller.go`: implementation of `desktopbridge.Controller`.
- `desktop/app/host.go`: gateway ownership, bootstrap URL, tray commands, and shutdown orchestration.
- `desktop/app/host_test.go`: fake-window, fake-dialog, fake-tray, and fake-system operation tests.
- `desktop/wailsapp/contract.go`: platform-neutral native window and menu configuration used by tests.
- `desktop/wailsapp/application.go`: Wails application, window, single-instance, and shutdown adapter.
- `desktop/wailsapp/dialogs.go`: question, error, and file-picker adapters.
- `desktop/wailsapp/tray.go`: system tray and menu adapter.
- `desktop/wailsapp/system.go`: clipboard, browser, and directory opener adapter.
- `desktop/assets/appicon.svg`: canonical scalable desktop icon.
- `desktop/assets/tray.png`, `desktop/assets/tray-template.png`: Windows and macOS tray assets.
- `desktop/build/`: Wails Taskfiles, platform metadata, generated `.ico` and `.icns`, and NSIS resources.

### Build, release, and documentation

- `.github/workflows/ci.yml`: root regression checks plus desktop pure-Go module tests.
- `.github/workflows/desktop-build.yml`: native Windows and macOS build/package smoke tests.
- `.github/workflows/release.yml`: desktop package jobs and release aggregation.
- `scripts/release/build-desktop.sh`: exact-version Wails CLI invocation and version injection.
- `scripts/release/package-desktop-portable.ps1`: Windows portable archive layout.
- `scripts/release/package-desktop-dmg.sh`: macOS desktop `.app` to DMG packaging.
- `scripts/release/verify-assets.sh`: sixteen-package release manifest validation.
- `scripts/release/test-desktop-packaging.sh`: deterministic package-name and content contracts.
- `docs/development.md`: desktop development prerequisites and commands.
- `docs/release.md`: desktop artifacts, unsigned RC guidance, and release gates.
- `README.md`: desktop, CLI, and Linux installation choices.
- `docs/desktop-rc-checklist.md`: physical Windows and macOS acceptance record.

---

### Task 1: Extract the Shared Gateway Lifecycle

**Files:**
- Create: `internal/gatewayapp/options.go`
- Create: `internal/gatewayapp/status.go`
- Create: `internal/gatewayapp/app.go`
- Create: `internal/gatewayapp/app_test.go`
- Modify: `cmd/vibe-proxy/main.go`

**Interfaces:**
- Produces:

```go
package gatewayapp

type Options struct {
	ConfigPath string
	Runtime    runtime.Options
	Listen     func(network, address string) (net.Listener, error)
}

type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
	StateFailed   State = "failed"
)

type Status struct {
	State     State
	Address   string
	StartedAt time.Time
	LastError string
}

type StartupError struct {
	Stage   string
	Address string
	Err     error
}

func Start(ctx context.Context, options Options) (*App, error)
func (a *App) Ready() <-chan struct{}
func (a *App) Done() <-chan struct{}
func (a *App) Address() string
func (a *App) Status() Status
func (a *App) Wait() error
func (a *App) Shutdown(ctx context.Context) error
```

- Preserves: `ocr.BuiltinWorkerArgument` is handled before flag parsing, `--config` defaults to `configs/config.yaml`, and `--version` prints `buildinfo.String()`.

- [ ] **Step 1: Add failing lifecycle tests**

Create tests that write a temporary valid config with `server.listen: 127.0.0.1:0`, call `Start`, wait on `Ready`, assert `/healthz` returns `200`, call `Shutdown` twice, and verify a fresh listener can bind `App.Address()`. Add a second test whose injected `Listen` returns `syscall.EADDRINUSE` and assert `errors.As(err, *StartupError)` with `Stage == "listen"`.

```go
func TestStartServesHealthAndShutdownIsIdempotent(t *testing.T) {
	app, err := Start(context.Background(), Options{ConfigPath: writeTestConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	<-app.Ready()

	resp, err := http.Get("http://" + app.Address() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", app.Address())
	if err != nil {
		t.Fatalf("address was not released: %v", err)
	}
	listener.Close()
}
```

- [ ] **Step 2: Run the targeted package test and confirm the package is absent**

Run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./internal/gatewayapp -v'
```

Expected: failure because `internal/gatewayapp` and its exported lifecycle do not exist.

- [ ] **Step 3: Implement the lifecycle with ownership-safe cleanup**

Move config loading, validation, SQLite opening, retention, metrics, telemetry, runtime construction, listener binding, and HTTP serving from `cmd/vibe-proxy/main.go` into `gatewayapp.Start`. Bind through `options.Listen`, defaulting to `net.Listen`; set `StateRunning` and close `ready` only after the listener exists. Use `sync.Once` for shutdown, call `http.Server.Shutdown`, wait for `Serve`, then close SQLite and the `done` channel. Convert config, validation, database, and listener failures to `StartupError` stages `config`, `validate`, `database`, and `listen`.

The CLI becomes a signal-aware wrapper:

```go
signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

app, err := gatewayapp.Start(signalCtx, gatewayapp.Options{ConfigPath: *cfgPath})
if err != nil {
	log.Fatal(err)
}
log.Printf("vibe-proxy listening on %s", app.Address())

select {
case <-signalCtx.Done():
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
case <-app.Done():
	if err := app.Wait(); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 4: Run lifecycle, root regression, and race-safe static checks**

Run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./internal/gatewayapp ./cmd/vibe-proxy && /usr/local/go/bin/go test ./... && /usr/local/go/bin/go vet ./...'
```

Expected: all commands exit `0`; the existing runtime, OCR, frontend embed, and CLI tests remain green.

- [ ] **Step 5: Commit the shared lifecycle**

```bash
git add internal/gatewayapp cmd/vibe-proxy/main.go
git commit -m "refactor: share gateway application lifecycle"
```

---

### Task 2: Add One-Time Desktop Admin Sessions

**Files:**
- Create: `internal/auth/desktop_session.go`
- Create: `internal/auth/desktop_session_test.go`
- Create: `internal/runtime/options.go`
- Create: `internal/runtime/desktop_handlers.go`
- Create: `internal/runtime/desktop_handlers_test.go`
- Modify: `internal/runtime/server.go`
- Modify: `internal/runtime/admin_handlers.go`
- Modify: every runtime handler still calling `auth.AuthorizeAdmin` directly

**Interfaces:**
- Produces:

```go
const DesktopSessionCookie = "vibe_desktop_session"

type DesktopSessionStore struct {
	// private mutex, clock, nonce expiry, nonces, and sessions
}

func NewDesktopSessionStore(now func() time.Time, nonceTTL time.Duration) *DesktopSessionStore
func (s *DesktopSessionStore) NewBootstrapNonce() (string, error)
func (s *DesktopSessionStore) ConsumeBootstrap(nonce string) (session string, ok bool)
func (s *DesktopSessionStore) Authorize(r *http.Request) bool
func (s *DesktopSessionStore) RevokeAll()
```

```go
package runtime

type Options struct {
	AdminTokenOverride string
	DesktopSessions    *auth.DesktopSessionStore
	DesktopController  desktopbridge.Controller
}

func NewWithOptions(
	cfgPath string,
	cfg *config.RuntimeConfig,
	sink telemetry.EventSink,
	prom *metrics.Prometheus,
	options Options,
) *Server
```

- `runtime.New` remains a compatibility wrapper that calls `NewWithOptions` with zero options.
- `gatewayapp.Options.Runtime` passes the desktop-only dependencies without making `gatewayapp` Wails-aware.

- [ ] **Step 1: Add failing nonce, cookie, and origin tests**

Test that a nonce is URL-safe, succeeds once, fails on reuse, and fails after the injected clock passes 60 seconds. Test that the returned session authorizes a request carrying `vibe_desktop_session`, and `RevokeAll` rejects it.

At runtime level, test:

1. `GET /desktop/bootstrap/{nonce}` returns `303`, `Location: /`, and an `HttpOnly; SameSite=Strict; Path=/` cookie.
2. `GET /admin/config/snapshot` accepts that cookie.
3. `POST /admin/config/reload` rejects cookie auth without `Origin`.
4. The same POST accepts an `Origin` equal to `http://` plus `r.Host`.
5. Bearer auth still accepts state-changing requests without `Origin`.
6. CLI mode returns `404` for `/desktop/bootstrap/...`.

- [ ] **Step 2: Run targeted tests and confirm the missing types and route failures**

Run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./internal/auth ./internal/runtime -run "Desktop|Bootstrap|AdminCookie" -v'
```

Expected: compilation or assertion failure because desktop sessions and routes are not implemented.

- [ ] **Step 3: Implement cryptographic single-use bootstrap state**

Generate nonce and session values from 32 random bytes encoded with `base64.RawURLEncoding`. Store SHA-256 digests rather than raw values in the maps. Delete expired nonces while holding the mutex, consume the matching nonce before returning a session, and keep sessions process-local with no persistent expiry. Use `subtle.ConstantTimeCompare` when comparing digests.

- [ ] **Step 4: Centralise runtime admin authorization**

Store the admin-token override, session store, and controller on `runtime.Server`. `buildSnapshot` uses the override when non-empty and otherwise reads the configured environment variable.

Register `/desktop/bootstrap/` only when `DesktopSessions` is non-nil. The bootstrap handler consumes the suffix, sets a session cookie without `Max-Age` or `Expires`, and redirects with `303`.

Replace every direct runtime call to `auth.AuthorizeAdmin` with `s.adminAuthorize`. Its decision order is:

```go
if auth.AuthorizeAdmin(r, snap.AdminToken) {
	return true
}
if s.desktopSessions == nil || !s.desktopSessions.Authorize(r) {
	writeUnauthorized(w)
	return false
}
if isSafeMethod(r.Method) || sameOrigin(r) {
	return true
}
writeForbidden(w, "desktop session requires same-origin request")
return false
```

`sameOrigin` parses `Origin`, rejects missing, opaque, malformed, or user-info-bearing origins, and compares `origin.Host` to `r.Host` case-insensitively. It accepts only `http` or `https`.

- [ ] **Step 5: Run targeted and full root verification**

Run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '
    /usr/local/go/bin/go test ./internal/auth ./internal/runtime -run "Desktop|Bootstrap|AdminCookie" -v &&
    ! grep -R "auth.AuthorizeAdmin" internal/runtime --include="*.go" | grep -v "admin_handlers.go" &&
    /usr/local/go/bin/go test ./... &&
    /usr/local/go/bin/go vet ./...
  '
```

Expected: all tests pass; the grep pipeline finds no bypass outside the central helper.

- [ ] **Step 6: Commit desktop session authentication**

```bash
git add internal/auth internal/runtime internal/gatewayapp
git commit -m "feat: add desktop admin session bootstrap"
```

---

### Task 3: Define the Allow-Listed Desktop Control Contract

**Files:**
- Create: `internal/desktopbridge/controller.go`
- Modify: `internal/runtime/options.go`
- Modify: `internal/runtime/desktop_handlers.go`
- Modify: `internal/runtime/desktop_handlers_test.go`
- Modify: `internal/runtime/server.go`
- Modify: `internal/gatewayapp/options.go`

**Interfaces:**
- Produces:

```go
package desktopbridge

type CloseBehavior string

const (
	CloseAsk  CloseBehavior = "ask"
	CloseTray CloseBehavior = "tray"
	CloseQuit CloseBehavior = "quit"
)

type Snapshot struct {
	Available     bool          `json:"available"`
	Platform      string        `json:"platform"`
	CloseBehavior CloseBehavior `json:"close_behavior"`
	ListenAddress string        `json:"listen_address"`
	DataDir       string        `json:"data_dir"`
	LogDir        string        `json:"log_dir"`
	OwnsGateway   bool          `json:"owns_gateway"`
}

type ImportResult struct {
	Imported bool   `json:"imported"`
	Path     string `json:"path,omitempty"`
}

type Controller interface {
	Snapshot() Snapshot
	SetCloseBehavior(CloseBehavior) error
	OpenDataDir() error
	ImportConfig(context.Context) (ImportResult, error)
}
```

- Produces authenticated routes:
  - `GET /admin/desktop`
  - `PUT /admin/desktop/preferences`
  - `POST /admin/desktop/open-data-dir`
  - `POST /admin/desktop/import-config`

- [ ] **Step 1: Add failing controller route tests with a fake**

Implement a test-only fake recording `SetCloseBehavior`, `OpenDataDir`, and `ImportConfig` calls. Assert CLI mode returns:

```json
{"available":false}
```

Assert desktop mode returns the fake snapshot, rejects `"close_behavior":"delete-files"`, persists `"tray"`, invokes open and import exactly once, and maps controller errors to `500` JSON without exposing local file contents.

- [ ] **Step 2: Run the route tests and confirm they fail**

Run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./internal/runtime -run "DesktopController|DesktopPreferences|DesktopImport" -v'
```

Expected: failure because the contract and four routes do not exist.

- [ ] **Step 3: Implement method-exact handlers**

Register all four routes in CLI and desktop modes so the frontend can detect availability. Apply `s.adminAuthorize` before returning path information. Enforce the exact HTTP methods and close-behavior enum. Return `409` for native actions when no controller exists, and use the existing JSON helpers for every response.

After a successful config import, reload the config through the same validated runtime path used by `/admin/config/reload`; return the new `loaded_at` timestamp with the import result. Do not let the desktop controller mutate runtime state directly.

- [ ] **Step 4: Verify the control contract and root regressions**

Run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./internal/runtime ./internal/gatewayapp && /usr/local/go/bin/go test ./...'
```

Expected: exit `0`.

- [ ] **Step 5: Commit the desktop bridge**

```bash
git add internal/desktopbridge internal/runtime internal/gatewayapp
git commit -m "feat: expose allow-listed desktop controls"
```

---

### Task 4: Make Settings Desktop-Aware and Cookie-Aware

**Files:**
- Create: `frontend/src/components/desktop-settings.tsx`
- Create: `frontend/scripts/check-desktop-settings.mjs`
- Modify: `frontend/src/lib/types.ts`
- Modify: `frontend/src/lib/api.ts`
- Modify: `frontend/src/pages/settings.tsx`
- Modify: `frontend/src/locales/en.json`
- Modify: `frontend/src/locales/zh-CN.json`
- Modify: `frontend/package.json`
- Rebuild: `internal/runtime/web/dist/`

**Interfaces:**
- Consumes: the four Task 3 routes.
- Produces:

```ts
export type CloseBehavior = "ask" | "tray" | "quit"

export interface DesktopSnapshot {
  available: boolean
  platform?: "windows" | "darwin"
  close_behavior?: CloseBehavior
  listen_address?: string
  data_dir?: string
  log_dir?: string
  owns_gateway?: boolean
}

export const desktopApi = {
  snapshot: () => request<DesktopSnapshot>("/admin/desktop"),
  setCloseBehavior: (close_behavior: CloseBehavior) =>
    request<DesktopSnapshot>("/admin/desktop/preferences", {
      method: "PUT",
      body: JSON.stringify({ close_behavior }),
    }),
  openDataDir: () =>
    request<{ ok: boolean }>("/admin/desktop/open-data-dir", { method: "POST" }),
  importConfig: () =>
    request<{ imported: boolean; path?: string; loaded_at?: string }>(
      "/admin/desktop/import-config",
      { method: "POST" }
    ),
}
```

- [ ] **Step 1: Add a failing static frontend contract check**

The checker reads source files and asserts:

- `request` passes `credentials: "same-origin"` to `fetch`;
- Settings does not gate model-catalog loading on `getToken()`;
- `DesktopSettings` renders only when `desktop.available`;
- all three close values are present;
- config import and open-directory actions call `desktopApi`;
- both locale files contain identical `settings.desktop` key trees.

- [ ] **Step 2: Run the frontend check and confirm failure**

Run:

```bash
cd frontend
node scripts/check-desktop-settings.mjs
```

Expected: failure because the desktop API and component are absent.

- [ ] **Step 3: Implement cookie-aware requests and the desktop settings card**

Set `credentials: "same-origin"` explicitly on every admin request while retaining the conditional Bearer header. Remove the `if (!getToken()) return` guard from model-catalog status loading.

Load `/admin/desktop` on Settings mount. In desktop mode:

- replace the editable admin-token card with a read-only “Authenticated by desktop application” status;
- render close behavior as `Ask when closing`, `Minimise to tray`, and `Exit application`;
- render application-data and log paths as selectable text;
- provide `Open application data` and `Import config.yaml` buttons;
- reload the catalog and desktop snapshot after a successful import;
- show localized success, cancel, validation-failure, and native-operation-failure toasts.

In browser mode, preserve the current Bearer token card without visual or behavioral changes.

- [ ] **Step 4: Add complete Simplified Chinese and English strings**

Use the same key set in both locales:

```json
{
  "settings": {
    "desktop": {
      "title": "Desktop Application",
      "description": "Native window, tray and local application data",
      "authenticated": "Authenticated by the desktop application",
      "closeBehavior": "When closing the window",
      "ask": "Ask every time",
      "tray": "Minimise to tray",
      "quit": "Exit application",
      "dataDirectory": "Application data",
      "logsDirectory": "Logs",
      "openData": "Open application data",
      "importConfig": "Import config.yaml",
      "imported": "Configuration imported and reloaded",
      "importCancelled": "Import cancelled",
      "operationFailed": "Desktop operation failed: {{error}}"
    }
  }
}
```

Translate every value naturally in `zh-CN.json`; do not copy English values into the Chinese file.

- [ ] **Step 5: Run frontend tests, build, and embedded-bundle verification**

Run:

```bash
cd frontend
npm test
npm run build
cd ..
git diff --exit-code -- internal/runtime/web/dist || {
  echo "Embedded bundle changed as expected; stage it with this task."
}
```

Expected: all frontend checks and TypeScript build pass; the final diff is limited to source and the rebuilt embedded bundle.

- [ ] **Step 6: Commit desktop-aware Settings**

```bash
git add frontend internal/runtime/web/dist
git commit -m "feat: add desktop application settings"
```

---

### Task 5: Create the Isolated Desktop Module, Paths, and Preferences

**Files:**
- Create: `desktop/go.mod`
- Create: `desktop/go.sum`
- Create: `desktop/tools.go`
- Create: `desktop/app/paths.go`
- Create: `desktop/app/paths_darwin.go`
- Create: `desktop/app/paths_windows.go`
- Create: `desktop/app/paths_test.go`
- Create: `desktop/app/preferences.go`
- Create: `desktop/app/preferences_test.go`
- Modify: `.gitignore`

**Interfaces:**
- Produces:

```go
type Paths struct {
	DataDir         string
	ConfigPath      string
	DatabasePath    string
	PreferencesPath string
	LogDir          string
	LogPath         string
}

func ResolvePaths(goos string, getenv func(string) string, userHome func() (string, error)) (Paths, error)
func PlatformPaths() (Paths, error)

type WindowPreferences struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Preferences struct {
	CloseBehavior desktopbridge.CloseBehavior `json:"closeBehavior"`
	Window        WindowPreferences            `json:"window"`
}

func DefaultPreferences() Preferences
func LoadPreferences(path string) (Preferences, error)
func SavePreferences(path string, preferences Preferences) error
```

- Exact nested module requirements:

```go
module github.com/a448582655/vibe-proxy/desktop

go 1.25.0

require (
	github.com/a448582655/vibe-proxy v0.0.0
	github.com/wailsapp/wails/v3 v3.0.0-alpha2.117
)

replace github.com/a448582655/vibe-proxy => ..
```

`desktop/tools.go` keeps the CLI in the module graph without shipping it in the
application:

```go
//go:build tools

package tools

import _ "github.com/wailsapp/wails/v3/cmd/wails3"
```

- [ ] **Step 1: Add failing path and preference tests**

Test these exact path cases:

| OS | Input | Expected data directory |
|---|---|---|
| `windows` | `LOCALAPPDATA=C:\Users\A\AppData\Local` | `C:\Users\A\AppData\Local\vibe-proxy` |
| `darwin` | home `/Users/a` | `/Users/a/Library/Application Support/vibe-proxy` |
| `windows` | missing `LOCALAPPDATA` | error containing `LOCALAPPDATA` |
| `linux` | any | error containing `unsupported desktop platform` |

Test preference defaults `ask`, `1280x800`; atomic `0600` save; unknown JSON fields ignored; invalid close behavior and window sizes replaced with defaults; corrupt JSON renamed with `"desktop.json.corrupt-" + now.UTC().Format("20060102T150405.000000000Z")` and defaults returned.

- [ ] **Step 2: Run desktop domain tests and confirm failure**

Run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src/desktop \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./app -run "Paths|Preferences" -v'
```

Expected: compilation failure because the desktop module and domain package are absent.

- [ ] **Step 3: Initialise the nested module with the exact Wails pin**

Write `desktop/go.mod` exactly as declared, run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src/desktop \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -e GOPROXY=https://proxy.golang.org,direct \
  golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go mod tidy'
```

If the primary proxy is unreachable from the current network, rerun the same command with `GOPROXY=https://goproxy.cn,direct`; commit only `go.mod` and the resulting checksums, never a machine-specific proxy setting.

- [ ] **Step 4: Implement deterministic paths and atomic preferences**

`ResolvePaths` creates no directories and is fully injectable for tests.
Darwin paths use `filepath.Join`. Windows paths trim trailing `\` or `/` from
`LOCALAPPDATA` and append backslash-separated names so their tests are stable
even when they run in a Linux container. `PlatformPaths` delegates with
`runtime.GOOS`, `os.Getenv`, and `os.UserHomeDir`.

`SavePreferences` validates the enum and clamps window dimensions to minimum `960x640`, creates the parent directory with `0700`, writes a sibling temporary file with `0600`, calls `Sync`, closes, then renames it. `LoadPreferences` decodes with unknown fields allowed; corrupt JSON is backed up once and replaced with defaults.

- [ ] **Step 5: Verify domain tests and module isolation**

Run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src/desktop \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go mod verify && /usr/local/go/bin/go test ./app -run "Paths|Preferences" -v'

! grep -q 'wailsapp/wails' go.mod
grep -F 'github.com/wailsapp/wails/v3 v3.0.0-alpha2.117' desktop/go.mod
```

Expected: tests pass, the root module contains no Wails dependency, and the exact pin is present once in the desktop module.

- [ ] **Step 6: Commit the desktop foundation**

```bash
git add desktop .gitignore
git commit -m "feat: add isolated desktop module preferences"
```

---

### Task 6: Implement the Platform-Neutral Desktop Host

**Files:**
- Create: `desktop/app/ports.go`
- Create: `desktop/app/close.go`
- Create: `desktop/app/close_test.go`
- Create: `desktop/app/controller.go`
- Create: `desktop/app/controller_test.go`
- Create: `desktop/app/host.go`
- Create: `desktop/app/host_test.go`
- Modify: `desktop/app/preferences.go`

**Interfaces:**
- Produces host ports independent of Wails:

```go
type Window interface {
	Show()
	Hide()
	Focus()
	Navigate(string)
}

type DialogChoice string

const (
	ChoiceTray   DialogChoice = "tray"
	ChoiceQuit   DialogChoice = "quit"
	ChoiceCancel DialogChoice = "cancel"
)

type Dialogs interface {
	AskClose(context.Context) (DialogChoice, error)
	ShowStartupError(context.Context, StartupPresentation) (RecoveryChoice, error)
	SelectConfig(context.Context) (string, bool, error)
}

type Tray interface {
	SetStatus(string)
	Destroy()
}

type System interface {
	CopyText(string) error
	OpenBrowser(string) error
	OpenDirectory(string) error
}

type Application interface {
	Quit()
}
```

```go
type HostOptions struct {
	Paths       Paths
	Preferences Preferences
	Window      Window
	Dialogs     Dialogs
	Tray        Tray
	System      System
	Application Application
	StartGateway func(context.Context, gatewayapp.Options) (*gatewayapp.App, error)
}

func NewHost(options HostOptions) (*Host, error)
func (h *Host) Start(context.Context) error
func (h *Host) HandleWindowClose(context.Context)
func (h *Host) RequestQuit(context.Context)
func (h *Host) OpenControlPlane()
func (h *Host) CopyOpenAIBaseURL() error
func (h *Host) CopyAnthropicBaseURL() error
func (h *Host) ResetCloseBehavior() error
func (h *Host) Shutdown(context.Context) error
```

- [ ] **Step 1: Add failing close-state tests**

Use fakes to assert:

- saved `tray` hides the window without opening a dialog;
- saved `quit` starts graceful shutdown and then calls `Application.Quit`;
- saved `ask` with `ChoiceTray` saves `tray` before hiding;
- saved `ask` with `ChoiceQuit` saves `quit` before quitting;
- `ChoiceCancel` leaves the window visible and keeps `ask`;
- only one close dialog can be active;
- `RequestQuit` bypasses the close preference;
- `ResetCloseBehavior` saves `ask`;
- shutdown executes once even when called concurrently.

- [ ] **Step 2: Add failing host startup and controller tests**

Assert `Host.Start`:

1. creates a minimal valid config when absent;
2. configures its SQLite path as the resolved desktop database;
3. generates an in-memory admin token and desktop session store;
4. waits for gateway readiness;
5. creates one bootstrap nonce;
6. navigates the window to `"http://" + gateway.Address() + "/desktop/bootstrap/" + nonce`;
7. exposes `OwnsGateway: true` through the controller;
8. never writes the token or nonce to `desktop.json`, `config.yaml`, or logs.

Test controller import with a selected valid YAML file: copy to a sibling temporary file, validate through `config.LoadRuntime` and `config.ValidateRuntime`, atomically replace `config.yaml`, and return the source path. Invalid YAML must leave the original bytes unchanged; file-picker cancel returns `Imported:false`.

- [ ] **Step 3: Run host domain tests and confirm failure**

Run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src/desktop \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./app -run "Close|Host|Controller|Import" -v'
```

Expected: compilation failure because the host ports and state machine do not exist.

- [ ] **Step 4: Implement the close state machine and controller**

Protect close handling with a mutex and `dialogOpen` flag. Set `quitting` before shutdown so programmatic window destruction cannot reopen the question dialog. Persist a user’s close choice before applying it. Give shutdown a 10-second timeout and call `Application.Quit` only after `gatewayapp.Shutdown` returns.

The controller implements only `desktopbridge.Controller`; it cannot execute arbitrary paths or commands. `OpenDataDir` always opens the resolved data directory. `ImportConfig` accepts only the native file-picker result, validates before replacement, preserves `0600`, and returns errors without source file contents.

- [ ] **Step 5: Implement first-run config and bootstrap startup**

Create a minimal config by marshalling the existing config types:

```go
bootstrap := config.SimpleConfig{
	Version:    "vibeproxy.io/v1alpha1",
	Server:     config.ServerConfig{Listen: "127.0.0.1:8080"},
	Storage:    config.StorageConfig{SQLitePath: paths.DatabasePath, RetentionDays: 14},
	ClientKeys: []config.ClientKeyConfig{},
	Providers:  map[string]config.ProviderConfig{},
	Models: config.ModelsConfig{
		Default:  "",
		AllowRaw: true,
		Aliases:  map[string]string{},
	},
	AgentProfiles: map[string]config.AgentProfileConfig{},
	Routes:        []config.AdvancedRouteConfig{},
}
data, err := yaml.Marshal(bootstrap)
```

Set `gatewayapp.Options.Runtime.AdminTokenOverride` to a 32-byte random token, pass a fresh desktop session store and controller, and discard the raw token after runtime construction. Update the tray status from `Starting` to `"Running on " + gateway.Address()` or the structured startup error.

- [ ] **Step 6: Verify all desktop domain and root tests**

Run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src/desktop \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./app -v'

docker run --rm \
  -v "$PWD":/src -w /src \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./...'
```

Expected: both module test suites pass.

- [ ] **Step 7: Commit the testable desktop host**

```bash
git add desktop/app
git commit -m "feat: implement desktop host lifecycle"
```

---

### Task 7: Bind the Host to Wails Window, Tray, Dialog, and Single Instance APIs

**Files:**
- Create: `desktop/main.go`
- Create: `desktop/wailsapp/contract.go`
- Create: `desktop/wailsapp/application.go`
- Create: `desktop/wailsapp/dialogs.go`
- Create: `desktop/wailsapp/tray.go`
- Create: `desktop/wailsapp/system.go`
- Create: `desktop/assets/appicon.svg`
- Create: `desktop/assets/tray.png`
- Create: `desktop/assets/tray-template.png`
- Create: `desktop/wailsapp/assets.go`
- Create: `desktop/wailsapp/adapter_test.go`

**Interfaces:**
- Consumes: Task 6 `Host` and ports.
- Produces: one Wails desktop process with unique ID `io.vibeproxy.desktop`.

- [ ] **Step 1: Add adapter contract tests**

Keep tests free of native windows by testing `contract.go`, whose data structures
describe the window, platform quit behavior, unique instance ID, and tray item
order without importing Wails. Add `//go:build windows || darwin` to the files
that import Wails so Linux-container tests compile only the contract. Assert:

- the main window is named `main`, titled `Vibe Proxy`, starts at `1280x800`, and has minimum `960x640`;
- Windows sets `DisableQuitOnLastWindowClosed: true`;
- macOS sets `ApplicationShouldTerminateAfterLastWindowClosed: false`;
- the second-instance action is `show`, then `focus`;
- the close hook contract is cancellable and delegates to the host;
- the tray exit action maps to `request_quit`, not direct process quit;
- the shutdown sequence is gateway, tray destroy, application quit.

- [ ] **Step 2: Run the adapter test and confirm failure**

Run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src/desktop \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./wailsapp -run Adapter -v'
```

Expected: compilation failure because the Wails adapter does not exist.

- [ ] **Step 3: Implement early headless modes in `desktop/main.go`**

Before `application.New`, handle:

```go
if len(os.Args) == 2 && os.Args[1] == ocr.BuiltinWorkerArgument {
	if err := ocr.RunBuiltinWorker(context.Background(), os.Stdin, os.Stdout); err != nil {
		os.Exit(1)
	}
	return
}
if slices.Contains(os.Args[1:], "--version") {
	fmt.Fprintln(os.Stdout, buildinfo.String())
	return
}
```

This keeps `--version` and the self-launched OCR worker usable without WebView2 or WKWebView initialization.

- [ ] **Step 4: Implement Wails application and window integration**

Create the application with:

```go
app := application.New(application.Options{
	Name: "Vibe Proxy",
	SingleInstance: &application.SingleInstanceOptions{
		UniqueID: "io.vibeproxy.desktop",
		OnSecondInstanceLaunch: func(application.SecondInstanceData) {
			mainWindow.Show()
			mainWindow.Focus()
		},
	},
	Windows: application.WindowsOptions{
		DisableQuitOnLastWindowClosed: true,
	},
	Mac: application.MacOptions{
		ApplicationShouldTerminateAfterLastWindowClosed: false,
	},
})
```

Create a single `application.WebviewWindowOptions` window with the tested dimensions. Register `events.Common.WindowClosing`, call `event.Cancel()`, and delegate asynchronously so the native callback is not blocked by a dialog. Run the host after the Wails application is initialized, then show and focus only after gateway readiness and bootstrap navigation.

- [ ] **Step 5: Implement native dialogs, tray, clipboard, and openers**

Use Wails question dialogs with exact close buttons:

- `Minimise to tray and keep proxy running`
- `Exit vibe-proxy`
- `Cancel`

Use `app.Dialog.OpenFile().SetTitle("Import vibe-proxy config.yaml").AddFilter("YAML configuration", "*.yaml;*.yml").PromptForSingleSelection()` for import.

Build the tray menu in this order:

1. `Open Control Plane`
2. disabled dynamic status
3. separator
4. `Copy OpenAI Base URL`
5. `Copy Anthropic Base URL`
6. `Open Logs Folder`
7. `Open in Browser`
8. separator
9. `Reset Close Behavior`
10. `Exit vibe-proxy`

Call `menu.Update()` after status changes. Use `SetTemplateIcon` on macOS and the colored icon on Windows. Left-click opens and focuses the window. Destroy the tray in the Wails shutdown hook after `Host.Shutdown`.

- [ ] **Step 6: Generate and inspect icon assets**

Export `desktop/assets/appicon.svg` to a `1024x1024` PNG, then let the exact pinned Wails generator produce Windows `.ico` and macOS `.icns` resources. Verify:

```bash
file desktop/assets/tray.png desktop/assets/tray-template.png desktop/build/appicon.png
sips -g pixelWidth -g pixelHeight desktop/build/appicon.png
```

Expected: all are PNG images; the application icon reports `1024` for both dimensions.

- [ ] **Step 7: Run pure adapter tests and a native macOS development build**

Run pure tests:

```bash
docker run --rm \
  -v "$PWD":/src -w /src/desktop \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./app ./wailsapp -v'
```

Then, on the current macOS host with Go 1.26.5 and the exact Wails CLI:

```bash
cd desktop
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117 build GOOS=darwin GOARCH="$(go env GOARCH)"
./bin/vibe-proxy-desktop --version
cd ..
```

Expected: native build exits `0`; `--version` prints `vibe-proxy` build information without opening a window.

- [ ] **Step 8: Commit the Wails desktop shell**

```bash
git add desktop
git commit -m "feat: add Wails desktop application shell"
```

---

### Task 8: Add Native Startup Recovery and Browser Fallback

**Files:**
- Modify: `desktop/app/host.go`
- Modify: `desktop/app/host_test.go`
- Modify: `desktop/wailsapp/dialogs.go`
- Modify: `desktop/wailsapp/application.go`
- Modify: `desktop/wailsapp/tray.go`
- Create: `desktop/app/recovery.go`
- Create: `desktop/app/recovery_test.go`

**Interfaces:**
- Produces:

```go
type RecoveryKind string

const (
	RecoveryInvalidConfig RecoveryKind = "invalid_config"
	RecoveryPortConflict  RecoveryKind = "port_conflict"
	RecoveryDatabase      RecoveryKind = "database"
	RecoveryWebView       RecoveryKind = "webview"
)

type RecoveryChoice string

const (
	RecoveryOpenExisting RecoveryChoice = "open_existing"
	RecoveryOpenData     RecoveryChoice = "open_data"
	RecoveryExit         RecoveryChoice = "exit"
)

type StartupPresentation struct {
	Kind    RecoveryKind
	Title   string
	Summary string
	Address string
}
```

- [ ] **Step 1: Add failing recovery decision tests**

Map `gatewayapp.StartupError.Stage` deterministically:

| Stage | Kind | Actions |
|---|---|---|
| `config`, `validate` | `invalid_config` | open data, exit |
| `listen` with address already serving a vibe-proxy `/healthz` | `port_conflict` | open existing, open data, exit |
| `listen` with unrelated or unreachable service | `port_conflict` | open data, exit |
| `database` | `database` | open data, exit |

Assert “open existing” launches the existing control plane, marks `OwnsGateway:false`, does not create a gateway session cookie, and never shuts down the external server. Assert WebView creation/navigation failure keeps the gateway and tray running and exposes browser/log actions.

- [ ] **Step 2: Run recovery tests and confirm failure**

Run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src/desktop \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./app -run "Recovery|PortConflict|WebViewFailure" -v'
```

Expected: failure because recovery classification and ownership handling are absent.

- [ ] **Step 3: Implement structured native recovery**

Write every startup error to `logs/vibe-proxy.log` with UTC timestamp, stage, address, and wrapped error. Native dialogs show a concise localized summary but never provider secrets, API keys, config contents, or SQL data.

Probe an occupied address with a two-second `GET /healthz`; identify vibe-proxy only when status is `200` and JSON contains `ok`, `started_at`, and `loaded_at`. The native recovery action is labelled **Open Existing Control Plane**. It uses the external browser because the desktop session belongs only to gateways started by this process.

For config and database failures, keep only the native dialog alive. For WebView failure after a healthy gateway start, keep the tray and gateway alive, set the status line to `Running; desktop window unavailable`, and make browser/log actions operational.

- [ ] **Step 4: Verify recovery, host, and root regression tests**

Run:

```bash
docker run --rm \
  -v "$PWD":/src -w /src/desktop \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./app -v'

docker run --rm \
  -v "$PWD":/src -w /src \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go test ./...'
```

Expected: exit `0`.

- [ ] **Step 5: Commit native recovery behavior**

```bash
git add desktop/app desktop/wailsapp
git commit -m "feat: add desktop startup recovery"
```

---

### Task 9: Package Windows and macOS Desktop Applications

**Files:**
- Create: `desktop/Taskfile.yml`
- Create: `desktop/build/config.yml`
- Create: `desktop/build/Taskfile.yml`
- Create: `desktop/build/windows/Taskfile.yml`
- Create: `desktop/build/windows/info.json`
- Create: `desktop/build/windows/nsis/project.nsi`
- Create: `desktop/build/darwin/Taskfile.yml`
- Create: `desktop/build/darwin/Info.plist`
- Create: `scripts/release/build-desktop.sh`
- Create: `scripts/release/package-desktop-portable.ps1`
- Create: `scripts/release/package-desktop-dmg.sh`
- Create: `scripts/release/test-desktop-packaging.sh`
- Modify: `scripts/release/verify-assets.sh`
- Modify: `scripts/release/test-packaging.sh`

**Interfaces:**
- Produces exact release assets:

```text
vibe-proxy-desktop_VERSION_windows_amd64-setup.exe
vibe-proxy-desktop_VERSION_windows_arm64-setup.exe
vibe-proxy-desktop_VERSION_windows_amd64-portable.zip
vibe-proxy-desktop_VERSION_windows_arm64-portable.zip
vibe-proxy-desktop_VERSION_darwin_amd64.dmg
vibe-proxy-desktop_VERSION_darwin_arm64.dmg
```

- Existing ten CLI package assets remain unchanged; `verify-assets.sh` expects sixteen package files before `SHA256SUMS` is generated.

- [ ] **Step 1: Add failing packaging contract tests**

`test-desktop-packaging.sh` checks:

- exact names for six desktop assets;
- Windows portable ZIP contains `vibe-proxy-desktop.exe`, `README.txt`, and `LICENSE`;
- NSIS project creates Start Menu and uninstall entries;
- NSIS uninstall does not delete `%LOCALAPPDATA%\vibe-proxy`;
- each DMG contains `Vibe Proxy.app` and an `Applications` link;
- each app bundle has `CFBundleIdentifier=io.vibeproxy.desktop`;
- architecture and version inspection commands reject mismatches;
- `verify-assets.sh` rejects missing, duplicate, or unexpected package files.

- [ ] **Step 2: Run packaging contracts and confirm failure**

Run:

```bash
bash scripts/release/test-desktop-packaging.sh
```

Expected: failure because desktop package scripts and manifest entries do not exist.

- [ ] **Step 3: Scaffold and normalize Wails build configuration**

Generate Wails v3 Taskfiles from `v3.0.0-alpha2.117`, then commit normalized files with:

- product name `Vibe Proxy`;
- binary name `vibe-proxy-desktop`;
- bundle identifier `io.vibeproxy.desktop`;
- build output under `desktop/bin`;
- build tags and linker values for `buildinfo.Version`, `buildinfo.Commit`, and `buildinfo.BuildDate`;
- Windows GUI subsystem flags so production launch has no console window;
- WebView2 bootstrapper enabled in NSIS;
- macOS app bundle architecture matching the requested runner architecture.

- [ ] **Step 4: Implement deterministic desktop packaging scripts**

`build-desktop.sh VERSION GOOS GOARCH OUTPUT_DIR` validates the semantic version, verifies the exact Wails line in `desktop/go.mod`, and runs:

```bash
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117 \
  build GOOS="$goos" GOARCH="$goarch" \
  VERSION="$version" COMMIT="$commit" BUILD_DATE="$build_date"
```

On Windows, package the NSIS setup executable and create the portable ZIP with PowerShell `Compress-Archive`. On macOS, package the `.app`, copy it into a read-only DMG staging directory beside an `Applications` symlink, and create a compressed DMG with `hdiutil`.

- [ ] **Step 5: Run script syntax and packaging contract checks**

Run:

```bash
bash -n scripts/release/build-desktop.sh
bash -n scripts/release/package-desktop-dmg.sh
bash scripts/release/test-desktop-packaging.sh
bash scripts/release/test-packaging.sh
```

Expected: all scripts and manifest tests exit `0`.

- [ ] **Step 6: Build and inspect the current macOS desktop package**

Run on macOS:

```bash
scripts/release/build-desktop.sh v0.1.0-desktop-test darwin "$(go env GOARCH)" "$PWD/dist-desktop-test"
scripts/release/package-desktop-dmg.sh \
  v0.1.0-desktop-test "$(go env GOARCH)" \
  "$PWD/desktop/bin/Vibe Proxy.app" "$PWD/dist-desktop-test"
hdiutil imageinfo "$PWD"/dist-desktop-test/*.dmg >/dev/null
"$PWD/desktop/bin/Vibe Proxy.app/Contents/MacOS/vibe-proxy-desktop" --version |
  grep -F 'vibe-proxy v0.1.0-desktop-test'
rm -rf "$PWD/dist-desktop-test"
```

Expected: build and DMG inspection succeed; the embedded version is exact.

- [ ] **Step 7: Commit desktop packaging**

```bash
git add desktop/build desktop/Taskfile.yml scripts/release
git commit -m "build: package desktop applications"
```

---

### Task 10: Add Native Desktop CI and Release Publication

**Files:**
- Create: `.github/workflows/desktop-build.yml`
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release.yml`
- Modify: `scripts/release/verify-assets.sh`

**Interfaces:**
- Uses `actions/checkout@v6`, `actions/setup-go@v6`, `actions/setup-node@v6`, `actions/upload-artifact@v7`, and `actions/download-artifact@v8`.
- Uses Go `1.26.5`, Node `22`, and the Wails version from the exact `desktop/go.mod` line.
- Produces six desktop artifacts consumed by the existing release `publish` job.

- [ ] **Step 1: Add failing workflow and manifest assertions**

Extend shell contract tests to parse the workflow files and assert:

- desktop module tests run on Ubuntu without building a native window;
- Windows builds run on `windows-latest` for `amd64` and `arm64`;
- macOS builds run on native macOS runners for `amd64` and `arm64`;
- every binary runs `--version`;
- Windows packages inspect PE architecture and NSIS contents;
- macOS packages inspect Mach-O architecture, bundle metadata, and DMG contents;
- the publish job needs both desktop jobs;
- all sixteen package assets are downloaded before checksum generation.

- [ ] **Step 2: Run release contract tests and confirm workflow assertions fail**

Run:

```bash
bash scripts/release/test-packaging.sh
bash scripts/release/test-desktop-packaging.sh
```

Expected: failure because native desktop CI jobs are absent.

- [ ] **Step 3: Implement branch/PR desktop build workflow**

`desktop-build.yml` runs for pull requests and pushes affecting `desktop/**`, `internal/gatewayapp/**`, `internal/runtime/**`, frontend sources, or release scripts.

The workflow:

1. runs root frontend and Go regressions on Ubuntu;
2. runs `go mod verify` and `go test ./app/...` in `desktop`;
3. builds Windows AMD64/ARM64 on `windows-latest`;
4. builds macOS ARM64 on `macos-14` and AMD64 on `macos-13`;
5. runs `--version` before packaging;
6. uploads packages for inspection but does not publish a GitHub Release.

- [ ] **Step 4: Extend the tag release workflow**

Add:

- `desktop-windows` matrix job for setup and portable packages;
- `desktop-macos` matrix job for app bundle and DMG packages;
- native package-content and architecture checks before upload;
- unique artifact names;
- `publish.needs: [portable, deb, dmg, desktop-windows, desktop-macos]`;
- upload through `actions/upload-artifact@v7`;
- aggregation through `actions/download-artifact@v8`;
- unchanged draft-first GitHub Release behavior;
- `--prerelease --latest=false` for desktop RC tags containing a hyphen.

- [ ] **Step 5: Validate workflow syntax and release contracts locally**

Run:

```bash
ruby -e '
  require "yaml"
  ARGV.each do |path|
    YAML.safe_load(File.read(path), aliases: true)
    puts path
  end
' \
  .github/workflows/ci.yml \
  .github/workflows/desktop-build.yml \
  .github/workflows/release.yml
bash scripts/release/test-packaging.sh
bash scripts/release/test-desktop-packaging.sh
```

Expected: three workflow paths print and both test scripts exit `0`.

- [ ] **Step 6: Commit native CI and release integration**

```bash
git add .github/workflows scripts/release
git commit -m "ci: build desktop release artifacts"
```

---

### Task 11: Document, Exercise, and Gate the Desktop Release Candidate

**Files:**
- Create: `docs/desktop-rc-checklist.md`
- Modify: `README.md`
- Modify: `docs/development.md`
- Modify: `docs/release.md`
- Modify: `docs/admin-api.md`

**Interfaces:**
- Documents desktop app, CLI fallback, Linux Web UI, admin bootstrap behavior, local paths, unsigned RC warnings, development commands, artifact names, and physical acceptance evidence.

- [ ] **Step 1: Add documentation contract checks**

Extend `scripts/release/test-desktop-packaging.sh` to assert the documentation contains:

- all six desktop artifact names;
- Windows `%LOCALAPPDATA%\vibe-proxy`;
- macOS `~/Library/Application Support/vibe-proxy`;
- Linux CLI plus browser workflow;
- first-close choice and reset instructions;
- config import, logs, and external-browser fallback;
- SmartScreen and Gatekeeper instructions for unsigned RCs;
- explicit statement that stable desktop publication requires signing/notarization or maintainer exception;
- Wails exact version and native build prerequisites.

- [ ] **Step 2: Run the documentation contract and confirm failure**

Run:

```bash
bash scripts/release/test-desktop-packaging.sh
```

Expected: failure listing missing documentation strings.

- [ ] **Step 3: Write user and contributor documentation**

README installation paths:

- Windows/macOS desktop app for users who want a native window and tray;
- Windows/macOS CLI packages for automation and terminal-first use;
- Linux binary or DEB plus `http://127.0.0.1:8080/` Web UI.

Development documentation includes exact Go, Node, Wails, WebView2, Xcode Command Line Tools, and NSIS prerequisites; root and nested-module test commands; native development commands; and the reason Docker cannot replace final native GUI packaging checks.

Admin API documentation covers the single-use bootstrap and four desktop routes, explicitly marking them unavailable or conflict-returning in CLI mode.

- [ ] **Step 4: Create the physical RC acceptance record**

Use unchecked boxes grouped by:

- Windows 10/11 AMD64 installer, portable app, no console, first-close choices, tray restore, preference persistence, text request, image/OCR/vision request, browser fallback, uninstall data preservation, and SmartScreen guidance;
- Windows ARM64 binary architecture, setup, tray, and request smoke checks;
- macOS Apple Silicon and Intel DMG mount, app launch, menu bar, close conventions, preference persistence, text and image requests, data/log paths, graceful quit, and Gatekeeper guidance.

Include fields for tester, hardware/OS version, artifact checksum, date, result, and issue links. The document must state that unchecked physical tests block a stable desktop release but do not block publishing an explicitly unsigned pre-release candidate.

- [ ] **Step 5: Run the complete repository verification gate**

Run frontend:

```bash
cd frontend
npm ci
npm test
npm run build
cd ..
git diff --exit-code -- internal/runtime/web/dist
```

Run root Go:

```bash
docker run --rm \
  -v "$PWD":/src -w /src \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go mod verify && /usr/local/go/bin/go test ./... && /usr/local/go/bin/go vet ./...'
```

Run desktop domain and release contracts:

```bash
docker run --rm \
  -v "$PWD":/src -w /src/desktop \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc '/usr/local/go/bin/go mod verify && /usr/local/go/bin/go test ./app/...'

bash scripts/release/test-packaging.sh
bash scripts/release/test-desktop-packaging.sh
git diff --check
```

Expected: every command exits `0`.

- [ ] **Step 6: Run the current-platform end-to-end desktop smoke test**

On macOS, launch the built `.app` with a temporary application-data directory override accepted only in development builds. Verify:

1. one native window opens without Terminal interaction;
2. `/healthz` and `/v1/models` are reachable on loopback;
3. Settings shows desktop authentication and the close selector;
4. the first close can minimise to tray;
5. tray restore works;
6. reset returns close behavior to `ask`;
7. explicit exit releases `127.0.0.1:8080`;
8. a second launch focuses the first instance;
9. `--version` and `ocr.BuiltinWorkerArgument` complete without opening the window.

Record the result in `docs/desktop-rc-checklist.md`.

- [ ] **Step 7: Review the change set against every stability gate**

Run:

```bash
git diff --stat origin/main...HEAD
git log --oneline origin/main..HEAD
if grep -R "github.com/wailsapp/wails" go.mod go.sum; then
  echo "Wails leaked into the root module." >&2
  exit 1
fi
grep -F "github.com/wailsapp/wails/v3 v3.0.0-alpha2.117" desktop/go.mod
git grep -nE 'vibe_desktop_session|desktop/bootstrap|same-origin|CloseBehavior'
```

Confirm all twelve stability gates in the approved design have code, tests, CI, documentation, or a recorded physical acceptance item.

- [ ] **Step 8: Commit documentation and acceptance infrastructure**

```bash
git add README.md docs scripts/release
git commit -m "docs: add desktop release candidate guide"
```

---

## Plan Completion Checks

Before implementation handoff:

1. Every approved design section maps to at least one task:
   - lifecycle: Tasks 1 and 6;
   - authentication: Task 2;
   - narrow native control plane: Tasks 3 and 4;
   - data paths and preferences: Task 5;
   - window, tray, close behavior, and single instance: Tasks 6 and 7;
   - startup recovery and browser fallback: Task 8;
   - packaging: Task 9;
   - CI and release: Task 10;
   - user guidance and physical acceptance: Task 11.
2. Interfaces use one shared `desktopbridge.CloseBehavior` enum with values `ask`, `tray`, and `quit`.
3. The root constructor remains backward-compatible through `runtime.New`.
4. The desktop WebView uses only a one-time nonce and process-local session cookie.
5. The CLI, Linux Web UI, raw data-plane API, OCR worker, and browser Bearer auth remain supported.
6. Each task has a failing test, focused implementation, verification command, and commit.
