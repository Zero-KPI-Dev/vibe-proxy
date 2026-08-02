#!/usr/bin/env bash

set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

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
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

fake_binary="$tmp_dir/fake-vibe-proxy"
printf '#!/bin/sh\nprintf "fake vibe-proxy\\n"\n' >"$fake_binary"
chmod 0755 "$fake_binary"

output_dir="$tmp_dir/output"
mkdir -p "$output_dir"

test_deb() {
  command -v dpkg-deb >/dev/null 2>&1 || fail "DEB test requires dpkg-deb"

  local arch
  for arch in amd64 arm64; do
    local deb_artifact
    deb_artifact=$(
      "$script_dir/package-deb.sh" \
        v0.1.0-rc.1 "$arch" "$fake_binary" "$output_dir"
    )
    local expected_deb="$output_dir/vibe-proxy_0.1.0-rc.1_linux_${arch}.deb"
    [[ "$deb_artifact" == "$expected_deb" ]] ||
      fail "DEB artifact path for $arch: got $deb_artifact"
    assert_file "$expected_deb" "DEB artifact for $arch"

    [[ "$(dpkg-deb --field "$expected_deb" Package)" == vibe-proxy ]] ||
      fail "DEB package name for $arch"
    [[ "$(dpkg-deb --field "$expected_deb" Version)" == 0.1.0~rc.1 ]] ||
      fail "DEB version for $arch"
    dpkg --compare-versions \
      "$(dpkg-deb --field "$expected_deb" Version)" lt 0.1.0 ||
      fail "DEB release candidate does not sort before stable for $arch"
    [[ "$(dpkg-deb --field "$expected_deb" Architecture)" == "$arch" ]] ||
      fail "DEB architecture for $arch"

    local extracted="$tmp_dir/deb-$arch"
    local control="$tmp_dir/deb-control-$arch"
    mkdir -p "$extracted" "$control"
    dpkg-deb --extract "$expected_deb" "$extracted"
    dpkg-deb --control "$expected_deb" "$control"

    cat >"$tmp_dir/deb.expected" <<'EOF'
./usr/bin/vibe-proxy
./usr/share/doc/vibe-proxy/LICENSE
./usr/share/doc/vibe-proxy/README.md
./usr/share/doc/vibe-proxy/examples/bootstrap.yaml
./usr/share/doc/vibe-proxy/examples/simple.yaml
EOF
    (
      cd "$extracted"
      find . -type f | LC_ALL=C sort
    ) >"$tmp_dir/deb.actual"
    assert_members "$tmp_dir/deb.actual" "$tmp_dir/deb.expected" "DEB $arch"

    [[ ! -e "$extracted/etc" ]] || fail "DEB $arch unexpectedly contains /etc"
    [[ ! -e "$extracted/usr/lib/systemd" ]] ||
      fail "DEB $arch unexpectedly contains systemd units"
    [[ "$(find "$control" -type f -exec basename {} \; | LC_ALL=C sort)" == control ]] ||
      fail "DEB $arch unexpectedly contains maintainer scripts"
  done

  printf 'DEB packaging tests: PASS\n'
}

test_asset_manifest() {
  local manifest_dir="$tmp_dir/manifest"
  mkdir -p "$manifest_dir"

  if "$script_dir/verify-assets.sh" \
    v0.1.0-rc.1 "$manifest_dir" >/dev/null 2>&1; then
    fail "empty release asset directory was accepted"
  fi

  local name
  for name in \
    vibe-proxy-desktop_0.1.0-rc.1_windows_amd64-setup.exe \
    vibe-proxy-desktop_0.1.0-rc.1_windows_arm64-setup.exe \
    vibe-proxy-desktop_0.1.0-rc.1_darwin_amd64.dmg \
    vibe-proxy-desktop_0.1.0-rc.1_darwin_arm64.dmg \
    vibe-proxy_0.1.0-rc.1_linux_amd64.deb \
    vibe-proxy_0.1.0-rc.1_linux_arm64.deb; do
    : >"$manifest_dir/$name"
  done

  "$script_dir/verify-assets.sh" v0.1.0-rc.1 "$manifest_dir" >/dev/null ||
    fail "complete release asset directory was rejected"

  : >"$manifest_dir/unexpected.txt"
  if "$script_dir/verify-assets.sh" \
    v0.1.0-rc.1 "$manifest_dir" >/dev/null 2>&1; then
    fail "unexpected release asset was accepted"
  fi
  mv "$manifest_dir/unexpected.txt" "$tmp_dir/unexpected.txt"

  mv \
    "$manifest_dir/vibe-proxy-desktop_0.1.0-rc.1_darwin_arm64.dmg" \
    "$tmp_dir/missing.dmg"
  if "$script_dir/verify-assets.sh" \
    v0.1.0-rc.1 "$manifest_dir" >/dev/null 2>&1; then
    fail "missing release asset was accepted"
  fi

  printf 'release asset manifest tests: PASS\n'
}

scope=${RELEASE_TEST_SCOPE:-all}
case "$scope" in
  all)
    if command -v dpkg-deb >/dev/null 2>&1; then
      test_deb
    else
      printf 'DEB packaging tests: SKIP (dpkg-deb unavailable)\n'
    fi
    test_asset_manifest
    ;;
  deb)
    test_deb
    ;;
  assets)
    test_asset_manifest
    ;;
  *)
    fail "unknown RELEASE_TEST_SCOPE: $scope"
    ;;
esac
