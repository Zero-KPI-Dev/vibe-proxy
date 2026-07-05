#!/usr/bin/env bash
set -euo pipefail

CONFIG_PATH="${1:-configs/bootstrap.yaml}"
PORT="${VIBE_PROXY_PORT:-8080}"
ADMIN_TOKEN="${VIBE_PROXY_ADMIN_TOKEN:-admin-token}"
CONTAINER_NAME="${VIBE_PROXY_CONTAINER:-vibe-proxy-dev}"
GO_MOD_CACHE_VOLUME="${VIBE_PROXY_GOMODCACHE_VOLUME:-vibe-proxy-gomodcache}"
GO_BUILD_CACHE_VOLUME="${VIBE_PROXY_GOCACHE_VOLUME:-vibe-proxy-gocache}"
TMP_CONFIG="/tmp/vibe-proxy-docker-${PORT}.yaml"

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker is required to run vibe-proxy without a local Go installation." >&2
  exit 1
fi

if [ ! -f "$CONFIG_PATH" ]; then
  echo "Config file not found: $CONFIG_PATH" >&2
  exit 1
fi

python3 - "$CONFIG_PATH" "$TMP_CONFIG" <<'PY'
import re
import sys
src, dst = sys.argv[1], sys.argv[2]
text = open(src, 'r', encoding='utf-8').read()
if re.search(r'(?m)^\s*listen\s*:', text):
    text = re.sub(r'(?m)^(\s*listen\s*:\s*).+$', r'\g<1>0.0.0.0:8080', text, count=1)
else:
    text = text.replace('server:\n', 'server:\n  listen: 0.0.0.0:8080\n', 1)
open(dst, 'w', encoding='utf-8').write(text)
PY

docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true

docker run --rm \
  --name "$CONTAINER_NAME" \
  -p "${PORT}:8080" \
  -v "$PWD":/src \
  -v "$TMP_CONFIG":/tmp/vibe-proxy-docker.yaml:ro \
  -v "${GO_MOD_CACHE_VOLUME}":/go/pkg/mod \
  -v "${GO_BUILD_CACHE_VOLUME}":/root/.cache/go-build \
  -w /src \
  -e "VIBE_PROXY_ADMIN_TOKEN=${ADMIN_TOKEN}" \
  -e "ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:-}" \
  -e "DEEPSEEK_API_KEY=${DEEPSEEK_API_KEY:-}" \
  -e "NEW_API_KEY=${NEW_API_KEY:-}" \
  golang:1.22 \
  sh -lc "/usr/local/go/bin/go run ./cmd/vibe-proxy -config /tmp/vibe-proxy-docker.yaml"
