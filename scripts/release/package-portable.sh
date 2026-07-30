#!/usr/bin/env bash

set -euo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=common.sh
source "$script_dir/common.sh"

if [[ $# -ne 5 ]]; then
  printf 'usage: %s VERSION GOOS GOARCH INPUT_BINARY OUTPUT_DIR\n' "$0" >&2
  exit 2
fi

version=$1
goos=$2
goarch=$3
input_binary=$4
output_dir=$5

base=$(release_artifact_base "$version" "$goos" "$goarch") || {
  printf 'invalid release target: version=%s target=%s/%s\n' "$version" "$goos" "$goarch" >&2
  exit 2
}
if [[ ! -f "$input_binary" ]]; then
  printf 'input binary does not exist: %s\n' "$input_binary" >&2
  exit 1
fi

mkdir -p "$output_dir"
if [[ "$goos" == windows ]]; then
  artifact="$output_dir/$base.zip"
  binary_name=vibe-proxy.exe
  command -v zip >/dev/null 2>&1 || {
    printf 'zip is required to package Windows artifacts\n' >&2
    exit 1
  }
else
  artifact="$output_dir/$base.tar.gz"
  binary_name=vibe-proxy
  command -v tar >/dev/null 2>&1 || {
    printf 'tar is required to package Unix artifacts\n' >&2
    exit 1
  }
fi

if [[ -e "$artifact" ]]; then
  printf 'artifact already exists: %s\n' "$artifact" >&2
  exit 1
fi

stage_dir=$(mktemp -d "${TMPDIR:-/tmp}/vibe-proxy-portable.XXXXXX")
trap 'rm -rf "$stage_dir"' EXIT
package_root="$stage_dir/$base"
mkdir -p "$package_root/configs"

cp "$input_binary" "$package_root/$binary_name"
chmod 0755 "$package_root/$binary_name"
cp "$release_repo_root/README.md" "$package_root/README.md"
cp "$release_repo_root/LICENSE" "$package_root/LICENSE"
cp "$release_repo_root/configs/bootstrap.yaml" "$package_root/configs/bootstrap.yaml"
cp "$release_repo_root/configs/simple.yaml" "$package_root/configs/simple.yaml"

if [[ "$goos" == windows ]]; then
  (
    cd "$stage_dir"
    zip -X -q -r "$artifact" "$base"
  )
else
  tar -C "$stage_dir" -czf "$artifact" "$base"
fi

printf '%s\n' "$artifact"
