# Slim Release Installers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the sixteen-file portable/native release with six AMD64/ARM64 installers, stop retaining ordinary desktop CI packages, free current Actions artifact storage, and republish `v0.1.0-rc.1`.

**Architecture:** The release workflow keeps the native Windows and macOS desktop packaging jobs plus the Linux DEB job, using one-day Actions artifacts only to transfer packages to the publisher. Existing shell contract tests define the exact six-file manifest and inspect both workflows, while ordinary desktop CI validates packages without uploading them.

**Tech Stack:** GitHub Actions YAML, Bash, PowerShell, NSIS, Wails v3, Debian packaging, GitHub CLI

## Global Constraints

- Windows and macOS provide native desktop installers; Linux provides the CLI service plus browser control plane.
- Support AMD64 and ARM64 on Windows, macOS, and Linux.
- Publish no portable ZIP or tar archive and no CLI-only macOS DMG.
- Publish exactly six packages plus `SHA256SUMS`.
- Do not make the repository public or enable paid Actions overages.
- Use one-day retention for temporary release artifacts and no artifact upload for ordinary desktop CI.
- Preserve run `30708162010` until the replacement release succeeds.

---

### Task 1: Free Current Actions Artifact Storage

**Files:**
- No repository files

**Interfaces:**
- Consumes: GitHub Actions artifacts for `a448582655/vibe-proxy`
- Produces: exactly the 14 artifacts belonging to run `30708162010` remain

- [ ] **Step 1: Resolve and validate the deletion set**

Query every non-expired repository artifact. Require 14 artifacts for run
`30708162010` and 149 artifacts for all other runs before deleting anything.

```powershell
gh api --paginate 'repos/a448582655/vibe-proxy/actions/artifacts?per_page=100' --slurp
```

- [ ] **Step 2: Delete only the validated old artifact IDs**

For each artifact in the already validated `$targets` collection, call:

```powershell
$targets | ForEach-Object {
  gh api --method DELETE "repos/a448582655/vibe-proxy/actions/artifacts/$($_.id)"
}
```

- [ ] **Step 3: Verify current storage**

Requery the artifacts and require count `14`, workflow run `30708162010`, and
approximately `173.40 MiB` total storage.

---

### Task 2: Define the Six-Installer Contract in Tests

**Files:**
- Modify: `scripts/release/test-packaging.sh`
- Modify: `scripts/release/test-desktop-packaging.sh`

**Interfaces:**
- Consumes: `scripts/release/verify-assets.sh`, `.github/workflows/release.yml`, `.github/workflows/desktop-build.yml`, and release documentation
- Produces: executable tests for the exact release manifest and workflow retention policy

- [ ] **Step 1: Replace the manifest fixture with six package names**

In `test_asset_manifest`, create only these files:

```text
vibe-proxy-desktop_0.1.0-rc.1_windows_amd64-setup.exe
vibe-proxy-desktop_0.1.0-rc.1_windows_arm64-setup.exe
vibe-proxy-desktop_0.1.0-rc.1_darwin_amd64.dmg
vibe-proxy-desktop_0.1.0-rc.1_darwin_arm64.dmg
vibe-proxy_0.1.0-rc.1_linux_amd64.deb
vibe-proxy_0.1.0-rc.1_linux_arm64.deb
```

Use the macOS ARM64 DMG as the missing-file rejection case.

- [ ] **Step 2: Change desktop and workflow expectations**

Add an absence assertion:

```bash
reject_text() {
  if grep -Fq -- "$2" "$repo_root/$1"; then
    fail "$1 unexpectedly contains: $2"
  fi
}
```

Require the six installer names in `docs/release.md`, require
`needs: [deb, desktop-windows, desktop-macos]`, require `retention-days: 1`,
and reject all of the following:

