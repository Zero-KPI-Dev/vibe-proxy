# Cross-Platform Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish a tested `v0.1.0-rc.1` GitHub Pre-release containing Windows, macOS, and Linux AMD64/ARM64 artifacts, then retain the same automation for stable `v0.1.0`.

**Architecture:** A small Go build-information package supplies `--version`, while focused shell scripts build and package deterministic artifact layouts. A tag-triggered GitHub Actions workflow validates the tagged revision, builds all portable and native packages, verifies the complete asset manifest, and creates either a Pre-release or stable Release with generated notes.

**Tech Stack:** Go 1.25/1.26, POSIX shell, ZIP/tar, `dpkg-deb`, macOS `hdiutil`, GitHub Actions, GitHub CLI.

## Global Constraints

- Release builds use Go `1.26.5` and `CGO_ENABLED=0`.
- Supported targets are Windows, macOS, and Linux on AMD64 and ARM64.
- Tags are `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-rc.N`.
- The first published tag is `v0.1.0-rc.1` and must be a GitHub Pre-release.
- Stable `v0.1.0` is not published until cross-platform RC acceptance passes.
- Portable packages contain only the binary, `README.md`, `LICENSE`, `configs/bootstrap.yaml`, and `configs/simple.yaml`.
- The DEB creates no users, services, startup jobs, or `/etc` configuration.
- The DMG is unsigned and not notarized.
- The release workflow receives no provider credentials.
- GitHub `contents: write` permission is limited to the publish job.
- No Windows MSI, RPM, Homebrew, Winget, container publishing, updater, SBOM, provenance, or artifact signing is added in this phase.

---

## File Structure

### New files

- `internal/buildinfo/buildinfo.go` — build metadata and stable version string.
- `internal/buildinfo/buildinfo_test.go` — default and injected formatting tests.
- `scripts/release/common.sh` — version/target validation and shared naming.
- `scripts/release/build-binary.sh` — CGO-free binary build with linker metadata.
- `scripts/release/package-portable.sh` — ZIP/tar portable package creation.
- `scripts/release/package-deb.sh` — side-effect-free Debian package creation.
- `scripts/release/package-dmg.sh` — unsigned DMG creation on macOS.
- `scripts/release/verify-assets.sh` — exact release artifact manifest validation.
- `scripts/release/test-packaging.sh` — executable packaging contract tests.
- `.github/workflows/release.yml` — tag-triggered validate/build/publish pipeline.
- `docs/release.md` — download, install, checksum, and RC testing guide.

### Modified files

- `cmd/vibe-proxy/main.go` — add `--version` early exit.
- `.github/workflows/test.yml` — add release-script and cross-build gates.
- `README.md` — add release installation entry points.
- `docs/superpowers/specs/2026-07-30-cross-platform-release-design.md` — keep rollout state synchronized if implementation decisions change.

---

### Task 1: Embedded Build Information and `--version`

**Files:**
- Create: `internal/buildinfo/buildinfo.go`
- Create: `internal/buildinfo/buildinfo_test.go`
- Modify: `cmd/vibe-proxy/main.go`

**Interfaces:**
- Produces: `buildinfo.Version`, `buildinfo.Commit`, `buildinfo.BuildDate` string variables.
- Produces: `buildinfo.String() string`.
- Consumes later: linker flags target the three exported variables.

- [ ] **Step 1: Write failing build-information tests**

```go
package buildinfo

import "testing"

func TestStringUsesDevelopmentDefaults(t *testing.T) {
	got := String()
	want := "vibe-proxy dev (commit unknown, built unknown)"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestStringUsesInjectedValues(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, BuildDate
	t.Cleanup(func() {
		Version, Commit, BuildDate = oldVersion, oldCommit, oldDate
	})
	Version = "v0.1.0-rc.1"
	Commit = "9a4caf7"
	BuildDate = "2026-07-30T12:00:00Z"

	got := String()
	want := "vibe-proxy v0.1.0-rc.1 (commit 9a4caf7, built 2026-07-30T12:00:00Z)"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
CGO_ENABLED=0 go test ./internal/buildinfo
```

Expected: FAIL because package `internal/buildinfo` does not exist.

- [ ] **Step 3: Implement the minimal build-information package**

```go
package buildinfo

import "fmt"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func String() string {
	return fmt.Sprintf("vibe-proxy %s (commit %s, built %s)", Version, Commit, BuildDate)
}
```

- [ ] **Step 4: Verify GREEN**

Run:

```bash
CGO_ENABLED=0 go test ./internal/buildinfo
```

Expected: PASS.

- [ ] **Step 5: Add the CLI flag**

