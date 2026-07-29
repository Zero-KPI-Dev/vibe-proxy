# Cross-Platform Release Design

**Date:** 2026-07-30
**Status:** Approved for implementation
**Target release:** `v0.1.0`

## 1. Context

`vibe-proxy` is now a pure-Go application with embedded frontend and OCR assets. Its
SQLite driver no longer requires CGO, so one source revision can be cross-compiled
for Windows, macOS, and Linux without a C toolchain.

The repository currently has no Git tags, GitHub Releases, or release workflow.
Users who cannot run Docker must clone the repository and install Go, which is
unnecessarily difficult for a local desktop-oriented tool.

## 2. Goals

The first release system will:

1. Publish ready-to-run binaries for Windows, macOS, and Linux.
2. Publish both Intel/AMD64 and ARM64 variants.
3. Provide portable archives on every platform.
4. Provide a DMG for macOS and a DEB package for Debian-family Linux systems.
5. Generate checksums and release notes automatically.
6. make the binary report its embedded version, commit, and build timestamp.
7. Block publication unless tests, frontend consistency checks, and every package
   build succeed.
8. Keep the runtime fully local and independent of Docker, Python, a system OCR
   installation, and CGO.

## 3. Non-goals

The first release will not include:

- Windows MSI or an interactive installer.
- macOS code signing, notarization, or automatic updates.
- RPM, Snap, Flatpak, Homebrew, or Winget packages.
- Linux systemd service installation or automatic startup.
- A macOS `.app` wrapper around the command-line binary.
- Container image publication.
- SBOM, SLSA provenance, or artifact signing.

These can be added after the portable release path is proven.

## 4. Release Source and Version Policy

- Formal releases are cut from `main`.
- Development continues on `develop`.
- A release is promoted through a `develop -> main` pull request so GitHub CI is
  the final gate on the exact revision being released.
- Tags use semantic versions in the form `vMAJOR.MINOR.PATCH`.
- The initial release is `v0.1.0`.
- A tag is created only after the release workflow has landed on `main`.
- Pushing a matching tag triggers `.github/workflows/release.yml`.
- A failed workflow publishes no partial GitHub Release. The failed run may be
  rerun after the cause is corrected.

## 5. Artifact Matrix

| Platform | Architecture | Portable artifact | Native package |
| --- | --- | --- | --- |
| Windows | AMD64 | `vibe-proxy_0.1.0_windows_amd64.zip` | — |
| Windows | ARM64 | `vibe-proxy_0.1.0_windows_arm64.zip` | — |
| macOS | AMD64 | `vibe-proxy_0.1.0_darwin_amd64.tar.gz` | matching `.dmg` |
| macOS | ARM64 | `vibe-proxy_0.1.0_darwin_arm64.tar.gz` | matching `.dmg` |
| Linux | AMD64 | `vibe-proxy_0.1.0_linux_amd64.tar.gz` | matching `.deb` |
| Linux | ARM64 | `vibe-proxy_0.1.0_linux_arm64.tar.gz` | matching `.deb` |

The GitHub Release also contains `SHA256SUMS`, covering every ZIP, tarball, DMG,
and DEB file.

## 6. Package Contents and User Experience

Every portable archive contains one top-level versioned directory with:

- `vibe-proxy` or `vibe-proxy.exe`
- `README.md`
- `LICENSE`
- `configs/bootstrap.yaml`
- `configs/simple.yaml`

The bootstrap command remains explicit and predictable:

```bash
VIBE_PROXY_ADMIN_TOKEN=change-me \
  ./vibe-proxy -config configs/bootstrap.yaml
```

PowerShell users use:

```powershell
$env:VIBE_PROXY_ADMIN_TOKEN = "change-me"
.\vibe-proxy.exe -config .\configs\bootstrap.yaml
```

The DMG is a distribution container rather than a signed application bundle. It
contains the binary, example configuration, license, and installation
instructions. The release notes and documentation must state that the first DMG
is unsigned and not notarized.

The DEB installs:

- the binary at `/usr/bin/vibe-proxy`
- documentation and examples below `/usr/share/doc/vibe-proxy/`

It does not create `/etc` configuration, users, services, or startup jobs. This
keeps package installation side-effect free and avoids silently creating weak
credentials.

## 7. Embedded Build Information

A small `internal/buildinfo` package will expose:

