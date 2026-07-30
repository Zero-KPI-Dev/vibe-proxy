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
debian_version=$version_without_v
if [[ "$version_without_v" == *-* ]]; then
  debian_version="${version_without_v%%-*}~${version_without_v#*-}"
fi
release_validate_target linux "$goarch" || {
  printf 'unsupported Debian architecture: %s\n' "$goarch" >&2
  exit 2
}
if [[ ! -f "$input_binary" ]]; then
  printf 'input binary does not exist: %s\n' "$input_binary" >&2
  exit 1
fi
command -v dpkg-deb >/dev/null 2>&1 || {
  printf 'dpkg-deb is required to build Debian packages\n' >&2
  exit 1
}

base=$(release_artifact_base "$version" linux "$goarch")
mkdir -p "$output_dir"
artifact="$output_dir/$base.deb"
if [[ -e "$artifact" ]]; then
  printf 'artifact already exists: %s\n' "$artifact" >&2
  exit 1
fi

stage_dir=$(mktemp -d "${TMPDIR:-/tmp}/vibe-proxy-deb.XXXXXX")
trap 'rm -rf "$stage_dir"' EXIT
package_root="$stage_dir/package"

mkdir -p \
  "$package_root/DEBIAN" \
  "$package_root/usr/bin" \
  "$package_root/usr/share/doc/vibe-proxy/examples"

cat >"$package_root/DEBIAN/control" <<EOF
Package: vibe-proxy
Version: $debian_version
Section: utils
Priority: optional
Architecture: $goarch
Maintainer: vibe-proxy contributors
Description: Local-first multi-protocol LLM proxy
EOF

cp "$input_binary" "$package_root/usr/bin/vibe-proxy"
chmod 0755 "$package_root/usr/bin/vibe-proxy"
cp "$release_repo_root/README.md" "$package_root/usr/share/doc/vibe-proxy/README.md"
cp "$release_repo_root/LICENSE" "$package_root/usr/share/doc/vibe-proxy/LICENSE"
cp \
  "$release_repo_root/configs/bootstrap.yaml" \
  "$package_root/usr/share/doc/vibe-proxy/examples/bootstrap.yaml"
cp \
  "$release_repo_root/configs/simple.yaml" \
  "$package_root/usr/share/doc/vibe-proxy/examples/simple.yaml"
chmod 0644 \
  "$package_root/DEBIAN/control" \
  "$package_root/usr/share/doc/vibe-proxy/README.md" \
  "$package_root/usr/share/doc/vibe-proxy/LICENSE" \
  "$package_root/usr/share/doc/vibe-proxy/examples/bootstrap.yaml" \
  "$package_root/usr/share/doc/vibe-proxy/examples/simple.yaml"

dpkg-deb --root-owner-group --build "$package_root" "$artifact" >/dev/null

printf '%s\n' "$artifact"
