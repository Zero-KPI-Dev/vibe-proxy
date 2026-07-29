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
