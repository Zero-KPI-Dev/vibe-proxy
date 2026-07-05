#!/usr/bin/env bash
set -euo pipefail

CONFIG_PATH="${1:-configs/bootstrap.yaml}"
PORT="${VIBE_PROXY_PORT:-8080}"
ADMIN_TOKEN="${VIBE_PROXY_ADMIN_TOKEN:-admin-token}"
CONTAINER_NAME="${VIBE_PROXY_CONTAINER:-vibe-proxy-dev}"

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker is required to run vibe-proxy without a local Go installation." >&2
  exit 1
fi

if [ ! -f "$CONFIG_PATH" ]; then
  echo "Config file not found: $CONFIG_PATH" >&2
  exit 1
fi

docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true

docker run --rm \
  --name "$CONTAINER_NAME" \
  -p "${PORT}:8080" \
  -v "$PWD":/src \
  -w /src \
  -e "VIBE_PROXY_ADMIN_TOKEN=${ADMIN_TOKEN}" \
  -e "ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:-}" \
  -e "DEEPSEEK_API_KEY=${DEEPSEEK_API_KEY:-}" \
  -e "NEW_API_KEY=${NEW_API_KEY:-}" \
  golang:1.22 \
  sh -lc "/usr/local/go/bin/go run ./cmd/vibe-proxy -config ${CONFIG_PATH}"
