# Release Installation and Acceptance

GitHub Releases provide both the `vibe-proxy` CLI server and a native **Vibe
Proxy Desktop** shell for Windows and macOS. Linux uses the CLI plus the
browser-based control plane at `http://127.0.0.1:8080/`.

The first public build is the `v0.1.0-rc.1` release candidate. Use it for
cross-platform acceptance before treating it as a stable deployment.

## Download

Open the [GitHub Releases page](https://github.com/a448582655/vibe-proxy/releases)
and download the artifact matching the operating system and CPU:

| Platform | CPU shown by the OS | Artifact suffix |
| --- | --- | --- |
| Windows desktop | x64 | `vibe-proxy-desktop_0.1.0-rc.1_windows_amd64-setup.exe` or `vibe-proxy-desktop_0.1.0-rc.1_windows_amd64-portable.zip` |
| Windows desktop | ARM64 | `vibe-proxy-desktop_0.1.0-rc.1_windows_arm64-setup.exe` or `vibe-proxy-desktop_0.1.0-rc.1_windows_arm64-portable.zip` |
| macOS desktop | Intel | `vibe-proxy-desktop_0.1.0-rc.1_darwin_amd64.dmg` |
| macOS desktop | Apple Silicon | `vibe-proxy-desktop_0.1.0-rc.1_darwin_arm64.dmg` |
| Windows CLI | x64 / ARM64 | `windows_amd64.zip` / `windows_arm64.zip` |
| macOS CLI | Intel / Apple Silicon | `darwin_amd64.tar.gz` / `darwin_arm64.tar.gz` |
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

Run the desktop setup EXE or extract the desktop portable ZIP and launch
`vibe-proxy-desktop.exe`. The first launch asks you to create a management
password for browser access to the local control plane; the native window signs
in automatically. This is not a provider API key or the client key used by
agents. On the first window close, choose whether future closes should exit or
minimise to the tray. Reset the choice from **Settings → Desktop Application**
or the tray menu. Provider configuration, SQLite data, password hash,
preferences and logs live under:

```text
%LOCALAPPDATA%\vibe-proxy
```

The Settings page can import `config.yaml` and open the data directory. If the
native WebView cannot open, the proxy and tray remain running; use **Open in
Browser** or **Open Logs Folder** from the tray. Before first-run setup, that
tray action grants a one-time setup session; later browser windows show the
management-password login page.

The desktop window requires the Microsoft Edge **WebView2 Runtime**. The setup
installer tries to install it automatically, but that step needs network access.
On restricted or offline networks, install the matching Evergreen Standalone
Runtime from Microsoft's [WebView2 download page](https://developer.microsoft.com/en-us/microsoft-edge/webview2?form=MA13FL)
before launching the app. The portable ZIP does not install WebView2 for you.
If a double-click still produces no window, inspect:

```text
%LOCALAPPDATA%\vibe-proxy\logs\vibe-proxy.log
```

The desktop executable now writes native-shell startup failures to this file
and shows a visible Windows error dialog instead of failing silently.

Unsigned RC installers may trigger Microsoft SmartScreen. Verify the checksum
and repository source, then use **More info → Run anyway** only when you accept
the unsigned test build.

For the CLI ZIP, open PowerShell in the extracted directory and run:

```powershell
$env:VIBE_PROXY_ADMIN_TOKEN = 'choose-a-local-admin-token'
.\vibe-proxy.exe -config .\configs\bootstrap.yaml
```

Open `http://127.0.0.1:8080/`. Keep the PowerShell window open while using the
proxy. Stop it with `Ctrl+C`.

## Runtime Endpoints

After configuring a provider and client key in the control plane, local agents
can use these stable data-plane endpoints:

```text
OpenAI Chat Completions  http://127.0.0.1:8080/v1/chat/completions
OpenAI Responses         http://127.0.0.1:8080/v1/responses
Anthropic Messages       http://127.0.0.1:8080/anthropic/v1/messages
Anthropic short alias    http://127.0.0.1:8080/v1/messages
Model discovery          http://127.0.0.1:8080/v1/models
```

Authenticate data-plane requests with `Authorization: Bearer <client-key>`.
The Anthropic endpoint also accepts the client key in `x-api-key`.

## macOS

Open the desktop DMG, drag **Vibe Proxy.app** to the Applications link, and
launch it. The first launch asks you to create a management password for
browser access; the native window itself signs in automatically. Desktop state
is stored under:

```text
~/Library/Application Support/vibe-proxy
```

The menu-bar icon restores the window, opens the browser control plane or logs,
copies agent base URLs and exits cleanly. Regular browser windows authenticate
with the management password and keep it out of URLs and localStorage. Before
first-run setup only, **Open in Browser** can issue a one-time recovery session
if the native WebView is unavailable. The first-close choice can be reset from
Settings or the menu-bar item.

Unsigned RCs may be blocked by Gatekeeper. After verifying the checksum and
source, Control-click the app and choose **Open**, or remove quarantine from the
copied app:

```bash
xattr -dr com.apple.quarantine "/Applications/Vibe Proxy.app"
```

The CLI tarball and CLI DMG remain available for terminal use.

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
tasks, auto-start entries, or files under `/etc`. No release artifact installs
an operating-system service in this phase. Copy an example config to a writable
directory and run it explicitly:

```bash
mkdir -p ~/.config/vibe-proxy
cp /usr/share/doc/vibe-proxy/examples/bootstrap.yaml \
  ~/.config/vibe-proxy/bootstrap.yaml
cd ~/.config/vibe-proxy
export VIBE_PROXY_ADMIN_TOKEN='choose-a-local-admin-token'
/usr/bin/vibe-proxy -config ./bootstrap.yaml
```

## First-Run Security

- The desktop app asks for a management password on first launch and persists
  only its Argon2id hash in `auth.json`.
- The native window uses a process-local random internal admin token and
  one-time bootstrap sessions; it does not persist or ask the user to copy that
  token.
- Regular browsers sign in with the management password and receive an
  HttpOnly, same-origin session cookie. The password is not stored in the
  browser.
- To reset a forgotten desktop management password, fully exit Vibe Proxy,
  delete `auth.json` from the application data directory, and relaunch. This
  does not delete providers, client keys, configuration, or request history.
- CLI installs use `VIBE_PROXY_ADMIN_TOKEN` to protect the control plane and
  admin API.
- `configs/bootstrap.yaml` includes the development data-plane key
  `vibe-local-dev-key` so the first request can be tested.
- Create a new data-plane key under **Client Keys**, update clients, and remove
  the development key before using the proxy beyond isolated local testing.
- Provider credentials configured through the UI remain local to the selected
  configuration and database files.
- If the desktop app cannot reach models.dev on a restricted network, open
  **Settings → Model capability catalog** and configure an HTTP/HTTPS proxy.
  Authenticated proxy URLs are supported and credentials are not echoed after
  saving. Fully offline users can import the official `api.json` from the same
  card.

By default the SQLite database is created as `vibe-proxy.db` in the process
working directory. Start `vibe-proxy` from a writable directory that should own
its runtime state.

## Release-Candidate Acceptance Checklist

Test the release candidate on physical or representative Windows, macOS, and
Linux systems:

1. Verify the downloaded checksum.
2. Run `vibe-proxy --version` and confirm the release tag and commit are shown.
3. Start the desktop app, create the first-run management password, and confirm
   the native window enters the control plane.
4. Open `http://127.0.0.1:8080/` in a regular browser, sign in with the
   management password, and confirm that an incorrect password is rejected.
5. Add and test a provider without editing YAML.
6. Fetch and select provider models.
7. Create a new client key and call `/v1/models`.
8. Exercise OpenAI Chat, OpenAI Responses, and Anthropic Messages for a
   provider that supports them.
9. Restart from the same writable directory and confirm the management
   password, configuration and
   SQLite-backed request history persist.
10. On a suitable provider, test image input and inspect the OCR/Vision routing
   trace.
11. Report the OS version, CPU architecture, artifact name, and relevant logs
    for any failure.

Stable desktop publication requires code signing and macOS notarization, unless
a maintainer explicitly records an exception. Unchecked physical tests block a
stable desktop release but do not block an explicitly unsigned pre-release
candidate. See [`desktop-rc-checklist.md`](desktop-rc-checklist.md).