Add to `cmd/vibe-proxy/main.go`:

```go
version := flag.Bool("version", false, "print version and exit")
flag.Parse()
if *version {
	fmt.Fprintln(os.Stdout, buildinfo.String())
	return
}
```

The check must occur before loading configuration or opening SQLite, but after
the builtin OCR worker argument check.

- [ ] **Step 6: Verify the CLI without a configuration file**

Run from an empty temporary directory:

```bash
go build -o /tmp/vibe-proxy-version ./cmd/vibe-proxy
cd "$(mktemp -d)"
/tmp/vibe-proxy-version --version
```

Expected:

```text
vibe-proxy dev (commit unknown, built unknown)
```

- [ ] **Step 7: Run related tests and commit**

```bash
CGO_ENABLED=0 go test ./internal/buildinfo ./cmd/vibe-proxy
git add internal/buildinfo cmd/vibe-proxy/main.go
git commit -m "feat: expose release build information"
```

---

### Task 2: Portable Build and Packaging Contract

**Files:**
- Create: `scripts/release/common.sh`
- Create: `scripts/release/build-binary.sh`
- Create: `scripts/release/package-portable.sh`
- Create: `scripts/release/test-packaging.sh`

**Interfaces:**
- Produces: `release_version_without_v VERSION`.
- Produces: `release_validate_target GOOS GOARCH`.
- Produces: `release_artifact_base VERSION GOOS GOARCH`.
- Produces CLI: `build-binary.sh VERSION GOOS GOARCH OUTPUT_BINARY`.
- Produces CLI: `package-portable.sh VERSION GOOS GOARCH INPUT_BINARY OUTPUT_DIR`.
- Consumes: repository `README.md`, `LICENSE`, and two configuration examples.

- [ ] **Step 1: Write a failing portable packaging contract test**

`scripts/release/test-packaging.sh` must:

1. Create a temporary executable file containing `fake vibe-proxy`.
2. Call `package-portable.sh v0.1.0-rc.1 windows amd64`.
3. Assert the ZIP filename is
   `vibe-proxy_0.1.0-rc.1_windows_amd64.zip`.
4. Inspect ZIP members and require one versioned root containing exactly:
   `vibe-proxy.exe`, `README.md`, `LICENSE`, `configs/bootstrap.yaml`, and
   `configs/simple.yaml`.
5. Repeat for `linux arm64`, requiring the matching tarball and `vibe-proxy`.
6. Assert invalid version `0.1` and invalid architecture `386` fail.

The test exits nonzero and prints the failed assertion name.

- [ ] **Step 2: Run the test and verify RED**

```bash
bash scripts/release/test-packaging.sh
```

Expected: FAIL because packaging scripts do not exist.

- [ ] **Step 3: Implement shared validation**

`common.sh` must:

```bash
release_validate_version() {
  [[ "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$ ]]
}

release_version_without_v() {
  release_validate_version "$1" || return 1
  printf '%s\n' "${1#v}"
}

release_validate_target() {
  case "$1/$2" in
    windows/amd64|windows/arm64|darwin/amd64|darwin/arm64|linux/amd64|linux/arm64) ;;
    *) return 1 ;;
  esac
}
```

All scripts use `set -euo pipefail`, resolve the repository root from their own
location, quote every path, and create staging directories with `mktemp -d`.

- [ ] **Step 4: Implement the binary build script**

`build-binary.sh` must:

- require exactly four arguments
- reject an output path that already exists
- derive a 12-character commit from `git rev-parse --short=12 HEAD`
- derive UTC build time from the commit timestamp
- run:

```bash
CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
  -trimpath \
  -ldflags="-s -w \
    -X github.com/a448582655/vibe-proxy/internal/buildinfo.Version=$version \
    -X github.com/a448582655/vibe-proxy/internal/buildinfo.Commit=$commit \
    -X github.com/a448582655/vibe-proxy/internal/buildinfo.BuildDate=$build_date" \
  -o "$output" ./cmd/vibe-proxy
```

- [ ] **Step 5: Implement portable packaging**

`package-portable.sh` must:

- validate that the input binary is a regular file
- use `.exe` only for Windows
- set executable mode for Unix binaries
- create a single versioned top-level directory
- use `zip -X -r` for Windows
- use `tar -czf` for Unix
- reject pre-existing output artifacts
- print only the resulting artifact path on stdout

- [ ] **Step 6: Verify GREEN and build all targets**

