[CmdletBinding()]
param(
    [string]$Version = "0.1.0",
    [string]$Output = "bin\fastv.exe",
    [string]$GoExe = "go",
    [string]$PnpmExe = "pnpm",
    [string]$PnpmScript = "",
    [switch]$SkipFrontend,
    [switch]$SkipChecks
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
function Invoke-Pnpm {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)
    if ($PnpmScript) {
        & $PnpmExe $PnpmScript @Arguments
    } else {
        & $PnpmExe @Arguments
    }
}
Push-Location $root

try {
    if (-not $SkipFrontend) {
        Write-Host "[build] frontend install"
        Invoke-Pnpm --dir frontend install
        if ($LASTEXITCODE -ne 0) { throw "frontend install failed" }
    }

    if (-not $SkipChecks) {
        Write-Host "[build] typecheck"
        Invoke-Pnpm --dir frontend run typecheck
        if ($LASTEXITCODE -ne 0) { throw "frontend typecheck failed" }
    }

    if (-not $SkipFrontend) {
        Write-Host "[build] frontend production bundle"
        Invoke-Pnpm --dir frontend run build
        if ($LASTEXITCODE -ne 0) { throw "frontend build failed" }
    }

    $commit = "dev"
    if (Get-Command git -ErrorAction SilentlyContinue) {
        $candidate = (& git rev-parse --short HEAD 2>$null)
        if ($LASTEXITCODE -eq 0 -and $candidate) { $commit = $candidate.Trim() }
    }
    $buildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

    $ldflags = @(
        "-s",
        "-w",
        "-H=windowsgui",
        "-X github.com/MardukPrime/Fast-V/internal/buildinfo.Version=$Version",
        "-X github.com/MardukPrime/Fast-V/internal/buildinfo.Commit=$commit",
        "-X github.com/MardukPrime/Fast-V/internal/buildinfo.BuildDate=$buildDate"
    ) -join " "

    New-Item -ItemType Directory -Force -Path (Split-Path -Parent (Join-Path $root $Output)) | Out-Null
    $outputPath = Join-Path $root $Output

    Write-Host "[build] go production build"
    & $GoExe build -tags production -trimpath -ldflags $ldflags -o $outputPath .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }

    # Verify the PE subsystem is WINDOWS_GUI (2), not WINDOWS_CUI (3).
    $bytes = [System.IO.File]::ReadAllBytes($outputPath)
    $peOffset = [BitConverter]::ToInt32($bytes, 0x3C)
    $optionalHeader = $peOffset + 24
    $subsystemOffset = $optionalHeader + 68
    $subsystem = [BitConverter]::ToUInt16($bytes, $subsystemOffset)
    if ($subsystem -ne 2) {
        throw "build produced PE subsystem $subsystem; expected 2 (WINDOWS_GUI)"
    }

    $file = Get-Item $outputPath
    $hash = (Get-FileHash $outputPath -Algorithm SHA256).Hash
    Write-Host ""
    Write-Host "[build] success"
    Write-Host "  output:    $($file.FullName)"
    Write-Host "  size:      $($file.Length) bytes"
    Write-Host "  version:   $Version"
    Write-Host "  commit:    $commit"
    Write-Host "  buildDate: $buildDate"
    Write-Host "  subsystem: WINDOWS_GUI"
    Write-Host "  sha256:    $hash"
}
finally {
    Pop-Location
}