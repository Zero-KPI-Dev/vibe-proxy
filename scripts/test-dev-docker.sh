#!/usr/bin/env bash
set -euo pipefail

script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/dev-docker.sh"

bash -n "$script"

if grep -Eq 'ADMIN_TOKEN=.*admin-token' "$script"; then
  echo "dev-docker.sh must not provide a predictable default admin token" >&2
  exit 1
fi

grep -Fq 'secrets.token_urlsafe(32)' "$script" || {
  echo "dev-docker.sh must generate a high-entropy admin token" >&2
  exit 1
}

grep -Fq 'com.docker.network.bridge.enable_icc=false' "$script" || {
  echo "dev-docker.sh must disable inter-container communication" >&2
  exit 1
}

grep -Fq -- '--network "$NETWORK_NAME"' "$script" || {
  echo "dev-docker.sh must attach the gateway to its isolated network" >&2
  exit 1
}

temp_dir="$(mktemp -d)"
cleanup_test() {
  rm -r "$temp_dir"
}
trap cleanup_test EXIT

mkdir -p "$temp_dir/bin" "$temp_dir/work"
cat >"$temp_dir/bin/docker" <<'SH'
#!/usr/bin/env sh
printf '%s\n' "$*" >>"$DOCKER_CALL_LOG"
if [ "${1:-}" = "network" ] && [ "${2:-}" = "create" ]; then
  kill -TERM "$PPID"
fi
exit 0
SH
chmod +x "$temp_dir/bin/docker"

cat >"$temp_dir/work/config.yaml" <<'YAML'
version: vibeproxy.io/v1alpha1
server:
  listen: 127.0.0.1:8080
  admin_listen: 127.0.0.1:8081
storage:
  sqlite_path: /tmp/vibe-proxy-test.db
providers: {}
YAML

docker_log="$temp_dir/docker-calls.log"
set +e
(
  cd "$temp_dir/work"
  PATH="$temp_dir/bin:$PATH" \
    DOCKER_CALL_LOG="$docker_log" \
    VIBE_PROXY_CONTAINER="vibe-proxy-interrupt-test" \
    VIBE_PROXY_ADMIN_TOKEN="test-only-high-entropy-admin-token" \
    VIBE_PROXY_RUNTIME_CONFIG="runtime.yaml" \
    "$script" config.yaml >/dev/null 2>&1
)
exit_code=$?
set -e

if [ "$exit_code" -ne 143 ]; then
  echo "interrupted launcher exited with $exit_code, want 143" >&2
  cat "$docker_log" >&2
  exit 1
fi

network_name="$(awk '/^network create / { print $NF; exit }' "$docker_log")"
if [ -z "$network_name" ] || ! grep -Fqx "network rm $network_name" "$docker_log"; then
  echo "interrupted launcher did not remove the created network" >&2
  cat "$docker_log" >&2
  exit 1
fi

echo "Docker launcher security and interruption cleanup contracts OK"
