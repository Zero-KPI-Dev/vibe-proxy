# vibe-proxy Windows and macOS Desktop GUI Design

**Date:** 2026-07-30

**Status:** Approved for implementation

**Target:** First desktop release candidate after `v0.1.0-rc.1`

## 1. Summary

vibe-proxy currently ships as a local HTTP gateway with an embedded React
control plane. Windows, macOS, and Linux users start the CLI binary and open the
control plane in a browser.

The desktop phase adds a native application host for Windows and macOS while
retaining the current React UI, Go gateway, local HTTP endpoints, CLI entrypoint,
and Linux Web UI workflow.

The desktop host will use Wails v3 and the operating system WebView. It will
provide:

- a normal application window;
- a system tray or macOS menu-bar item;
- single-instance behavior;
- automatic startup and graceful shutdown of the gateway;
- a first-close choice between exiting and continuing in the tray;
- desktop-specific settings and native error dialogs;
- platform-appropriate configuration, database, preference, and log paths;
- Windows installers and portable packages;
- macOS `.app` bundles distributed in DMGs.

The desktop host is an additional product surface, not a replacement for the
CLI or browser control plane.

## 2. Goals

1. Give Windows and macOS users a desktop application that can be launched
   without a terminal or manually opening a browser.
2. Reuse the existing React control plane instead of creating separate WinUI
   and SwiftUI interfaces.
3. Run the desktop host and gateway in one process.
4. Preserve the existing stable local API addresses:
   - `http://127.0.0.1:8080/v1`
   - `http://127.0.0.1:8080/anthropic`
5. Keep the root CLI module free from desktop build requirements.
6. Keep Linux behavior unchanged.
7. Adopt Wails v3 in a controlled manner with an exact version pin, native CI
   builds, and a fully supported CLI fallback.

## 3. Non-goals for the First Desktop Release

- Rewriting the control plane with native Windows or macOS controls.
- A Linux desktop application.
- Automatic updates.
- Start-at-login integration.
- Windows Store or Mac App Store distribution.
- Multiple desktop windows.
- Windows Authenticode signing automation.
- macOS Developer ID signing and notarization automation.
- Replacing the HTTP admin API with Wails-only IPC.
- Automatically changing the gateway port when `8080` is unavailable.

Start-at-login, signed updates, and store distribution remain compatible future
extensions.

## 4. Framework Decision

### 4.1 Selected: Wails v3

Wails v3 fits the current Go and React stack and provides native WebView
windows, system tray support, native dialogs, cancellable window-close hooks,
single-instance support, and native packaging.

Wails v3 is still an active pre-release. The project will therefore:

1. pin one exact tested Wails version in `desktop/go.mod`;
2. never use `@latest` in CI or release builds;
3. upgrade Wails only in isolated dependency pull requests;
4. require Windows and macOS package builds before accepting an upgrade;
5. keep Wails dependencies out of the root Go module;
6. keep the CLI release available as a supported fallback.

References:

- <https://v3.wails.io/concepts/architecture/>
- <https://v3.wails.io/faq/>
- <https://v3.wails.io/reference/menu/>
- <https://v3.wails.io/features/windows/events/>
- <https://v3.wails.io/reference/dialogs/>

### 4.2 Rejected: Tauri v2

Tauri is stable and has strong packaging support, but it would add a Rust
desktop host around the Go gateway. That creates two backend toolchains, a
sidecar process, process supervision, and cross-process update coordination.

### 4.3 Rejected: Electron

Electron would reuse the React UI but bundle Chromium and Node.js. Its package
size, memory footprint, and multiprocess runtime conflict with vibe-proxy's
lightweight local-tool positioning.

## 5. Repository and Module Structure

The desktop host uses a nested Go module so root CLI tests and pure-Go release
builds do not compile Wails:

```text
cmd/
  vibe-proxy/
    main.go

desktop/
  go.mod
  go.sum
  main.go
  app/
    host.go
    lifecycle.go
    preferences.go
    tray.go
  assets/
    icons/
  build/
    windows/
    darwin/

internal/
  gatewayapp/
    app.go
    options.go
    status.go
```

`desktop/go.mod` requires the root vibe-proxy module and uses a repository-local
replace directive during development and CI.

