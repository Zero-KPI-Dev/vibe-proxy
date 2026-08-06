#!/usr/bin/env bash
set -euo pipefail

CONFIG_PATH="${1:-configs/bootstrap.yaml}"
PORT="${VIBE_PROXY_PORT:-8080}"
ADMIN_PORT="${VIBE_PROXY_ADMIN_PORT:-8081}"
ADMIN_TOKEN="${VIBE_PROXY_ADMIN_TOKEN:-admin-token}"
CONTAINER_NAME="${VIBE_PROXY_CONTAINER:-vibe-proxy-dev}"
GO_MOD_CACHE_VOLUME="${VIBE_PROXY_GOMODCACHE_VOLUME:-vibe-proxy-gomodcache}"
GO_BUILD_CACHE_VOLUME="${VIBE_PROXY_GOCACHE_VOLUME:-vibe-proxy-gocache}"
RUNTIME_CONFIG="${VIBE_PROXY_RUNTIME_CONFIG:-.vibe-proxy/runtime.yaml}"

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker is required to run vibe-proxy without a local Go installation." >&2
  exit 1
fi

if [ ! -f "$CONFIG_PATH" ]; then
  echo "Config file not found: $CONFIG_PATH" >&2
  exit 1
fi

mkdir -p "$(dirname "$RUNTIME_CONFIG")"
if [ ! -f "$RUNTIME_CONFIG" ]; then
  cp "$CONFIG_PATH" "$RUNTIME_CONFIG"
fi

python3 - "$RUNTIME_CONFIG" <<'PY'
import re
import sys
path = sys.argv[1]
text = open(path, 'r', encoding='utf-8').read()
if re.search(r'(?m)^\s*listen\s*:', text):
    text = re.sub(r'(?m)^(\s*listen\s*:\s*).+$', r'\g<1>0.0.0.0:8080', text, count=1)
elif 'server:\n' in text:
    text = text.replace('server:\n', 'server:\n  listen: 0.0.0.0:8080\n', 1)
else:
    text = 'server:\n  listen: 0.0.0.0:8080\n' + text
if re.search(r'(?m)^\s*admin_listen\s*:', text):
    text = re.sub(r'(?m)^(\s*admin_listen\s*:\s*).+$', r'\g<1>127.0.0.1:8081', text, count=1)
elif 'server:\n' in text:
    text = text.replace('server:\n', 'server:\n  admin_listen: 127.0.0.1:8081\n', 1)
open(path, 'w', encoding='utf-8').write(text)
PY

docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true

docker run --rm \
  --name "$CONTAINER_NAME" \
  -p "127.0.0.1:${PORT}:8080" \
  -p "127.0.0.1:${ADMIN_PORT}:18081" \
  -v "$PWD":/src \
  -v "$PWD/$RUNTIME_CONFIG":/tmp/vibe-proxy-docker.yaml \
  -v "${GO_MOD_CACHE_VOLUME}":/go/pkg/mod \
  -v "${GO_BUILD_CACHE_VOLUME}":/root/.cache/go-build \
  -w /src \
  -e "VIBE_PROXY_ADMIN_TOKEN=${ADMIN_TOKEN}" \
  -e "ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:-}" \
  -e "DEEPSEEK_API_KEY=${DEEPSEEK_API_KEY:-}" \
  -e "NEW_API_KEY=${NEW_API_KEY:-}" \
  golang:1.26.5 \
  sh -lc 'cat > /tmp/vibe-proxy-admin-relay.go <<GO
package main

import (
  "io"
  "net"
)

func main() {
  listener, err := net.Listen("tcp", "0.0.0.0:18081")
  if err != nil { panic(err) }
  for {
    downstream, err := listener.Accept()
    if err != nil { return }
    go func() {
      upstream, err := net.Dial("tcp", "127.0.0.1:8081")
      if err != nil { downstream.Close(); return }
      go func() { _, _ = io.Copy(upstream, downstream); _ = upstream.Close() }()
      _, _ = io.Copy(downstream, upstream)
      _ = downstream.Close()
    }()
  }
}
GO
/usr/local/go/bin/go run /tmp/vibe-proxy-admin-relay.go &
relay_pid=$!
trap "kill $relay_pid >/dev/null 2>&1 || true" EXIT INT TERM
/usr/local/go/bin/go run ./cmd/vibe-proxy -config /tmp/vibe-proxy-docker.yaml'
