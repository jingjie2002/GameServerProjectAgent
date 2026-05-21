param(
    [int]$Port = 18089
)

$ErrorActionPreference = "Stop"

function Assert-UnderPath {
    param(
        [string]$Path,
        [string]$Root
    )
    $resolvedPath = Resolve-Path -LiteralPath $Path -ErrorAction SilentlyContinue
    if ($null -eq $resolvedPath) {
        return
    }
    $resolvedRoot = Resolve-Path -LiteralPath $Root
    if (-not $resolvedPath.Path.StartsWith($resolvedRoot.Path)) {
        throw "refuse to clean outside smoke root: $($resolvedPath.Path)"
    }
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptDir
$workspace = Split-Path -Parent $repoRoot
$tmpRoot = Join-Path $repoRoot "tmp"
$outPath = Join-Path $tmpRoot "dashboard-smoke.out"
$errPath = Join-Path $tmpRoot "dashboard-smoke.err"
$gsaPath = Join-Path $repoRoot "bin\gsa.exe"

if (-not (Test-Path -LiteralPath $gsaPath)) {
    throw "missing $gsaPath; run go build -o bin\gsa.exe ./cmd/gsa first"
}
if (-not (Test-Path -LiteralPath $tmpRoot)) {
    New-Item -ItemType Directory -Path $tmpRoot | Out-Null
}
foreach ($file in @($outPath, $errPath)) {
    if (Test-Path -LiteralPath $file) {
        Assert-UnderPath -Path $file -Root $tmpRoot
        Remove-Item -LiteralPath $file -Force
    }
}

$oldHome = $env:GSA_HOME
$oldWorkspace = $env:GSA_WORKSPACE
$env:GSA_HOME = $repoRoot
$env:GSA_WORKSPACE = $workspace

$success = $false
$process = $null
try {
    $startInfo = [System.Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $gsaPath
    $startInfo.WorkingDirectory = $repoRoot
    $startInfo.Arguments = "dashboard --host 127.0.0.1 --port $Port"
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    $process = [System.Diagnostics.Process]::Start($startInfo)

    $ready = $false
    for ($i = 0; $i -lt 30; $i++) {
        try {
            $health = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$Port/healthz" -TimeoutSec 2
            if ($health.StatusCode -eq 200) {
                $ready = $true
                break
            }
        } catch {
            Start-Sleep -Milliseconds 300
        }
    }
    if (-not $ready) {
        throw "dashboard did not become ready"
    }

    $status = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$Port/api/status" -TimeoutSec 5
    $index = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$Port/" -TimeoutSec 5
    if ($status.Content -notmatch '"project_count"') {
        throw "status json missing project_count"
    }
    if ($index.Content -notmatch "GameServerProjectAgent") {
        throw "index missing title"
    }

    $success = $true
    Write-Output "DASHBOARD_SMOKE_OK"
} finally {
    if ($process -and -not $process.HasExited) {
        $process.Kill()
        $process.WaitForExit(5000) | Out-Null
    }
    if ($process) {
        [System.IO.File]::WriteAllText($outPath, $process.StandardOutput.ReadToEnd())
        [System.IO.File]::WriteAllText($errPath, $process.StandardError.ReadToEnd())
    }
    $env:GSA_HOME = $oldHome
    $env:GSA_WORKSPACE = $oldWorkspace

    if ($success) {
        foreach ($file in @($outPath, $errPath)) {
            if (Test-Path -LiteralPath $file) {
                Assert-UnderPath -Path $file -Root $tmpRoot
                Remove-Item -LiteralPath $file -Force
            }
        }
        Write-Output "cleaned: $tmpRoot\dashboard-smoke.*"
    } else {
        Write-Output "smoke files preserved:"
        Write-Output "  $outPath"
        Write-Output "  $errPath"
    }
}