- `Version`, defaulting to `dev`
- `Commit`, defaulting to `unknown`
- `BuildDate`, defaulting to `unknown`

Release builds set these values with Go linker flags. The CLI accepts
`--version`, prints a stable one-line representation, and exits before reading a
configuration file or opening SQLite.

Example:

```text
vibe-proxy v0.1.0 (commit 9a4caf7, built 2026-07-30T12:00:00Z)
```

## 8. Workflow Architecture

The workflow uses GitHub-hosted runners and official GitHub actions:

1. **Validate**
   - Check out the tagged revision.
   - Install Go `1.26.5`.
   - Run the frontend production build and verify that the committed embedded
     bundle does not change.
   - Run `CGO_ENABLED=0 go test ./...`.
   - Run `CGO_ENABLED=0 go vet ./...`.
   - Verify modules.
   - Validate the semantic tag format.

2. **Build portable artifacts**
   - Build all six OS/architecture combinations with `CGO_ENABLED=0`.
   - Inject version, commit, and UTC build time.
   - Package Windows as ZIP and Unix targets as tarballs.
   - Run an executable smoke check for binaries native to the runner.

3. **Build native packages**
   - Use macOS runners and `hdiutil` to create both DMGs.
   - Use Ubuntu and `dpkg-deb` to create both DEBs.
   - Do not sign or notarize packages in `v0.1.0`.

4. **Publish**
   - Download all job artifacts into one staging directory.
   - Reject missing, unexpected, or duplicate filenames.
   - Generate `SHA256SUMS`.
   - Use the GitHub CLI and the workflow `GITHUB_TOKEN` to create a GitHub
     Release with automatically generated notes.
   - Publish only after every validation and build job succeeds.

The workflow declares `contents: write` only for the publish job. Build jobs use
read-only repository permissions.

## 9. Packaging Scripts

Platform-independent build and packaging behavior will live under
`scripts/release/` rather than being duplicated as large inline workflow blocks.
Scripts will:

- validate required version, OS, and architecture inputs
- construct deterministic artifact names
- create a clean staging directory
- copy only the documented package contents
- build portable archives, DEBs, and DMGs
- validate archive contents before upload

Scripts must fail on missing files, unknown architectures, duplicate output, or
an empty version.

## 10. Security and Compatibility

- Release binaries are built with `CGO_ENABLED=0`.
- The release workflow receives no provider API keys.
- The only write permission is GitHub Release publication through
  `GITHUB_TOKEN`.
- SQLite data remains compatible with databases created by prior
  `mattn/go-sqlite3` builds, including committed WAL data.
- No existing configuration or database is bundled into release artifacts.
- Example configuration contains only documented local development credentials.
- The macOS unsigned-package limitation is documented prominently.
- Checksums protect against accidental corruption, but are not a substitute for
  future artifact signing.

## 11. Testing and Acceptance Criteria

Implementation is accepted when:

1. Unit tests cover default and linker-injected build information formatting.
2. `vibe-proxy --version` succeeds without a configuration file.
3. `CGO_ENABLED=0 go test ./...` and `go vet ./...` pass under supported Go
   versions.
4. The frontend production build matches the committed embedded bundle.
5. All six binaries cross-compile.
6. Windows ZIP and Unix tarballs contain exactly the documented files.
7. Both DEBs pass `dpkg-deb --info` and content inspection.
8. Both DMGs can be created and mounted for content inspection on macOS.
9. `actionlint` accepts the test and release workflows.
10. A local release dry run produces the complete expected artifact manifest and
    valid checksums.
11. The `develop -> main` pull request passes repository CI.
12. The `v0.1.0` workflow creates one non-draft GitHub Release with all expected
    downloadable files.

## 12. Rollout

1. Implement and test the release system on `codex/release-automation`.
2. Commit the implementation in reviewable stages.
3. Merge the feature branch back into `develop` and push it.
4. Open a `develop -> main` pull request and wait for CI.
5. Merge the pull request.
6. Create and push annotated tag `v0.1.0` on the merged `main` commit.
7. Observe the release workflow through completion.
8. Download at least one archive, one DMG, and one DEB, verify checksums, and run
   a final binary smoke test.

## 13. Follow-up Work

Later releases may add:

- Apple Developer ID signing and notarization
- Windows Authenticode and MSI
- RPM, Homebrew, Winget, and container publication
- SBOM, provenance, and Sigstore signing
- an automatic updater
- an OS-native launcher or tray application
