#!/usr/bin/env bash

set -euo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

assert_file() {
  local path=$1
  local label=$2
  [[ -f "$path" ]] || fail "$label: missing $path"
}

assert_members() {
  local actual_file=$1
  local expected_file=$2
  local label=$3
  if ! diff -u "$expected_file" "$actual_file"; then
    fail "$label: package members differ"
  fi
}

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/vibe-proxy-release-test.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT

fake_binary="$tmp_dir/fake-vibe-proxy"
printf '#!/bin/sh\nprintf "fake vibe-proxy\\n"\n' >"$fake_binary"
chmod 0755 "$fake_binary"

output_dir="$tmp_dir/output"
mkdir -p "$output_dir"

windows_artifact=$(
  "$script_dir/package-portable.sh" \
    v0.1.0-rc.1 windows amd64 "$fake_binary" "$output_dir"
)
expected_windows="$output_dir/vibe-proxy_0.1.0-rc.1_windows_amd64.zip"
[[ "$windows_artifact" == "$expected_windows" ]] ||
  fail "windows artifact path: got $windows_artifact"
assert_file "$expected_windows" "windows portable artifact"

windows_root="vibe-proxy_0.1.0-rc.1_windows_amd64"
cat >"$tmp_dir/windows.expected" <<EOF
$windows_root/LICENSE
$windows_root/README.md
$windows_root/configs/bootstrap.yaml
$windows_root/configs/simple.yaml
$windows_root/vibe-proxy.exe
EOF
unzip -Z1 "$expected_windows" |
  sed '/\/$/d' |
  LC_ALL=C sort >"$tmp_dir/windows.actual"
assert_members "$tmp_dir/windows.actual" "$tmp_dir/windows.expected" "windows portable"

linux_artifact=$(
  "$script_dir/package-portable.sh" \
    v0.1.0-rc.1 linux arm64 "$fake_binary" "$output_dir"
)
expected_linux="$output_dir/vibe-proxy_0.1.0-rc.1_linux_arm64.tar.gz"
[[ "$linux_artifact" == "$expected_linux" ]] ||
  fail "linux artifact path: got $linux_artifact"
assert_file "$expected_linux" "linux portable artifact"

linux_root="vibe-proxy_0.1.0-rc.1_linux_arm64"
cat >"$tmp_dir/linux.expected" <<EOF
$linux_root/LICENSE
$linux_root/README.md
$linux_root/configs/bootstrap.yaml
$linux_root/configs/simple.yaml
$linux_root/vibe-proxy
EOF
tar -tzf "$expected_linux" |
  sed '/\/$/d' |
  LC_ALL=C sort >"$tmp_dir/linux.actual"
assert_members "$tmp_dir/linux.actual" "$tmp_dir/linux.expected" "linux portable"

if "$script_dir/package-portable.sh" \
  0.1 windows amd64 "$fake_binary" "$output_dir" >/dev/null 2>&1; then
  fail "invalid version was accepted"
fi

if "$script_dir/package-portable.sh" \
  v0.1.0-rc.1 linux 386 "$fake_binary" "$output_dir" >/dev/null 2>&1; then
  fail "invalid architecture was accepted"
fi

printf 'portable packaging tests: PASS\n'
