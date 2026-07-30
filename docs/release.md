# Release Installation and Acceptance

GitHub Releases provide the same `vibe-proxy` server for Windows, macOS, and
Linux. The control plane is an embedded web application on every platform; it
opens in a browser at `http://127.0.0.1:8080/` rather than in a separate native
desktop window.

The first public build is the `v0.1.0-rc.1` release candidate. Use it for
cross-platform acceptance before treating it as a stable deployment.

## Download

Open the [GitHub Releases page](https://github.com/a448582655/vibe-proxy/releases)
and download the artifact matching the operating system and CPU:

| Platform | CPU shown by the OS | Artifact suffix |
| --- | --- | --- |
| Windows | x64 | `windows_amd64.zip` |
| Windows | ARM64 | `windows_arm64.zip` |
| macOS | Intel | `darwin_amd64.tar.gz` or `darwin_amd64.dmg` |
| macOS | Apple Silicon | `darwin_arm64.tar.gz` or `darwin_arm64.dmg` |
| Linux | x86_64 | `linux_amd64.tar.gz` or `linux_amd64.deb` |
| Linux | aarch64/arm64 | `linux_arm64.tar.gz` or `linux_arm64.deb` |

The portable archives contain one versioned directory with:

- the `vibe-proxy` executable
- `README.md` and `LICENSE`
- `configs/bootstrap.yaml`
- `configs/simple.yaml`

## Verify the Download

Download `SHA256SUMS` from the same release and compare the relevant line before
running an unsigned release candidate.

Linux:

```bash
sha256sum vibe-proxy_0.1.0-rc.1_linux_amd64.tar.gz
grep 'linux_amd64.tar.gz' SHA256SUMS
```

macOS:

```bash
shasum -a 256 vibe-proxy_0.1.0-rc.1_darwin_arm64.dmg
grep 'darwin_arm64.dmg' SHA256SUMS
```

Windows PowerShell:

```powershell
Get-FileHash .\vibe-proxy_0.1.0-rc.1_windows_amd64.zip -Algorithm SHA256
Select-String -Path .\SHA256SUMS -Pattern 'windows_amd64.zip'
```

## Windows

1. Extract the ZIP to a writable directory.
2. Open PowerShell in the extracted versioned directory.
3. Start the server:

```powershell
$env:VIBE_PROXY_ADMIN_TOKEN = 'choose-a-local-admin-token'
.\vibe-proxy.exe -config .\configs\bootstrap.yaml
```

Open `http://127.0.0.1:8080/`. Keep the PowerShell window open while using the
proxy. Stop it with `Ctrl+C`.

## macOS

The tarball and DMG contain the same server. The DMG is a convenient read-only
container, not a signed `.app`.

For the tarball:

```bash
tar -xzf vibe-proxy_0.1.0-rc.1_darwin_arm64.tar.gz
cd vibe-proxy_0.1.0-rc.1_darwin_arm64
export VIBE_PROXY_ADMIN_TOKEN='choose-a-local-admin-token'
./vibe-proxy -config configs/bootstrap.yaml
```

For the DMG, copy all files from the mounted image to a writable directory
before starting the server. This release candidate is not code-signed or
notarized. After verifying the checksum and repository source, macOS may require
removing the downloaded-file quarantine attribute:

```bash
xattr -d com.apple.quarantine ./vibe-proxy
```

Then use the same `export` and start command shown above. Open
`http://127.0.0.1:8080/` and stop the server with `Ctrl+C`.

## Linux Portable Archive

```bash
tar -xzf vibe-proxy_0.1.0-rc.1_linux_amd64.tar.gz
cd vibe-proxy_0.1.0-rc.1_linux_amd64
export VIBE_PROXY_ADMIN_TOKEN='choose-a-local-admin-token'
./vibe-proxy -config configs/bootstrap.yaml
```

Open `http://127.0.0.1:8080/` in a browser on the same machine. A headless Linux
host can expose the control plane through an SSH tunnel:

```bash
ssh -L 8080:127.0.0.1:8080 user@linux-host
```

Then open `http://127.0.0.1:8080/` on the local computer.

## Debian Package

Install the package:

```bash
sudo apt install ./vibe-proxy_0.1.0-rc.1_linux_amd64.deb
```

The package deliberately does not create a user, a system service, startup
tasks, or files under `/etc`. Copy an example config to a writable directory
and run it explicitly:

```bash
mkdir -p ~/.config/vibe-proxy
cp /usr/share/doc/vibe-proxy/examples/bootstrap.yaml \
  ~/.config/vibe-proxy/bootstrap.yaml
cd ~/.config/vibe-proxy
export VIBE_PROXY_ADMIN_TOKEN='choose-a-local-admin-token'
/usr/bin/vibe-proxy -config ./bootstrap.yaml
```

## First-Run Security

- `VIBE_PROXY_ADMIN_TOKEN` protects the local control plane and admin API.
- `configs/bootstrap.yaml` includes the development data-plane key
  `vibe-local-dev-key` so the first request can be tested.
- Create a new data-plane key under **Client Keys**, update clients, and remove
  the development key before using the proxy beyond isolated local testing.
- Provider credentials configured through the UI remain local to the selected
  configuration and database files.

By default the SQLite database is created as `vibe-proxy.db` in the process
working directory. Start `vibe-proxy` from a writable directory that should own
its runtime state.

## Release-Candidate Acceptance Checklist

Test the release candidate on physical or representative Windows, macOS, and
Linux systems:

1. Verify the downloaded checksum.
2. Run `vibe-proxy --version` and confirm the release tag and commit are shown.
3. Start with `configs/bootstrap.yaml` and open the control plane.
4. Add and test a provider without editing YAML.
5. Fetch and select provider models.
6. Create a new client key and call `/v1/models`.
7. Exercise OpenAI Chat, OpenAI Responses, and Anthropic Messages for a
   provider that supports them.
8. Restart from the same writable directory and confirm configuration and
   SQLite-backed request history persist.
9. On a suitable provider, test image input and inspect the OCR/Vision routing
   trace.
10. Report the OS version, CPU architecture, artifact name, and relevant logs
    for any failure.

Publish a stable `v0.1.0` only after this checklist passes on all three operating
systems. Fixes found during acceptance should produce another pre-release such
as `v0.1.0-rc.2`.
