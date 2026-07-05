# Development

## Run Tests

```bash
go test ./...
```

If Go is not installed locally, use Docker:

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.22 sh -lc '/usr/local/go/bin/go test ./...'
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
