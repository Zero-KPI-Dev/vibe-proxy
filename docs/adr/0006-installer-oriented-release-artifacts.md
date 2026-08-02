# ADR-0006: Publish an installer-oriented cross-platform artifact matrix

## Status

Accepted

## Decision date

2026-08-02

## Recorded date

2026-08-02

## Context

The first cross-platform release workflow produced portable CLI archives, CLI-native packages, desktop portable bundles, and desktop installers across multiple architectures. In the private repository, 163 retained Actions artifacts occupied approximately 2038.83 MiB at the time of this decision. A release run completed its build jobs but the dependent publication job was blocked by the account's Actions usage state.

Most users need one obvious installation path per supported operating system. Windows and macOS have a native desktop shell; Linux is intentionally supported as a CLI service with the browser control plane. Retaining duplicate portable and installable outputs increases build time, storage use, release ambiguity, and acceptance scope without serving the selected product matrix.

The implementation of this accepted target is tracked on `codex/slim-release-installers`. Until that branch merges, documentation and workflows on `main` still describe the broader historical artifact matrix.

## Decision

Publish exactly six platform packages plus `SHA256SUMS` for each tagged release:

| Platform | Architecture | Package |
| --- | --- | --- |
| Windows desktop | AMD64 | NSIS setup executable |
| Windows desktop | ARM64 | NSIS setup executable |
| macOS desktop | Intel/AMD64 | application DMG |
| macOS desktop | Apple Silicon/ARM64 | application DMG |
| Linux CLI | AMD64 | Debian package |
| Linux CLI | ARM64 | Debian package |

Portable ZIP and tar archives, Windows desktop portable bundles, macOS CLI-only DMGs, and a Linux desktop GUI are not part of the default release matrix.

Tag-triggered release builds retain three packaging jobs: Windows desktop installers, macOS desktop DMGs, and Linux CLI DEBs. Intermediate job artifacts are transport between build and publish jobs, use one-day retention, and are deleted after download according to workflow retention. Durable downloads live on the GitHub Release rather than in Actions artifact storage.

Ordinary desktop CI continues compiling and validating Windows and macOS targets but does not upload successful installer artifacts. Build, architecture, startup, and packaging validation still fail CI when broken.

Both AMD64 and ARM64 remain supported on all three operating systems. Signing, notarization, additional Linux formats, and automated updates require later decisions.

## Consequences

### Positive

- Each supported operating system has one clear installation experience.
- The release workflow builds and retains fewer duplicate packages.
- Ordinary desktop CI keeps package validation without consuming long-lived artifact storage.
- GitHub Release assets, not ephemeral Actions artifacts, become the durable distribution channel.
- ARM64 support remains available for Windows, Apple Silicon, and Linux.
- The exact six-package manifest can be validated before checksums and publication.

### Negative

- Users cannot run the official default release directly from a portable ZIP or tar archive.
- Linux distribution support is limited to Debian-family package tooling unless users build from source.
- Windows and macOS users depend on the desktop packaging path and its WebView, signing, and OS-policy constraints.
- Release portability for restricted machines is reduced until an explicit additional distribution decision is made.

### Neutral

- Source builds remain possible and are not GitHub Release artifacts under this policy.
- Release candidates may remain unsigned with documented warnings; stable signing and macOS notarization remain follow-up work.
- Historical portable packaging helpers may remain internally tested if useful, but they are not part of the release contract.

## Alternatives considered

### Keep the broad portable and native matrix

This preserves maximum download choice but duplicates deliverables, increases storage and build fan-out, and makes the release page and acceptance matrix harder to understand.

### Keep CI packages with one-day retention

Even short retention can temporarily consume more than 50 MiB per busy CI run and provides little durable value because tagged releases rebuild their packages. CI should validate packages and discard successful outputs.

### Publish only raw binaries

This minimizes packaging work but removes Start Menu/uninstall integration on Windows, the application-bundle experience on macOS, and package-manager installation on Linux.

### Publish only AMD64

This reduces jobs further but drops architectures already present in the release contract, including Apple Silicon and ARM64 Linux and Windows.

### Make the repository public to change Actions allowances

Repository visibility is a product and source-distribution decision, not a billing workaround. It was rejected as unnecessary for solving the artifact-matrix problem.

### Require users to build locally

This eliminates hosted release artifacts but conflicts with the goal of making the proxy installable for users who do not have Go, Node, Wails, or native packaging toolchains.

## Security and operational considerations

- `SHA256SUMS` is generated only after the exact six-package manifest passes validation.
- Intermediate artifacts use minimal retention and are not treated as durable release assets.
- Removing uploads from ordinary CI does not remove compilation, startup, architecture, or package-content verification.
- Windows signing and macOS Developer ID signing/notarization are not silently implied. Unsigned release candidates require explicit SmartScreen and Gatekeeper guidance.
- Installers preserve application configuration and the SQLite database on ordinary uninstall unless a later, explicit data-removal policy says otherwise.
- Linux support means the CLI and loopback browser control plane; the release must not imply a supported native Linux GUI.
- Artifact cleanup frees retained storage, but billing and usage dashboards may update later and already accrued usage may remain for the current period.

## References

- [Current `main` Release Guide — historical broad matrix until implementation merges](../release.md)
- [Original Cross-Platform Release Design](../superpowers/specs/2026-07-30-cross-platform-release-design.md)
- [`e8971df` — design the slim installer release target](https://github.com/a448582655/vibe-proxy/blob/e8971df/docs/superpowers/specs/2026-08-02-slim-release-installers-design.md)
- [`733038c` — plan the installer-only release implementation](https://github.com/a448582655/vibe-proxy/commit/733038c)
- [`e138814` — implement installer-only release workflows](https://github.com/a448582655/vibe-proxy/commit/e138814)