```bash
bash scripts/release/test-packaging.sh
asset_dir=$(mktemp -d)
binary_dir=$(mktemp -d)
for target in windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
  goos=${target%/*}
  goarch=${target#*/}
  extension=
  if [ "$goos" = windows ]; then
    extension=.exe
  fi
  binary="$binary_dir/vibe-proxy-${goos}-${goarch}${extension}"
  scripts/release/build-binary.sh v0.1.0-rc.1 "$goos" "$goarch" "$binary"
  scripts/release/package-portable.sh \
    v0.1.0-rc.1 "$goos" "$goarch" "$binary" "$asset_dir"
done
```

Expected: contract test passes and six portable archives are created.

- [ ] **Step 7: Commit**

```bash
git add scripts/release
git commit -m "feat: package portable release binaries"
```

---

### Task 3: Debian Packages

**Files:**
- Create: `scripts/release/package-deb.sh`
- Modify: `scripts/release/test-packaging.sh`

**Interfaces:**
- Produces CLI: `package-deb.sh VERSION GOARCH INPUT_BINARY OUTPUT_DIR`.
- Produces: `vibe-proxy_VERSION_linux_GOARCH.deb`.
- Consumes: `release_version_without_v` and shared repository paths.

- [ ] **Step 1: Extend the contract test and verify RED**

Add assertions for AMD64 and ARM64 DEBs:

- `dpkg-deb --field` reports package `vibe-proxy`
- version is `0.1.0-rc.1`
- architecture matches `amd64` or `arm64`
- archive contains `/usr/bin/vibe-proxy`
- archive contains examples under `/usr/share/doc/vibe-proxy/examples/`
- archive contains no `/etc`, systemd, init, or maintainer scripts

Run:

```bash
bash scripts/release/test-packaging.sh
```

Expected: FAIL because `package-deb.sh` does not exist.

- [ ] **Step 2: Implement the DEB packager**

Create a temporary package root containing:

```text
DEBIAN/control
usr/bin/vibe-proxy
usr/share/doc/vibe-proxy/README.md
usr/share/doc/vibe-proxy/LICENSE
usr/share/doc/vibe-proxy/examples/bootstrap.yaml
usr/share/doc/vibe-proxy/examples/simple.yaml
```

Control metadata:

```text
Package: vibe-proxy
Version: VERSION_WITHOUT_V
Section: utils
Priority: optional
Architecture: ARCH
Maintainer: vibe-proxy contributors
Description: Local-first multi-protocol LLM proxy
```

Use `dpkg-deb --root-owner-group --build`.

- [ ] **Step 3: Verify GREEN**

```bash
bash scripts/release/test-packaging.sh
```

Expected: all portable and DEB assertions pass.

- [ ] **Step 4: Commit**

```bash
git add scripts/release/package-deb.sh scripts/release/test-packaging.sh
git commit -m "feat: package Debian release artifacts"
```

---

### Task 4: macOS DMG Packages

**Files:**
- Create: `scripts/release/package-dmg.sh`
- Modify: `scripts/release/test-packaging.sh`

**Interfaces:**
- Produces CLI: `package-dmg.sh VERSION GOARCH INPUT_BINARY OUTPUT_DIR`.
- Produces: `vibe-proxy_VERSION_darwin_GOARCH.dmg`.
- Requires: macOS and `hdiutil`.

- [ ] **Step 1: Add a macOS-only failing test**

When `uname -s` is `Darwin`, extend `test-packaging.sh` to:

- package a fake AMD64 binary into a DMG
- attach the DMG read-only with `hdiutil attach -nobrowse`
- verify binary, README, LICENSE, and both configs
- detach the mounted image in a cleanup trap
- require the artifact filename

On non-macOS systems, print one explicit SKIP line and keep the test successful.

- [ ] **Step 2: Run on macOS and verify RED**

```bash
bash scripts/release/test-packaging.sh
```

Expected: FAIL because `package-dmg.sh` does not exist.

- [ ] **Step 3: Implement unsigned DMG packaging**

`package-dmg.sh` must:

- accept only `amd64` and `arm64`
- create the same documented file set as portable archives
- write `INSTALL.txt` explaining the package is unsigned and showing
  `vibe-proxy -config configs/bootstrap.yaml`
- use a temporary read-write source directory
- create a compressed read-only image with `hdiutil create -format UDZO`
- use volume name `vibe-proxy VERSION`
- reject an existing output artifact

- [ ] **Step 4: Verify GREEN**

```bash
bash scripts/release/test-packaging.sh
```

Expected: DMG creates, mounts, contains expected files, and detaches cleanly.

- [ ] **Step 5: Commit**

```bash
git add scripts/release/package-dmg.sh scripts/release/test-packaging.sh
git commit -m "feat: package macOS release images"
```

---

### Task 5: Exact Asset Verification and Release Workflow