The root `internal/gatewayapp` package is not desktop-aware. It owns the reusable
gateway lifecycle and is imported by both entrypoints.

## 6. Shared Gateway Lifecycle

The current setup in `cmd/vibe-proxy/main.go` will move behind a reusable API:

```go
type Options struct {
    ConfigPath         string
    AdminTokenOverride string
    DesktopController  DesktopController
}

type App struct {
    // private lifecycle state
}

func Start(ctx context.Context, options Options) (*App, error)
func (a *App) Ready() <-chan struct{}
func (a *App) Status() Status
func (a *App) Shutdown(ctx context.Context) error
```

The lifecycle package will:

1. load and validate configuration;
2. open SQLite;
3. initialise retention, metrics, telemetry, OCR, routing, and HTTP handlers;
4. bind the listener before reporting readiness;
5. serve the existing routes;
6. expose structured startup errors;
7. gracefully stop HTTP traffic and close SQLite.

The CLI will add signal-aware graceful shutdown but otherwise retain its current
arguments, defaults, output, and exit behavior.

The built-in OCR worker argument must be checked before Wails initialisation in
both entrypoints. The desktop executable must remain capable of launching
itself as the OCR worker.

## 7. Desktop Startup Flow

```text
Launch desktop application
  -> handle --version or built-in OCR worker mode
  -> acquire desktop single-instance lock
  -> resolve platform application-data paths
  -> create first-run config and preferences if absent
  -> generate an in-memory admin token
  -> start gatewayapp on configured loopback address
  -> wait for Ready
  -> create one-time desktop admin session
  -> open WebView at the one-time bootstrap URL
  -> show main window and system tray
```

If another desktop instance is running, the second invocation will activate and
focus the existing window, then exit.

The WebView loads the same loopback control plane served by the gateway. It does
not load a duplicate frontend build and does not use a second API transport.
Streaming, image upload, OCR traces, provider administration, and observability
therefore retain their browser-tested behavior.

## 8. Desktop Authentication Bootstrap

The desktop control plane must not ask the user to enter
`VIBE_PROXY_ADMIN_TOKEN`, and it must not put the raw token in a URL or
localStorage.

Desktop mode will use this bootstrap flow:

1. `gatewayapp` accepts an in-memory admin-token override.
2. The desktop host creates a cryptographically random, single-use bootstrap
   nonce.
3. The WebView opens `/desktop/bootstrap/<nonce>`.
4. The server consumes the nonce, sets an ephemeral `HttpOnly`,
   `SameSite=Strict` admin-session cookie, and redirects to `/`.
5. Admin authentication accepts the desktop session cookie or the existing
   Bearer token.
6. Cookie-authenticated state-changing requests enforce a same-origin check.
7. The nonce cannot be reused and expires quickly.
8. Desktop admin sessions expire when the desktop process exits.

The normal browser control plane and CLI continue to use the existing Bearer
token. A tray action may copy the current temporary admin token for an explicit
browser fallback, but the desktop WebView does not persist it.

## 9. Application Data

Desktop mode does not depend on the process working directory.

### Windows

```text
%LOCALAPPDATA%\vibe-proxy\
```

### macOS

```text
~/Library/Application Support/vibe-proxy/
```

### Layout

```text
config.yaml
vibe-proxy.db
desktop.json
logs/
```

`desktop.json` stores non-secret host preferences:

```json
{
  "closeBehavior": "ask",
  "window": {
    "width": 1280,
    "height": 800
  }
}
```

The file is written atomically. Unknown fields are ignored to allow future
extensions. Corrupt preferences are backed up and replaced with defaults rather
than preventing gateway startup.

On first launch, desktop mode writes a minimal valid configuration. Provider
credentials and client keys continue to be managed by the existing control
plane. An import action lets the user select an existing `config.yaml` with a
native file picker; the imported file is validated before replacement.

## 10. Window and Close Behavior

The initial close behavior is `ask`.

On the first user-initiated close:

1. cancel the Wails `WindowClosing` event;
2. show a native question dialog attached to the main window;
3. offer:
   - **Minimise to tray and keep proxy running**
   - **Exit vibe-proxy**
   - **Cancel**
4. persist the first two choices;
5. execute the chosen action.

