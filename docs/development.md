# Development

## Run Tests

vibe-proxy supports the two Go major versions maintained upstream. The module
minimum is the older supported release (currently Go 1.25), while release and
container builds use the latest security patch of the newest release.

```bash
go test ./...
```

If Go is not installed locally, use Docker:

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.26.5 sh -lc '/usr/local/go/bin/go test ./...'
```

The desktop shell is a separate Go module so Wails never leaks into the server
binary:

```bash
cd desktop
go mod verify
CGO_ENABLED=0 go test ./app/... ./wailsapp
```

## Native Desktop Development

Desktop builds pin **Go 1.26.5**, **Node.js 22**, and
**Wails v3.0.0-alpha2.117**. Windows also needs Microsoft WebView2 and NSIS;
macOS needs Xcode Command Line Tools. Use the exact pinned CLI:

```bash
cd desktop
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117 \
  build GOOS=darwin GOARCH="$(go env GOARCH)" ARCH="$(go env GOARCH)" \
  VERSION=v0.0.0-dev
```

Release packages are built with:

```bash
scripts/release/build-desktop.sh v0.1.0-rc.1 darwin arm64 "$PWD/dist"
scripts/release/package-desktop-dmg.sh \
  v0.1.0-rc.1 arm64 "$PWD/desktop/bin/Vibe Proxy.app" "$PWD/dist"
```

Docker is useful for platform-neutral tests, but it cannot replace final
WebView2, AppKit, tray, installer, signing, or notarization checks on the native
operating system.

## Run Locally

```bash
export VIBE_PROXY_ADMIN_TOKEN='change-me-admin-token'
export ANTHROPIC_API_KEY='sk-ant-your-key'
export DEEPSEEK_API_KEY='sk-your-key'
go run ./cmd/vibe-proxy -config configs/simple.yaml
```

## Local Endpoints

```text
/v1/chat/completions
/v1/responses
/anthropic/v1/messages
/v1/messages
```

## Commit Rule

Each phase should:

1. include tests for new behavior
2. pass `go test ./...`
3. be committed to `develop`
4. not be pushed until explicitly requested


## Docker Start

If Go is not installed locally, start the control plane with Docker:

```bash
VIBE_PROXY_ADMIN_TOKEN='admin-token' ./scripts/dev-docker.sh configs/bootstrap.yaml
```

Then open:

```text
http://127.0.0.1:8080/
```

Stop with `Ctrl+C`.

The Docker launcher rewrites the in-container listen address to `0.0.0.0:8080` so `http://127.0.0.1:8080/` works from your Mac.

The Docker launcher uses named Docker volumes for Go module and build caches, so dependencies are downloaded only the first time unless the cache is cleared.

To clear those caches manually:

```bash
docker volume rm vibe-proxy-gomodcache vibe-proxy-gocache
```

The Docker launcher writes UI changes to `.vibe-proxy/runtime.yaml` so local control-plane edits survive container restarts. Delete that file to reset from the source config.
