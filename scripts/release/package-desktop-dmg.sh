#!/usr/bin/env bash

set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=common.sh
source "$script_dir/common.sh"

if [[ $# -ne 4 ]]; then
  printf 'usage: %s VERSION GOARCH APP_BUNDLE OUTPUT_DIR\n' "$0" >&2
  exit 2
fi

version=$1
goarch=$2
app_bundle=$3
output_dir=$4
version_without_v=$(release_version_without_v "$version") || {
  printf 'invalid release version: %s\n' "$version" >&2
  exit 2
}
case "$goarch" in amd64 | arm64) ;; *) exit 2 ;; esac
[[ -d "$app_bundle" && -f "$app_bundle/Contents/Info.plist" ]] || {
  printf 'invalid Vibe Proxy.app bundle: %s\n' "$app_bundle" >&2
  exit 1
}
command -v hdiutil >/dev/null 2>&1 || {
  printf 'hdiutil is required to create desktop DMGs\n' >&2
  exit 1
}

mkdir -p "$output_dir"
artifact="$output_dir/vibe-proxy-desktop_${version_without_v}_darwin_${goarch}.dmg"
[[ ! -e "$artifact" ]] || {
  printf 'artifact already exists: %s\n' "$artifact" >&2
  exit 1
}

stage=$(mktemp -d "${TMPDIR:-/tmp}/vibe-proxy-desktop-dmg.XXXXXX")
trap 'rm -rf "$stage"' EXIT
cp -R "$app_bundle" "$stage/Vibe Proxy.app"
ln -s /Applications "$stage/Applications"
hdiutil create -quiet -format UDZO -volname "Vibe Proxy" -srcfolder "$stage" "$artifact"
printf '%s\n' "$artifact"