Subsequent closes use the saved behavior. The desktop section in Settings and a
tray menu action can reset the behavior to `ask`.

Programmatic application quit bypasses the window-close preference after the
user explicitly selects **Exit** from the tray or close dialog. It still performs
graceful gateway shutdown.

## 11. Tray and macOS Menu-bar Behavior

The tray menu contains:

- **Open Control Plane**
- a disabled status line: `Running on 127.0.0.1:8080` or the current error;
- **Copy OpenAI Base URL**
- **Copy Anthropic Base URL**
- **Open Logs Folder**
- **Open in Browser**
- **Reset Close Behavior**
- **Exit vibe-proxy**

Clicking or double-clicking the icon opens and focuses the main window according
to platform convention.

Closing the window does not remove the tray icon when the selected behavior is
`tray`. Exiting removes the tray icon only after gateway shutdown completes.

## 12. Desktop-aware Control Plane

The runtime accepts an optional, narrow `DesktopController` interface. In CLI
mode it is absent.

When present, authenticated admin endpoints expose:

- desktop availability and platform;
- close-behavior preference;
- application and log paths;
- gateway ownership and status.

The Settings page shows a **Desktop Application** section only when desktop mode
is available. It supports:

- `Ask when closing`;
- `Minimise to tray`;
- `Exit application`;
- opening the application-data directory;
- importing an existing config.

The control plane does not receive unrestricted filesystem or process access.
Native operations remain an allow-listed interface implemented by the desktop
host.

## 13. Errors and Recovery

### Invalid configuration

Show a native error dialog with:

- the validation summary;
- **Open Config Folder**;
- **Exit**.

Do not start a partially configured gateway.

### Port conflict

Do not silently choose another port because clients depend on a stable endpoint.
Show the occupied address and offer:

- **Open Existing Control Plane** when the endpoint is confirmed to be another
  vibe-proxy instance;
- **Open Config Folder**;
- **Exit**.

The desktop host does not claim ownership of an externally started gateway and
does not stop it on exit.

### Database or startup failure

Write the structured error to the desktop log, show a native dialog, and leave
SQLite data untouched.

### WebView failure

The tray remains active and provides **Open in Browser** and **Open Logs
Folder**. A WebView failure must not terminate a healthy gateway.

## 14. Packaging and Release Assets

Existing CLI assets remain unchanged.

### Windows

```text
vibe-proxy-desktop_VERSION_windows_amd64-setup.exe
vibe-proxy-desktop_VERSION_windows_arm64-setup.exe
vibe-proxy-desktop_VERSION_windows_amd64-portable.zip
vibe-proxy-desktop_VERSION_windows_arm64-portable.zip
```

The setup executable creates Start Menu entries and an uninstaller. Uninstalling
the application preserves user configuration and the SQLite database by
default.

### macOS

```text
vibe-proxy-desktop_VERSION_darwin_amd64.dmg
vibe-proxy-desktop_VERSION_darwin_arm64.dmg
```

Each DMG contains a standard `Vibe Proxy.app` and an Applications shortcut.

The first desktop RC may be unsigned. It must be labelled as a pre-release with
clear SmartScreen and Gatekeeper instructions. A stable desktop release requires
signing/notarization or an explicit maintainer-approved exception.

All desktop packages are included in `SHA256SUMS`.

## 15. Build and CI Isolation

Root CI continues to run:

- frontend tests and production build verification;
- root Go tests and vet with supported Go versions;
- pure-Go CLI cross-builds;
- existing CLI package tests.

Desktop CI adds:

- `desktop` module tests;
- an exact Wails dependency check;
- Windows AMD64 and ARM64 native builds on Windows runners;
- macOS AMD64 and ARM64 native builds on macOS runners;
- package-content inspection;
- executable architecture and embedded-version inspection;
- verification that the desktop executable supports `--version`;
- verification that the desktop executable supports built-in OCR worker mode;
- release-manifest and checksum validation.

Desktop package failures block desktop Release publication but do not remove the
ability to build and test the CLI locally.

## 16. Test Strategy

### 16.1 Unit tests

- gateway lifecycle state transitions;
- listener binding and port-conflict errors;
- graceful shutdown and idempotent shutdown;
- desktop preference defaults, persistence, atomic writes, and corruption
  recovery;