**Files:**
- Create: `scripts/release/verify-assets.sh`
- Create: `.github/workflows/release.yml`
- Modify: `scripts/release/test-packaging.sh`

**Interfaces:**
- Produces CLI: `verify-assets.sh VERSION ASSET_DIRECTORY`.
- Requires exactly ten package assets before checksum generation.
- Workflow consumes every script created in Tasks 2–4.

- [ ] **Step 1: Add failing asset-manifest tests**

The test creates an empty temporary asset directory, then:

1. Verifies `verify-assets.sh` fails.
2. Creates the exact ten expected filenames and verifies success.
3. Adds `unexpected.txt` and verifies failure.
4. Removes one expected DMG and verifies failure.

Run and expect RED because `verify-assets.sh` is absent:

```bash
bash scripts/release/test-packaging.sh
```

- [ ] **Step 2: Implement exact manifest validation**

Expected package assets:

```text
vibe-proxy_VERSION_windows_amd64.zip
vibe-proxy_VERSION_windows_arm64.zip
vibe-proxy_VERSION_darwin_amd64.tar.gz
vibe-proxy_VERSION_darwin_arm64.tar.gz
vibe-proxy_VERSION_darwin_amd64.dmg
vibe-proxy_VERSION_darwin_arm64.dmg
vibe-proxy_VERSION_linux_amd64.tar.gz
vibe-proxy_VERSION_linux_arm64.tar.gz
vibe-proxy_VERSION_linux_amd64.deb
vibe-proxy_VERSION_linux_arm64.deb
```

The script sorts expected and actual basenames and uses `diff -u` so failures
show missing and unexpected assets.

- [ ] **Step 3: Verify the manifest tests are GREEN**

```bash
bash scripts/release/test-packaging.sh
```

- [ ] **Step 4: Create the release workflow**

Workflow triggers:

```yaml
on:
  push:
    tags:
      - 'v*.*.*'
```

Jobs:

- `validate`: frontend `npm ci && npm run build`, embedded bundle diff check,
  Go test/vet/module verification, packaging contract tests, tag validation.
- `portable`: six-target matrix using `build-binary.sh` and
  `package-portable.sh`.
- `deb`: two-architecture matrix using `build-binary.sh` and
  `package-deb.sh`.
- `dmg`: two-architecture matrix on `macos-latest` using `build-binary.sh` and
  `package-dmg.sh`.
- `publish`: download all artifacts with merge enabled, run
  `verify-assets.sh`, generate sorted `SHA256SUMS`, and execute:

```bash
args=(release create "$GITHUB_REF_NAME" dist/* \
  --verify-tag --generate-notes --title "vibe-proxy $GITHUB_REF_NAME")
if [[ "$GITHUB_REF_NAME" == *-* ]]; then
  args+=(--prerelease)
fi
gh "${args[@]}"
```

Set workflow-level `permissions: contents: read`; set only the publish job to
`permissions: contents: write`.

- [ ] **Step 5: Validate the workflow**

```bash
actionlint .github/workflows/release.yml
bash scripts/release/test-packaging.sh
```

Expected: both pass.

- [ ] **Step 6: Commit**

```bash
git add .github/workflows/release.yml scripts/release
git commit -m "feat: automate cross-platform GitHub releases"
```

---

### Task 6: Pull-Request Gates and Installation Documentation

**Files:**
- Modify: `.github/workflows/test.yml`
- Create: `docs/release.md`
- Modify: `README.md`

**Interfaces:**
- CI runs packaging checks before release workflow changes reach `main`.
- Documentation is copied unchanged into release artifacts through `README.md`.

- [ ] **Step 1: Extend normal CI**

Add an Ubuntu release-package job that:

- installs Go `1.26.5`
- runs `bash scripts/release/test-packaging.sh`
- builds all six binaries with `build-binary.sh`
- packages all portable and DEB assets
- validates their names

Add a macOS job that:

- installs Go `1.26.5`
- builds both Darwin binaries
- creates and mounts both DMGs through the packaging test

- [ ] **Step 2: Write installation and RC-test documentation**

`docs/release.md` must include:

- architecture mapping (`x86_64 -> amd64`, `aarch64/arm64 -> arm64`)
- Windows ZIP commands
- macOS tarball and unsigned DMG instructions
- Linux tarball and DEB commands
- admin token and control-plane URL
- OpenAI and Anthropic endpoint URLs
- checksum verification commands for PowerShell, macOS, and Linux
- the complete RC platform checklist
- explicit statement that no system service or auto-start is installed

- [ ] **Step 3: Update README**

