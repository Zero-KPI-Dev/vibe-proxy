# Desktop Release Candidate Acceptance

Record tester, hardware/OS version, artifact checksum, date, result and issue
links for each platform. Unchecked physical tests block a stable desktop
release, but do not block publishing an explicitly unsigned pre-release
candidate. Stable publication also requires signing/notarization or a recorded
maintainer exception.

## Windows 10/11 AMD64

- Tester / date:
- Hardware and OS:
- Artifact and SHA-256:
- Result / issue links:
- [ ] Setup EXE installs, creates Start Menu and uninstall entries
- [ ] Installed `vibe-proxy-desktop.exe` launches from the Start Menu
- [ ] Production app opens without a console window
- [ ] First close offers tray, exit and cancel behavior
- [ ] Tray restores the window and preference persists after restart
- [ ] Text request succeeds
- [ ] Image request shows OCR/Vision routing
- [ ] Browser fallback and logs action work
- [ ] Uninstall preserves `%LOCALAPPDATA%\vibe-proxy`
- [ ] SmartScreen guidance is accurate

## Windows ARM64

- Tester / date:
- Hardware and OS:
- Artifact and SHA-256:
- Result / issue links:
- [ ] PE architecture is ARM64
- [ ] Setup and installed application launch succeed
- [ ] Tray restore and graceful exit succeed
- [ ] Text and image request smoke tests pass

## macOS Apple Silicon

- Tester / date:
- Hardware and macOS:
- Artifact and SHA-256:
- Result / issue links:
- [ ] DMG mounts with `Vibe Proxy.app` and Applications link
- [ ] App launches and menu-bar icon works
- [ ] First-close choice and preference persistence work
- [ ] Text and image/OCR/Vision requests pass
- [ ] Data and logs are under `~/Library/Application Support/vibe-proxy`
- [ ] Graceful exit releases port 8080
- [ ] Gatekeeper guidance is accurate

## macOS Intel

- Tester / date:
- Hardware and macOS:
- Artifact and SHA-256:
- Result / issue links:
- [ ] Mach-O architecture is x86_64
- [ ] DMG, app launch and menu-bar behavior work
- [ ] Close preference persists
- [ ] Text and image request smoke tests pass
- [ ] Graceful exit releases port 8080
