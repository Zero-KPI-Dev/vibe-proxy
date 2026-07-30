#!/usr/bin/env bash

set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=common.sh
source "$script_dir/common.sh"

if [[ $# -ne 4 ]]; then
  printf 'usage: %s VERSION GOOS GOARCH OUTPUT_BINARY\n' "$0" >&2
  exit 2
fi

version=$1
goos=$2
goarch=$3
output=$4

release_validate_version "$version" || {
  printf 'invalid release version: %s\n' "$version" >&2
  exit 2
}
release_validate_target "$goos" "$goarch" || {
  printf 'unsupported release target: %s/%s\n' "$goos" "$goarch" >&2
  exit 2
}
if [[ -e "$output" ]]; then
  printf 'output already exists: %s\n' "$output" >&2
  exit 1
fi

go_binary=${GO_BINARY:-go}
command -v "$go_binary" >/dev/null 2>&1 || {
  printf 'Go executable not found: %s\n' "$go_binary" >&2
  exit 1
}

commit=$(git -C "$release_repo_root" rev-parse --short=12 HEAD)
commit_epoch=$(git -C "$release_repo_root" show -s --format=%ct HEAD)
build_date=$(release_commit_date_utc "$commit_epoch")

mkdir -p "$(dirname -- "$output")"

ldflags="-s -w"
ldflags+=" -X github.com/a448582655/vibe-proxy/internal/buildinfo.Version=$version"
ldflags+=" -X github.com/a448582655/vibe-proxy/internal/buildinfo.Commit=$commit"
ldflags+=" -X github.com/a448582655/vibe-proxy/internal/buildinfo.BuildDate=$build_date"

(
  cd "$release_repo_root"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" "$go_binary" build \
    -tags="netgo osusergo" \
    -trimpath \
    -ldflags="$ldflags" \
    -o "$output" \
    ./cmd/vibe-proxy
)

printf '%s\n' "$output"
