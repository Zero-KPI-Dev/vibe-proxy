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
mounted_dmg=
cleanup() {
  if [[ -n "$mounted_dmg" ]]; then
    hdiutil detach "$mounted_dmg" -quiet >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

fake_binary="$tmp_dir/fake-vibe-proxy"
printf '#!/bin/sh\nprintf "fake vibe-proxy\\n"\n' >"$fake_binary"
chmod 0755 "$fake_binary"

output_dir="$tmp_dir/output"
mkdir -p "$output_dir"

test_portable() {
  command -v zip >/dev/null 2>&1 || fail "portable test requires zip"
  command -v unzip >/dev/null 2>&1 || fail "portable test requires unzip"

  local windows_artifact
  windows_artifact=$(
    "$script_dir/package-portable.sh" \
      v0.1.0-rc.1 windows amd64 "$fake_binary" "$output_dir"
  )
  local expected_windows="$output_dir/vibe-proxy_0.1.0-rc.1_windows_amd64.zip"
  [[ "$windows_artifact" == "$expected_windows" ]] ||
    fail "windows artifact path: got $windows_artifact"
  assert_file "$expected_windows" "windows portable artifact"

  local windows_root="vibe-proxy_0.1.0-rc.1_windows_amd64"
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

  local linux_artifact
  linux_artifact=$(
    "$script_dir/package-portable.sh" \
      v0.1.0-rc.1 linux arm64 "$fake_binary" "$output_dir"
  )
  local expected_linux="$output_dir/vibe-proxy_0.1.0-rc.1_linux_arm64.tar.gz"
  [[ "$linux_artifact" == "$expected_linux" ]] ||
    fail "linux artifact path: got $linux_artifact"
  assert_file "$expected_linux" "linux portable artifact"

  local linux_root="vibe-proxy_0.1.0-rc.1_linux_arm64"
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
}

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
    [[ "$(dpkg-deb --field "$expected_deb" Version)" == 0.1.0-rc.1 ]] ||
      fail "DEB version for $arch"
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

test_dmg() {
  command -v hdiutil >/dev/null 2>&1 || fail "DMG test requires hdiutil"

  local dmg_artifact
  dmg_artifact=$(
    "$script_dir/package-dmg.sh" \
      v0.1.0-rc.1 amd64 "$fake_binary" "$output_dir"
  )
  local expected_dmg="$output_dir/vibe-proxy_0.1.0-rc.1_darwin_amd64.dmg"
  [[ "$dmg_artifact" == "$expected_dmg" ]] ||
    fail "DMG artifact path: got $dmg_artifact"
  assert_file "$expected_dmg" "macOS DMG artifact"

  mounted_dmg="$tmp_dir/dmg-mount"
  mkdir -p "$mounted_dmg"
  hdiutil attach \
    -nobrowse \
    -readonly \
    -mountpoint "$mounted_dmg" \
    "$expected_dmg" >/dev/null

  assert_file "$mounted_dmg/vibe-proxy" "DMG binary"
  assert_file "$mounted_dmg/README.md" "DMG README"
  assert_file "$mounted_dmg/LICENSE" "DMG license"
  assert_file "$mounted_dmg/INSTALL.txt" "DMG installation guide"
  assert_file "$mounted_dmg/configs/bootstrap.yaml" "DMG bootstrap config"
  assert_file "$mounted_dmg/configs/simple.yaml" "DMG simple config"

  hdiutil detach "$mounted_dmg" -quiet
  mounted_dmg=

  printf 'DMG packaging tests: PASS\n'
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
    vibe-proxy_0.1.0-rc.1_windows_amd64.zip \
    vibe-proxy_0.1.0-rc.1_windows_arm64.zip \
    vibe-proxy_0.1.0-rc.1_darwin_amd64.tar.gz \
    vibe-proxy_0.1.0-rc.1_darwin_arm64.tar.gz \
    vibe-proxy_0.1.0-rc.1_darwin_amd64.dmg \
    vibe-proxy_0.1.0-rc.1_darwin_arm64.dmg \
    vibe-proxy_0.1.0-rc.1_linux_amd64.tar.gz \
    vibe-proxy_0.1.0-rc.1_linux_arm64.tar.gz \
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
    "$manifest_dir/vibe-proxy_0.1.0-rc.1_darwin_arm64.dmg" \
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
    test_portable
    if command -v dpkg-deb >/dev/null 2>&1; then
      test_deb
    else
      printf 'DEB packaging tests: SKIP (dpkg-deb unavailable)\n'
    fi
    if command -v hdiutil >/dev/null 2>&1; then
      test_dmg
    else
      printf 'DMG packaging tests: SKIP (hdiutil unavailable)\n'
    fi
    test_asset_manifest
    ;;
  portable)
    test_portable
    ;;
  deb)
    test_deb
    ;;
  dmg)
    test_dmg
    ;;
  assets)
    test_asset_manifest
    ;;
  *)
    fail "unknown RELEASE_TEST_SCOPE: $scope"
    ;;
esac