Add a “Download a Release” section above source-build instructions. Link to the
GitHub Releases page and `docs/release.md`, and label `v0.1.0-rc.1` as a testing
release rather than stable software.

- [ ] **Step 4: Validate docs and workflows**

```bash
git diff --check
python3 - <<'PY'
from pathlib import Path
bad = ["T" + "BD", "T" + "ODO", "FIX" + "ME", "X" + "XX"]
for name in ["docs/release.md", "README.md"]:
    text = Path(name).read_text()
    found = [word for word in bad if word in text]
    if found:
        raise SystemExit(f"{name}: placeholder markers: {found}")
print("documentation placeholder scan: ok")
PY
actionlint .github/workflows/test.yml .github/workflows/release.yml
```

Expected: no placeholders, whitespace errors, or workflow errors.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/test.yml README.md docs/release.md
git commit -m "docs: add cross-platform release installation guide"
```

---

### Task 7: Full Dry Run, Integration, and `v0.1.0-rc.1`

**Files:**
- Modify only if verification discovers defects in files owned by Tasks 1–6.

**Interfaces:**
- Consumes all prior tasks.
- Produces the pushed release branch, updated `develop`, main PR, and RC tag.

- [ ] **Step 1: Run full Go and frontend verification**

```bash
cd frontend
npm ci
npm run build
cd ..
git diff --exit-code -- internal/runtime/web/dist

CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go vet ./...
go mod verify
```

Repeat Go tests under both `1.25.12` and `1.26.5`.

- [ ] **Step 2: Run a complete local artifact dry run**

Create all ten package assets in a fresh temporary directory:

- six portable archives
- two DEBs
- two DMGs

Run:

```bash
asset_dir=$(mktemp -d)
binary_dir=$(mktemp -d)
for target in windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
  goos=${target%/*}
  goarch=${target#*/}
  extension=
  if [ "$goos" = windows ]; then
    extension=.exe
  fi
  binary="$binary_dir/vibe-proxy-${goos}-${goarch}${extension}"
  scripts/release/build-binary.sh v0.1.0-rc.1 "$goos" "$goarch" "$binary"
  scripts/release/package-portable.sh \
    v0.1.0-rc.1 "$goos" "$goarch" "$binary" "$asset_dir"
  if [ "$goos" = linux ]; then
    scripts/release/package-deb.sh \
      v0.1.0-rc.1 "$goarch" "$binary" "$asset_dir"
  fi
  if [ "$goos" = darwin ]; then
    scripts/release/package-dmg.sh \
      v0.1.0-rc.1 "$goarch" "$binary" "$asset_dir"
  fi
done
scripts/release/verify-assets.sh v0.1.0-rc.1 "$asset_dir"
(cd "$asset_dir" && shasum -a 256 * > SHA256SUMS)
```

Mount and inspect both DMGs, inspect both DEBs, list every archive member, and
run native `--version` smoke tests.

- [ ] **Step 3: Run final workflow and repository checks**

```bash
actionlint .github/workflows/test.yml .github/workflows/release.yml
bash -n scripts/release/*.sh
git diff --check
git status --short --branch
```

- [ ] **Step 4: Review and commit verification fixes**

If verification required changes, rerun the affected RED/GREEN cycle, inspect
`git status --short` and `git diff`, stage only the exact reviewed release files,
and commit them with message `fix: harden release artifact validation`. Do not
use `git add -A`.

- [ ] **Step 5: Merge to develop and push**

Fast-forward `codex/release-automation` into `develop`, rerun the full test suite
on the merged revision, and push `origin/develop`.

- [ ] **Step 6: Open and validate the main pull request**

Open `develop -> main` as a ready-for-review PR titled:

```text
Release vibe-proxy v0.1.0-rc.1
```

The body summarizes artifact formats, pure-Go portability, unsigned DMG status,
tests, and RC acceptance expectations. Wait for every GitHub check to pass.

- [ ] **Step 7: Merge main and create the annotated RC tag**

After PR checks pass:

```bash
git fetch origin
git tag -a v0.1.0-rc.1 origin/main -m "vibe-proxy v0.1.0-rc.1"
git push origin v0.1.0-rc.1
```

- [ ] **Step 8: Verify the GitHub Pre-release**

Confirm:

- workflow conclusion is success
- release is marked Pre-release, not draft
- all ten package files plus `SHA256SUMS` exist
- checksums verify after downloading one ZIP, one tarball, one DEB, and one DMG
- native binaries print `v0.1.0-rc.1`

- [ ] **Step 9: Record RC testing status**

Do not create stable `v0.1.0`. Report the release URL and hand the documented
Windows/macOS/Linux checklist to the user for physical cross-system testing.
