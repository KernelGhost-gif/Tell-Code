[CmdletBinding()]
param(
    [string]$LLVMRoot = $env:LLVM_ROOT,
    [string]$BuildRoot = $env:TELL_BUILD_ROOT,
    [int]$Jobs = [Environment]::ProcessorCount
)

$ErrorActionPreference = "Stop"

$repositoryRoot = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($LLVMRoot)) {
    $llcCommand = Get-Command "llc.exe" -ErrorAction SilentlyContinue
    if (-not $llcCommand) {
        $llcCommand = Get-Command "llc" -ErrorAction SilentlyContinue
    }
    if (-not $llcCommand) {
        throw "LLVM was not found. Install LLVM or set LLVM_ROOT to its installation directory."
    }
    $LLVMRoot = Split-Path -Parent (Split-Path -Parent $llcCommand.Source)
}

$LLVMRoot = (Resolve-Path $LLVMRoot).Path
if (-not (Get-Command "link.exe" -ErrorAction SilentlyContinue)) {
    throw "MSVC link.exe was not found. Run this script from a Visual Studio 2022 Developer PowerShell session."
}
$llvmConfig = Join-Path $LLVMRoot "lib\cmake\llvm\LLVMConfig.cmake"
$llcPath = Join-Path $LLVMRoot "bin\llc.exe"
$clangPath = Join-Path $LLVMRoot "bin\clang.exe"

foreach ($requiredPath in @($llvmConfig, $llcPath, $clangPath)) {
    if (-not (Test-Path $requiredPath -PathType Leaf)) {
        throw "Required LLVM file was not found: $requiredPath"
    }
}

if ($Jobs -lt 1) {
    throw "Jobs must be a positive integer."
}

if ([string]::IsNullOrWhiteSpace($BuildRoot)) {
    $BuildRoot = Join-Path $repositoryRoot "build\tellc-windows-x64"
}
if (-not [System.IO.Path]::IsPathRooted($BuildRoot)) {
    $BuildRoot = Join-Path $repositoryRoot $BuildRoot
}
$BuildRoot = [System.IO.Path]::GetFullPath($BuildRoot)

$env:PATH = "$(Join-Path $LLVMRoot 'bin');$env:PATH"
$llcCMakePath = $llcPath.Replace("\", "/")

Write-Host "LLVM: $LLVMRoot"
Write-Host "Build directory: $BuildRoot"
Write-Host "Parallel jobs: $Jobs"

& cmake -S (Join-Path $repositoryRoot "TellC") -B $BuildRoot -G "Visual Studio 17 2022" -A x64 "-DLLVM_DIR=$(Join-Path $LLVMRoot 'lib\cmake\llvm')" "-DTELL_LLC_PATH=$llcCMakePath"
if ($LASTEXITCODE -ne 0) {
    throw "CMake configuration failed with exit code $LASTEXITCODE."
}

& cmake --build $BuildRoot --config Release --parallel $Jobs
if ($LASTEXITCODE -ne 0) {
    throw "TellC build failed with exit code $LASTEXITCODE."
}

& ctest --test-dir $BuildRoot -C Release --output-on-failure
if ($LASTEXITCODE -ne 0) {
    throw "TellC tests failed with exit code $LASTEXITCODE."
}

$compiler = Join-Path $BuildRoot "Release\tellc.exe"
if (-not (Test-Path $compiler -PathType Leaf)) {
    throw "The native Windows compiler was not produced: $compiler"
}

$smokeOutput = Join-Path $BuildRoot "tellc-windows-smoke.exe"
$smokeSource = Join-Path $repositoryRoot "TellC\examples\factorial.tell"
& $compiler $smokeSource -o $smokeOutput
if ($LASTEXITCODE -ne 0) {
    throw "TellC failed to compile the Windows smoke-test program."
}

$smokeResult = & $smokeOutput
if ($LASTEXITCODE -ne 0 -or (($smokeResult -join [Environment]::NewLine).Trim() -ne "120")) {
    throw "The generated Windows executable did not produce the expected result. Output: $($smokeResult -join ', ')"
}

Write-Host "Native Windows TellC compiler: $compiler"
Write-Host "Generated Windows executable smoke test passed."
