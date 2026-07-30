#!/usr/bin/env bash

set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=common.sh
source "$script_dir/common.sh"

if [[ $# -ne 4 ]]; then
  printf 'usage: %s VERSION GOOS GOARCH OUTPUT_DIR\n' "$0" >&2
  exit 2
fi

version=$1
goos=$2
goarch=$3
output_dir=$4
version_without_v=$(release_version_without_v "$version") || {
  printf 'invalid release version: %s\n' "$version" >&2
  exit 2
}
version_numeric=${version_without_v%%-*}
case "$goos/$goarch" in
  windows/amd64 | windows/arm64 | darwin/amd64 | darwin/arm64) ;;
  *)
    printf 'unsupported desktop target: %s/%s\n' "$goos" "$goarch" >&2
    exit 2
    ;;
esac

desktop_dir="$release_repo_root/desktop"
grep -Fxq '	github.com/wailsapp/wails/v3 v3.0.0-alpha2.117' "$desktop_dir/go.mod" || {
  printf 'desktop/go.mod must pin Wails v3.0.0-alpha2.117 exactly\n' >&2
  exit 1
}
command -v go >/dev/null 2>&1 || {
  printf 'Go is required for native desktop builds\n' >&2
  exit 1
}
if [[ "$goos" == windows ]] && ! command -v makensis >/dev/null 2>&1; then
  printf 'NSIS makensis is required for Windows desktop installers\n' >&2
  exit 1
fi

commit=$(git -C "$release_repo_root" rev-parse --short=12 HEAD)
build_date=$(release_commit_date_utc "$(git -C "$release_repo_root" show -s --format=%ct HEAD)")
mkdir -p "$output_dir"
rm -rf "$desktop_dir/bin"

(
  cd "$desktop_dir"
  # The exact version comes from the verified desktop/go.mod line. Avoiding an
  # @version query also makes native release builds reproducible offline.
  go run github.com/wailsapp/wails/v3/cmd/wails3 \
    package \
    GOOS="$goos" \
    GOARCH="$goarch" \
    ARCH="$goarch" \
    VERSION="$version" \
    VERSION_NUMERIC="$version_numeric" \
    COMMIT="$commit" \
    BUILD_DATE="$build_date"
)

if [[ "$goos" == windows ]]; then
  source_path="$desktop_dir/bin/vibe-proxy-desktop-${goarch}-setup.exe"
  artifact="$output_dir/vibe-proxy-desktop_${version_without_v}_windows_${goarch}-setup.exe"
  cp "$source_path" "$artifact"
else
  artifact="$desktop_dir/bin/Vibe Proxy.app"
  [[ -d "$artifact" ]] || {
    printf 'Wails did not produce the app bundle: %s\n' "$artifact" >&2
    exit 1
  }
fi

printf '%s\n' "$artifact"