```text
actions/upload-artifact@v7                 in desktop-build.yml
package-desktop-portable.ps1               in release.yml
needs: [portable, deb, dmg, ...]            in release.yml
-portable.zip                              in docs/release.md
.tar.gz                                    in docs/release.md
```

Continue requiring `actions/upload-artifact@v7` in `release.yml` only.

- [ ] **Step 3: Run the focused tests and verify RED**

```bash
RELEASE_TEST_SCOPE=assets bash scripts/release/test-packaging.sh
bash scripts/release/test-desktop-packaging.sh
```

Expected: the asset test fails because `verify-assets.sh` still expects sixteen
files; the desktop test fails because both workflows and documentation still
describe portable artifacts.

---

### Task 3: Remove Unsupported Packaging Paths

**Files:**
- Delete: `scripts/release/package-portable.sh`
- Delete: `scripts/release/package-desktop-portable.ps1`
- Delete: `scripts/release/package-dmg.sh`
- Modify: `scripts/release/test-packaging.sh`
- Modify: `scripts/release/test-desktop-packaging.sh`
- Modify: `.github/workflows/test.yml`

**Interfaces:**
- Consumes: the installer-only contract from Task 2
- Produces: CI exercises only Linux DEBs and native desktop packages

- [ ] **Step 1: Remove obsolete test scopes and helper requirements**

Delete `test_portable`, `test_dmg`, their case branches, and requirements for
the two portable helpers. Make the `all` scope run `test_deb` when available
and always run `test_asset_manifest`.

- [ ] **Step 2: Remove portable and CLI-DMG CI work**

In `.github/workflows/test.yml`, rename the Linux packaging step to
`Cross-build Debian packages`, build only `linux/amd64` and `linux/arm64`, and
assert only these outputs:

```text
vibe-proxy_0.0.0-ci_linux_amd64.deb
vibe-proxy_0.0.0-ci_linux_arm64.deb
```

Delete the `release-packaging-macos` job because desktop macOS DMGs are already
built and inspected in `.github/workflows/desktop-build.yml`.

- [ ] **Step 3: Delete the three unused packaging helpers**

Remove the portable CLI archive helper, Windows desktop portable helper, and
CLI-only DMG helper. Native desktop packaging and `package-deb.sh` remain.

- [ ] **Step 4: Verify shell syntax and focused packaging behavior**

```bash
bash -n scripts/release/test-packaging.sh
bash -n scripts/release/test-desktop-packaging.sh
RELEASE_TEST_SCOPE=deb bash scripts/release/test-packaging.sh
```

Expected: syntax checks pass and both AMD64/ARM64 DEB contract cases pass.

---

### Task 4: Slim the Release and Desktop CI Workflows

**Files:**
- Modify: `.github/workflows/release.yml`
- Modify: `.github/workflows/desktop-build.yml`
- Modify: `scripts/release/verify-assets.sh`

**Interfaces:**
- Consumes: native setup EXEs, desktop DMGs, and CLI DEBs
- Produces: six packages downloaded into `dist` for the `publish` job

- [ ] **Step 1: Change the exact production manifest**

Make `verify-assets.sh` expect only:

```text
vibe-proxy-desktop_VERSION_darwin_amd64.dmg
vibe-proxy-desktop_VERSION_darwin_arm64.dmg
vibe-proxy-desktop_VERSION_windows_amd64-setup.exe
vibe-proxy-desktop_VERSION_windows_arm64-setup.exe
vibe-proxy_VERSION_linux_amd64.deb
vibe-proxy_VERSION_linux_arm64.deb
```

- [ ] **Step 2: Remove the release portable and CLI-DMG jobs**

Delete the complete `portable` and `dmg` jobs from `release.yml`. Keep `deb`,
`desktop-windows`, and `desktop-macos` with both architectures.

- [ ] **Step 3: Upload only exact installers for one day**

Remove the Windows `Package portable desktop app` step. Upload the exact package
path produced by each build and set every release upload to:

```yaml
retention-days: 1
```

