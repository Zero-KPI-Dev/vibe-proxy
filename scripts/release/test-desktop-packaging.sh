#!/usr/bin/env bash

set -euo pipefail

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
require_file() { [[ -f "$repo_root/$1" ]] || fail "missing $1"; }
require_text() { grep -Fq -- "$2" "$repo_root/$1" || fail "$1 missing: $2"; }
require_count() {
  local actual
  actual=$(grep -Fc -- "$2" "$repo_root/$1" || true)
  [[ "$actual" == "$3" ]] ||
    fail "$1 count for '$2': got $actual, want $3"
}
reject_text() {
  if grep -Fq -- "$2" "$repo_root/$1"; then
    fail "$1 unexpectedly contains: $2"
  fi
}

for path in \
  desktop/Taskfile.yml \
  desktop/build/config.yml \
  desktop/build/windows/Taskfile.yml \
  desktop/build/windows/info.json \
  desktop/build/windows/nsis/project.nsi \
  desktop/build/darwin/Taskfile.yml \
  desktop/build/darwin/Info.plist \
  scripts/release/build-desktop.sh \
  scripts/release/package-desktop-dmg.sh; do
  require_file "$path"
done

require_text desktop/go.mod 'github.com/wailsapp/wails/v3 v3.0.0-alpha2.118'
require_text desktop/build/config.yml 'productIdentifier: io.vibeproxy.desktop'
require_text desktop/build/darwin/Info.plist '<string>io.vibeproxy.desktop</string>'
require_text desktop/build/windows/Taskfile.yml '-H windowsgui'
require_text desktop/build/windows/Taskfile.yml '{{if eq OS "windows"}}'
require_text desktop/build/windows/Taskfile.yml '{{.ROOT_DIR}}\{{.BIN_DIR}}\{{.APP_NAME}}.exe'
require_text desktop/build/windows/nsis/project.nsi 'CreateShortcut "$SMPROGRAMS\Vibe Proxy.lnk"'
require_text desktop/build/windows/nsis/project.nsi 'wails.writeUninstaller'
require_text desktop/build/windows/nsis/project.nsi '%LOCALAPPDATA%\vibe-proxy is intentionally preserved'
if grep -Fq 'RMDir /r "$LOCALAPPDATA\vibe-proxy"' desktop/build/windows/nsis/project.nsi; then
  fail 'NSIS uninstaller deletes desktop application data'
fi
require_text scripts/release/package-desktop-dmg.sh 'Vibe Proxy.app'
require_text scripts/release/package-desktop-dmg.sh 'ln -s /Applications'

expected_names=(
  vibe-proxy-desktop_0.1.0-rc.1_windows_amd64-setup.exe
  vibe-proxy-desktop_0.1.0-rc.1_windows_arm64-setup.exe
  vibe-proxy-desktop_0.1.0-rc.1_darwin_amd64.dmg
  vibe-proxy-desktop_0.1.0-rc.1_darwin_arm64.dmg
  vibe-proxy_0.1.0-rc.1_linux_amd64.deb
  vibe-proxy_0.1.0-rc.1_linux_arm64.deb
)
for name in "${expected_names[@]}"; do
  require_text docs/release.md "$name"
done

require_text docs/release.md '%LOCALAPPDATA%\vibe-proxy'
require_text docs/release.md '~/Library/Application Support/vibe-proxy'
require_text docs/release.md 'Linux uses the CLI'
require_text docs/release.md 'first window close'
require_text docs/release.md 'Open in Browser'
require_text docs/release.md 'management password'
require_text docs/release.md 'auth.json'
require_text docs/release.md 'SmartScreen'
require_text docs/release.md 'Gatekeeper'
require_text docs/release.md 'requires code signing and macOS notarization'
require_text docs/development.md 'Wails v3.0.0-alpha2.118'
require_text docs/development.md 'Docker is useful'
require_text docs/admin-api.md '/desktop/bootstrap/{nonce}'
require_text docs/admin-api.md 'POST /auth/setup'
require_text docs/admin-api.md 'POST /auth/login'
require_text docs/admin-api.md 'POST /admin/desktop/import-config'

require_file .github/workflows/desktop-build.yml
require_file .github/workflows/release.yml
require_file scripts/release/test-windows-desktop-startup.ps1
for workflow in .github/workflows/desktop-build.yml .github/workflows/release.yml; do
  require_text "$workflow" 'actions/checkout@v6'
  require_text "$workflow" 'actions/setup-go@v6'
done
require_text .github/workflows/release.yml 'actions/upload-artifact@v7'
reject_text .github/workflows/desktop-build.yml 'actions/upload-artifact@v7'
require_text .github/workflows/release.yml 'actions/download-artifact@v8'
require_text .github/workflows/release.yml 'desktop-windows:'
require_text .github/workflows/release.yml 'desktop-macos:'
require_text .github/workflows/release.yml 'needs: [deb, desktop-windows, desktop-macos]'
require_count .github/workflows/release.yml 'retention-days: 1' 3
reject_text .github/workflows/release.yml 'retention-days: 7'
reject_text .github/workflows/release.yml '  portable:'
reject_text .github/workflows/release.yml '  dmg:'
reject_text .github/workflows/release.yml 'package-portable.sh'
reject_text .github/workflows/release.yml 'package-desktop-portable.ps1'
reject_text .github/workflows/release.yml 'package-dmg.sh'
reject_text .github/workflows/test.yml 'package-portable.sh'
reject_text .github/workflows/test.yml 'package-dmg.sh'
reject_text .github/workflows/test.yml 'release-packaging-macos:'
reject_text docs/release.md '-portable.zip'
reject_text docs/release.md '.tar.gz'
reject_text README.md 'portable ZIP'
reject_text docs/desktop-rc-checklist.md 'portable launch'
require_text .github/workflows/release.yml 'macos-14'
require_text .github/workflows/release.yml 'macos-15-intel'
require_text .github/workflows/release.yml 'windows-latest'
require_text .github/workflows/release.yml 'choco install nsis'
require_text .github/workflows/desktop-build.yml 'macos-15-intel'
require_text .github/workflows/desktop-build.yml 'choco install nsis'
require_text .github/workflows/desktop-build.yml "if: matrix.goarch == 'amd64'"
require_text .github/workflows/release.yml "if: matrix.goarch == 'amd64'"
require_text .github/workflows/desktop-build.yml 'test-windows-desktop-startup.ps1'
require_text .github/workflows/release.yml 'test-windows-desktop-startup.ps1'
require_text .github/workflows/release.yml '--prerelease --latest=false'

bash -n "$repo_root/scripts/release/build-desktop.sh"
bash -n "$repo_root/scripts/release/package-desktop-dmg.sh"
printf 'desktop packaging contracts: PASS\n'
