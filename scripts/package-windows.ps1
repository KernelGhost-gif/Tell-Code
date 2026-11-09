param(
    [Parameter(Mandatory=$true)][string]$Version,
    [Parameter(Mandatory=$true)][string]$BinaryPath,
    [string]$OutputDirectory = "dist"
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$out = Join-Path $root $OutputDirectory
New-Item -ItemType Directory -Force -Path $out | Out-Null
$binary = Join-Path $out "tell-windows-amd64.exe"
Copy-Item -Force $BinaryPath $binary

$iscc = Get-Command "ISCC.exe" -ErrorAction SilentlyContinue
if (-not $iscc) {
    $programFilesX86 = $env:ProgramFiles + " (x86)"
    $candidate = Join-Path $programFilesX86 "Inno Setup 6\ISCC.exe"
    if (Test-Path $candidate) { $iscc = Get-Item $candidate }
}
if (-not $iscc) { throw "Inno Setup 6 compiler (ISCC.exe) is required." }

$iss = Join-Path $root "packaging\windows\tell.iss"
& $iscc.Source "/DAppVersion=$Version" "/DSourceDir=$out" $iss
if ($LASTEXITCODE -ne 0) { throw "Inno Setup failed with exit code $LASTEXITCODE" }