- close-decision state machine;
- bootstrap nonce expiry and single-use enforcement;
- desktop cookie authentication and same-origin enforcement;
- optional `DesktopController` behavior in desktop and CLI modes;
- platform application-data path resolution.

### 16.2 Integration tests

- start `gatewayapp` with a temporary config and SQLite database;
- wait for readiness and call `/healthz`;
- complete the one-time desktop bootstrap;
- call an admin API with the desktop session cookie;
- verify that the same admin API rejects an unauthenticated request;
- verify existing Bearer-token authentication remains valid;
- execute a data-plane request through a deterministic mock provider;
- shut down and verify the listener and database are released;
- start the CLI entrypoint against the refactored lifecycle and verify unchanged
  endpoints.

### 16.3 Desktop host tests

Native host logic is separated from Wails objects behind small interfaces so
tray commands, close behavior, and startup errors can be tested without opening
a real window.

Native build smoke tests verify:

- package compilation on each target runner;
- resource and icon inclusion;
- application name and version;
- installer or app-bundle layout;
- headless `--version` behavior.

### 16.4 Manual RC acceptance

#### Windows 10/11 AMD64

- install and uninstall the setup package;
- launch from Start Menu without a terminal;
- confirm no console window remains visible;
- configure a provider and make a text request;
- make an image request and exercise OCR/vision fallback;
- verify both first-close choices;
- restore from tray;
- relaunch and confirm the saved close preference;
- confirm the browser fallback;
- verify SmartScreen guidance for the unsigned RC.

#### Windows ARM64

Repeat the native binary, installer, tray, and request checks when hardware is
available.

#### macOS Apple Silicon and Intel

- install `Vibe Proxy.app` from each DMG;
- launch without Terminal;
- verify menu-bar behavior and native close conventions;
- run text and image requests;
- verify app-data paths and logs;
- verify Gatekeeper guidance for the unsigned RC;
- quit and confirm graceful gateway shutdown.

## 17. Implementation Phases

### Phase D1: Shared lifecycle

- create `internal/gatewayapp`;
- migrate CLI startup;
- add graceful signal shutdown;
- preserve OCR worker mode;
- add unit and integration tests.

### Phase D2: Desktop foundation

- create the isolated `desktop` module;
- pin Wails v3;
- start the gateway in the desktop lifecycle;
- create the WebView window;
- add single-instance behavior;
- add application-data paths and logs.

### Phase D3: Desktop experience

- add tray/menu-bar support;
- implement first-close decision and preferences;
- add native startup-error dialogs;
- add desktop bootstrap authentication;
- add the desktop section to Settings;
- add config import and browser fallback.

### Phase D4: Packaging and CI

- create Windows installers and portable packages;
- create macOS app bundles and DMGs;
- extend the release manifest and checksums;
- add native Windows and macOS CI jobs;
- document installation and RC acceptance.

### Phase D5: Release candidate

- run all automated gates;
- publish a desktop pre-release;
- complete physical Windows and macOS acceptance;
- resolve findings before declaring the desktop build stable.

Each phase receives its own tests and commit. A phase is not committed as
complete until its full relevant test suite passes.

## 18. Stability Gates

The desktop implementation is acceptable when:

1. root CLI tests and packages remain unchanged and green;
2. the Wails version is exactly pinned;
3. Windows and macOS native builds pass in CI;
4. desktop startup requires no terminal or environment variables;
5. closing to tray keeps all HTTP endpoints available;
6. explicit exit releases the port and SQLite database;
7. the first-close choice persists and can be reset;
8. the desktop WebView receives authenticated admin access without persisting
   the raw admin token;
9. browser Bearer-token access still works;
10. built-in OCR worker mode functions from the desktop executable;
11. package contents and checksums match the manifest;
12. manual RC acceptance is recorded before a stable desktop release.

## 19. Follow-up Opportunities

After the first desktop RC is accepted:

- start at login, disabled by default;
- signed and verified automatic updates;
- Windows Authenticode signing;
- macOS Developer ID signing and notarization;
- a unified macOS binary if Wails and OCR packaging support it reliably;
- native desktop notifications for gateway failures or update availability;
- Winget and Homebrew Cask distribution.
