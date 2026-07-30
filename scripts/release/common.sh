#!/usr/bin/env bash

release_script_dir=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
release_repo_root=$(CDPATH= cd -- "$release_script_dir/../.." && pwd)

release_validate_version() {
  [[ "${1:-}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$ ]]
}

release_version_without_v() {
  local version=${1:-}
  release_validate_version "$version" || return 1
  printf '%s\n' "${version#v}"
}

release_validate_target() {
  local goos=${1:-}
  local goarch=${2:-}
  case "$goos/$goarch" in
    windows/amd64 | windows/arm64 | darwin/amd64 | darwin/arm64 | linux/amd64 | linux/arm64)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

release_artifact_base() {
  local version
  version=$(release_version_without_v "${1:-}") || return 1
  release_validate_target "${2:-}" "${3:-}" || return 1
  printf 'vibe-proxy_%s_%s_%s\n' "$version" "$2" "$3"
}

release_commit_date_utc() {
  local epoch=${1:-}
  [[ "$epoch" =~ ^[0-9]+$ ]] || return 1
  if date -u -r "$epoch" '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null; then
    return 0
  fi
  date -u -d "@$epoch" '+%Y-%m-%dT%H:%M:%SZ'
}
