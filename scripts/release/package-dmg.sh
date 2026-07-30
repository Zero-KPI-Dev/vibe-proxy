#!/usr/bin/env bash

set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=common.sh
source "$script_dir/common.sh"

if [[ $# -ne 4 ]]; then
  printf 'usage: %s VERSION GOARCH INPUT_BINARY OUTPUT_DIR\n' "$0" >&2
  exit 2
fi

version=$1
goarch=$2
input_binary=$3
output_dir=$4

version_without_v=$(release_version_without_v "$version") || {
  printf 'invalid release version: %s\n' "$version" >&2
  exit 2
}
release_validate_target darwin "$goarch" || {
  printf 'unsupported macOS architecture: %s\n' "$goarch" >&2
  exit 2
}
if [[ ! -f "$input_binary" ]]; then
  printf 'input binary does not exist: %s\n' "$input_binary" >&2
  exit 1
fi
command -v hdiutil >/dev/null 2>&1 || {
  printf 'hdiutil is required to build macOS disk images\n' >&2
  exit 1
}

base=$(release_artifact_base "$version" darwin "$goarch")
mkdir -p "$output_dir"
artifact="$output_dir/$base.dmg"
if [[ -e "$artifact" ]]; then
  printf 'artifact already exists: %s\n' "$artifact" >&2
  exit 1
fi

stage_dir=$(mktemp -d "${TMPDIR:-/tmp}/vibe-proxy-dmg.XXXXXX")
trap 'rm -rf "$stage_dir"' EXIT
source_dir="$stage_dir/source"
mkdir -p "$source_dir/configs"

cp "$input_binary" "$source_dir/vibe-proxy"
chmod 0755 "$source_dir/vibe-proxy"
cp "$release_repo_root/README.md" "$source_dir/README.md"
cp "$release_repo_root/LICENSE" "$source_dir/LICENSE"
cp "$release_repo_root/configs/bootstrap.yaml" "$source_dir/configs/bootstrap.yaml"
cp "$release_repo_root/configs/simple.yaml" "$source_dir/configs/simple.yaml"
image_status="This disk image is unsigned and not notarized."
if [[ "$version_without_v" == *-* ]]; then
  image_status="This release-candidate disk image is unsigned and not notarized."
fi
cat >"$source_dir/INSTALL.txt" <<EOF
vibe-proxy $version

$image_status

Copy all files from this image to a writable local directory, open Terminal in
that directory, and run:

  export VIBE_PROXY_ADMIN_TOKEN='choose-a-local-admin-token'
  ./vibe-proxy -config configs/bootstrap.yaml

Then open http://127.0.0.1:8080/ in a browser.
EOF

hdiutil create \
  -quiet \
  -format UDZO \
  -volname "vibe-proxy $version_without_v" \
  -srcfolder "$source_dir" \
  "$artifact"

printf '%s\n' "$artifact"
