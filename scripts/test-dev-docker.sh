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
