# Slim Release Installers Design

## Context

The private repository currently retains 163 non-expired GitHub Actions
artifacts totaling 2,137,870,677 bytes (2038.83 MiB). The failed
`v0.1.0-rc.1` release run completed every build job, but GitHub refused to
start the dependent `publish` job because Actions usage was blocked by the
account billing or spending state.

The retained storage is split as follows:

| Source | Count | Size |
| --- | ---: | ---: |
| Desktop CI installers | 83 | 1073.48 MiB |
| Release portable archives | 36 | 358.70 MiB |
| Release Windows desktop bundles | 10 | 240.10 MiB |
| Release CLI DMGs | 12 | 133.41 MiB |
| Release macOS desktop DMGs | 10 | 131.19 MiB |
| Release DEBs | 12 | 101.95 MiB |

The application has a native desktop shell on Windows and macOS. Linux support
remains the CLI service plus the browser control plane; a Linux native GUI is
out of scope.

## Goals

- Publish only installable packages for Windows, macOS, and Linux.
- Continue supporting AMD64 and ARM64 on all three operating systems.
- Stop retaining ordinary CI packages that are not release deliverables.
- Keep the release manifest exact and covered by packaging contract tests.
- Free enough current Actions artifact storage for CI and release jobs to run
  in a private repository without making the source public.
- Recreate `v0.1.0-rc.1` from the merged implementation and publish it as a
  prerelease.

## Non-goals

- A native Linux desktop GUI.
- Portable ZIP or tar archives.
- A separate macOS CLI-only disk image.
- RPM, AppImage, MSI, Homebrew, Winget, signing, or notarization.
- Changing repository visibility or enabling paid Actions overages.

## Release Assets

Each release contains exactly six packages plus a generated checksum manifest:

| Platform | Architecture | Asset |
| --- | --- | --- |
| Windows desktop | AMD64 | `vibe-proxy-desktop_VERSION_windows_amd64-setup.exe` |
| Windows desktop | ARM64 | `vibe-proxy-desktop_VERSION_windows_arm64-setup.exe` |
| macOS desktop | Intel/AMD64 | `vibe-proxy-desktop_VERSION_darwin_amd64.dmg` |
| macOS desktop | Apple Silicon/ARM64 | `vibe-proxy-desktop_VERSION_darwin_arm64.dmg` |
| Linux CLI | AMD64 | `vibe-proxy_VERSION_linux_amd64.deb` |
| Linux CLI | ARM64 | `vibe-proxy_VERSION_linux_arm64.deb` |

`SHA256SUMS` is generated only after the six-package manifest passes.

## Workflow Changes

The tag-triggered release workflow will retain three build jobs:

1. `desktop-windows` builds and smoke-tests the NSIS setup executable for both
   architectures. It no longer creates a desktop portable ZIP and uploads the
   exact setup path instead of a wildcard.
2. `desktop-macos` builds and validates the desktop DMG for both architectures.
3. `deb` builds and inspects the Linux CLI DEB for both architectures.

The `portable` and CLI-only `dmg` jobs are removed. The `publish` job depends
only on the three retained package jobs, downloads their six artifacts, checks
the exact manifest, generates `SHA256SUMS`, creates a draft release, and then
publishes it.

Intermediate release artifacts use a one-day retention period. They are only a
transport between build and publish jobs; the final GitHub Release assets are
the durable downloads.

The ordinary desktop CI workflow will continue compiling and validating all
four desktop targets but will stop uploading the resulting installers. Build,
architecture, startup, and packaging failures still fail CI, while successful
packages are discarded with the runner.

## Tests and Documentation

- Update the exact asset-manifest contract to require only the six installers.
- Keep focused unit tests for portable packaging helpers if they remain useful
  as internal code, but remove portable artifacts from the release-level
  manifest and end-to-end release expectations.
- Update release documentation and the physical RC checklist so users are only
  directed to setup EXEs, desktop DMGs, and DEBs.
- Update README release wording to remove portable and generic CLI artifact
  choices.
- Validate YAML syntax, shell syntax, packaging contract tests, and the normal
  repository test suite before publishing the branch.

## Existing Artifact Cleanup

Before running new GitHub-hosted CI, retain the 14 artifacts from run
`30708162010` temporarily and delete the other 149 non-expired artifacts. This
reduces current repository artifact storage from 2038.83 MiB to approximately
173.40 MiB. Deletion stops future storage accrual and frees current storage,
although GitHub billing dashboards can take 6 to 12 hours to reflect it and
already accrued GB-hours remain in the current billing period.

After the replacement release succeeds, delete the retained failed-run
artifacts as well. Release assets remain available independently on the GitHub
Release.

## Delivery Sequence

1. Delete the 149 old Actions artifacts while retaining run `30708162010`.
2. Implement and locally verify the workflow, manifest, tests, and docs.
3. Push the branch and open a pull request.
4. Wait for all required checks and merge the pull request.
5. Move `v0.1.0-rc.1` from the previous merge commit to the new `main` commit.
6. Let the tag-triggered workflow build and publish the six-package prerelease.
7. Verify the Release asset names, checksums, and download availability.
8. Delete the retained artifacts from failed run `30708162010`.

## Alternatives Considered

### Keep CI artifacts with one-day retention

This preserves downloadable packages for every CI run but can still create
more than 50 MiB per run and temporarily exhaust a 500 MiB account during a
busy day. It provides little value because release packages are rebuilt from a
tag.

### Make the repository public

Public repositories can use standard GitHub-hosted runners without the private
repository allowance, but repository visibility is a product and source-code
decision. It is unnecessary for this release and should not be used only as a
billing workaround.

### Publish only AMD64

This minimizes jobs and storage further but drops native Apple Silicon, Windows
ARM64, and Linux ARM64 support already present in the release contract. Keeping
both architectures costs modestly more after portable and CI artifacts are
removed.
