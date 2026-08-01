[CmdletBinding(DefaultParameterSetName = "Installer")]
param(
    [Parameter(Mandatory = $true, ParameterSetName = "Installer")]
    [string]$Installer,

    [Parameter(Mandatory = $true, ParameterSetName = "Binary")]
    [string]$Binary,

    [int]$TimeoutSeconds = 60
)

$ErrorActionPreference = "Stop"
$installerPath = $null
$uninstallerPath = $null
if ($PSCmdlet.ParameterSetName -eq "Installer") {
    $installerPath = (Resolve-Path $Installer).Path
    $installDirectory = Join-Path $env:LOCALAPPDATA "Programs\Vibe Proxy"
    $binaryPath = Join-Path $installDirectory "vibe-proxy-desktop.exe"
    $uninstallerPath = Join-Path $installDirectory "uninstall.exe"
}
else {
    $binaryPath = (Resolve-Path $Binary).Path
}
$primaryLog = Join-Path $env:LOCALAPPDATA "vibe-proxy\logs\vibe-proxy.log"
$fallbackLog = Join-Path $env:TEMP "vibe-proxy-startup.log"
$desktopProcess = $null
$failure = $null
$diagnosticLengths = @{}

foreach ($candidate in @($primaryLog, $fallbackLog)) {
    $diagnosticLengths[$candidate] = if (Test-Path $candidate) {
        (Get-Item $candidate).Length
    }
    else {
        0
    }
}

function Write-StartupDiagnostics {
    foreach ($candidate in @($primaryLog, $fallbackLog)) {
        if (Test-Path $candidate) {
            Write-Host "Startup diagnostic: $candidate"
            Get-Content $candidate -Tail 100 | Write-Host
        }
    }
}

function Test-NewStartupDiagnostics {
    foreach ($candidate in @($primaryLog, $fallbackLog)) {
        if ((Test-Path $candidate) -and (Get-Item $candidate).Length -gt $diagnosticLengths[$candidate]) {
            return $true
        }
    }
    return $false
}

try {
    if ($PSCmdlet.ParameterSetName -eq "Installer") {
        $installProcess = Start-Process -FilePath $installerPath -ArgumentList "/S" -Wait -PassThru
        if ($installProcess.ExitCode -ne 0) {
            throw "Desktop installer exited with code $($installProcess.ExitCode)."
        }
        if (-not (Test-Path $binaryPath)) {
            throw "Installed desktop executable was not found at $binaryPath."
        }
    }

    $desktopProcess = Start-Process -FilePath $binaryPath -PassThru
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $healthReady = $false
    $windowReady = $false
    $lastProbeError = "not attempted"

    while ((Get-Date) -lt $deadline) {
        $desktopProcess.Refresh()
        if ($desktopProcess.HasExited) {
            throw "Installed desktop process exited early with code $($desktopProcess.ExitCode)."
        }

        if (-not $healthReady) {
            try {
                $response = Invoke-WebRequest -Uri "http://127.0.0.1:8080/healthz" -UseBasicParsing -TimeoutSec 2
                $healthReady = $response.StatusCode -eq 200 -and $response.Content -match '"ok"\s*:\s*true'
            }
            catch {
                $lastProbeError = $_.Exception.Message
            }
        }

        if ($desktopProcess.MainWindowHandle -ne 0) {
            $windowReady = $true
        }
        if ($healthReady -and $windowReady) {
            break
        }
        Start-Sleep -Milliseconds 500
    }

    if (-not $healthReady) {
        throw "Desktop gateway did not become healthy within $TimeoutSeconds seconds. Last probe error: $lastProbeError"
    }
    if (-not $windowReady) {
        throw "Desktop process became healthy but did not create a native main window within $TimeoutSeconds seconds."
    }
    if (Test-NewStartupDiagnostics) {
        throw "Desktop process emitted a startup diagnostic instead of opening a usable main window."
    }

    Write-Host "Windows desktop startup smoke test passed."
}
catch {
    $failure = $_
    Write-StartupDiagnostics
}
finally {
    if ($null -ne $desktopProcess) {
        $desktopProcess.Refresh()
        if (-not $desktopProcess.HasExited) {
            Stop-Process -Id $desktopProcess.Id -Force -ErrorAction SilentlyContinue
            $desktopProcess.WaitForExit(5000) | Out-Null
        }
    }
    if ($null -ne $uninstallerPath -and (Test-Path $uninstallerPath)) {
        $uninstallProcess = Start-Process -FilePath $uninstallerPath -ArgumentList "/S" -Wait -PassThru
        if ($uninstallProcess.ExitCode -ne 0 -and $null -eq $failure) {
            $failure = "Desktop uninstaller exited with code $($uninstallProcess.ExitCode)."
        }
    }
}

if ($null -ne $failure) {
    throw $failure
}
