param(
  [Parameter(Mandatory = $true)][string]$Version,
  [Parameter(Mandatory = $true)][ValidateSet("amd64", "arm64")][string]$Architecture,
  [Parameter(Mandatory = $true)][string]$Binary,
  [Parameter(Mandatory = $true)][string]$OutputDirectory
)

$ErrorActionPreference = "Stop"
if ($Version -notmatch '^v\d+\.\d+\.\d+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$') {
  throw "Invalid release version: $Version"
}
if (-not (Test-Path -LiteralPath $Binary -PathType Leaf)) {
  throw "Desktop binary does not exist: $Binary"
}

$versionWithoutV = $Version.Substring(1)
$artifact = Join-Path $OutputDirectory "vibe-proxy-desktop_${versionWithoutV}_windows_${Architecture}-portable.zip"
$stage = Join-Path ([System.IO.Path]::GetTempPath()) ("vibe-proxy-desktop-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $OutputDirectory, $stage | Out-Null
try {
  Copy-Item -LiteralPath $Binary -Destination (Join-Path $stage "vibe-proxy-desktop.exe")
  Copy-Item -LiteralPath (Join-Path $PSScriptRoot "../../LICENSE") -Destination (Join-Path $stage "LICENSE")
  @"
Vibe Proxy Desktop $Version

Run vibe-proxy-desktop.exe. The native window starts the local gateway and
stores configuration under %LOCALAPPDATA%\vibe-proxy.
"@ | Set-Content -LiteralPath (Join-Path $stage "README.txt") -Encoding UTF8
  if (Test-Path -LiteralPath $artifact) {
    Remove-Item -LiteralPath $artifact -Force
  }
  Compress-Archive -Path (Join-Path $stage "*") -DestinationPath $artifact
} finally {
  Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue
}
Write-Output $artifact
