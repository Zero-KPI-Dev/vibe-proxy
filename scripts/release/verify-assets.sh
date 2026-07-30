#!/usr/bin/env bash

set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=common.sh
source "$script_dir/common.sh"

if [[ $# -ne 2 ]]; then
  printf 'usage: %s VERSION ASSET_DIRECTORY\n' "$0" >&2
  exit 2
fi

version=$1
asset_dir=$2
version_without_v=$(release_version_without_v "$version") || {
  printf 'invalid release version: %s\n' "$version" >&2
  exit 2
}
if [[ ! -d "$asset_dir" ]]; then
  printf 'asset directory does not exist: %s\n' "$asset_dir" >&2
  exit 1
fi

unexpected_entries=$(find "$asset_dir" -mindepth 1 -maxdepth 1 ! -type f -print)
if [[ -n "$unexpected_entries" ]]; then
  printf 'release asset directory contains non-file entries:\n%s\n' \
    "$unexpected_entries" >&2
  exit 1
fi

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/vibe-proxy-assets.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT

cat >"$tmp_dir/expected" <<EOF
vibe-proxy-desktop_${version_without_v}_darwin_amd64.dmg
vibe-proxy-desktop_${version_without_v}_darwin_arm64.dmg
vibe-proxy-desktop_${version_without_v}_windows_amd64-portable.zip
vibe-proxy-desktop_${version_without_v}_windows_amd64-setup.exe
vibe-proxy-desktop_${version_without_v}_windows_arm64-portable.zip
vibe-proxy-desktop_${version_without_v}_windows_arm64-setup.exe
vibe-proxy_${version_without_v}_darwin_amd64.dmg
vibe-proxy_${version_without_v}_darwin_amd64.tar.gz
vibe-proxy_${version_without_v}_darwin_arm64.dmg
vibe-proxy_${version_without_v}_darwin_arm64.tar.gz
vibe-proxy_${version_without_v}_linux_amd64.deb
vibe-proxy_${version_without_v}_linux_amd64.tar.gz
vibe-proxy_${version_without_v}_linux_arm64.deb
vibe-proxy_${version_without_v}_linux_arm64.tar.gz
vibe-proxy_${version_without_v}_windows_amd64.zip
vibe-proxy_${version_without_v}_windows_arm64.zip
EOF

find "$asset_dir" -mindepth 1 -maxdepth 1 -type f -exec basename {} \; |
  LC_ALL=C sort >"$tmp_dir/actual"

if ! diff -u "$tmp_dir/expected" "$tmp_dir/actual"; then
  printf 'release asset manifest does not match expected files\n' >&2
  exit 1
fi

printf 'release asset manifest: PASS\n'