Do not use a wildcard for the Windows upload because it could silently restore
portable assets later.

- [ ] **Step 4: Change publish dependencies**

Set:

```yaml
needs: [deb, desktop-windows, desktop-macos]
```

Keep exact manifest validation, checksum generation, draft creation, and final
publication unchanged.

- [ ] **Step 5: Stop ordinary desktop CI uploads**

Delete both `actions/upload-artifact@v7` steps from `desktop-build.yml`. Keep
all preceding compilation, architecture, startup, DMG, and installer checks.

- [ ] **Step 6: Verify GREEN for the production asset manifest**

```bash
RELEASE_TEST_SCOPE=assets bash scripts/release/test-packaging.sh
```

Expected: the asset manifest suite prints `release asset manifest tests: PASS`.
The combined desktop, workflow, and documentation contract remains RED until
Task 5 aligns the user-facing documentation.

---

### Task 5: Align User-Facing Release Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/release.md`
- Modify: `docs/desktop-rc-checklist.md`

**Interfaces:**
- Consumes: the six exact package names from Task 4
- Produces: installer-only download and acceptance instructions

- [ ] **Step 1: Replace the download table and wording**

Document only Windows setup EXEs, desktop DMGs, and Linux DEBs. State clearly
that Linux runs the CLI service and exposes the browser UI at
`http://127.0.0.1:8080/`.

- [ ] **Step 2: Remove obsolete portable and CLI-DMG instructions**

Remove archive extraction/layout text, portable WebView2 caveats, and macOS
CLI-only DMG instructions. Preserve setup, SmartScreen, Gatekeeper, checksum,
configuration, startup, and data-path guidance that still applies.

- [ ] **Step 3: Update acceptance wording**

Change the Windows ARM64 checklist from `Setup and portable launch succeed` to
`Setup and installed application launch succeed`. Update README to direct users
only to installers.

- [ ] **Step 4: Re-run documentation contracts**

```bash
bash scripts/release/test-desktop-packaging.sh
```

Expected: `desktop packaging contracts: PASS`.

---

### Task 6: Verify, Publish the PR, and Replace the RC Tag

**Files:**
- All files modified by Tasks 2 through 5

**Interfaces:**
- Consumes: complete installer-only implementation
- Produces: merged pull request and published `v0.1.0-rc.1` Release

- [ ] **Step 1: Run the focused release validation**

```bash
bash -n scripts/release/*.sh
bash scripts/release/test-packaging.sh
bash scripts/release/test-desktop-packaging.sh
```

Expected: all executable release contract tests pass; platform-specific tests
print only their documented skips on Windows.

- [ ] **Step 2: Run repository regression tests**

```powershell
$env:CGO_ENABLED = '0'
go test ./...
go vet ./...
npm --prefix frontend test
npm --prefix frontend run build
git diff --exit-code -- internal/runtime/web/dist
```

Expected: every command exits zero and generated frontend output is current.

- [ ] **Step 3: Review and commit the implementation**

```powershell
git diff --check
git status --short
git add .github/workflows README.md docs scripts/release
git commit -m "ci: publish installer-only releases"
```

- [ ] **Step 4: Push and open a ready pull request**

```powershell
git push -u origin codex/slim-release-installers
gh pr create --base main --head codex/slim-release-installers --title "ci: publish installer-only releases"
```

- [ ] **Step 5: Wait for required checks and merge**

Use `gh pr checks --watch` and merge only after every required check succeeds.

- [ ] **Step 6: Recreate the RC tag on the merge commit**

Delete the old remote and local `v0.1.0-rc.1` tag, create an annotated tag at
the new `origin/main`, and push it. Verify the new release workflow succeeds.

- [ ] **Step 7: Verify and clean up the published release**

Require the six installer assets plus `SHA256SUMS`, verify the release is a
published prerelease, and then delete the 14 artifacts retained from failed run
`30708162010`.
