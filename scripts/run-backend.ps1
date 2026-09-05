$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repoRoot
$runtimeDir = Join-Path $repoRoot '.local\vidlens-dev'
$serverBinary = Join-Path $runtimeDir 'vidlens-server.exe'
New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null

Write-Host "Building VidLens backend: $serverBinary" -ForegroundColor Cyan
& go build -o $serverBinary ./cmd/server
if ($LASTEXITCODE -ne 0) {
    throw "Backend build failed with code $LASTEXITCODE."
}

Write-Host "Starting VidLens backend: $serverBinary" -ForegroundColor Cyan
& $serverBinary
if ($LASTEXITCODE -ne 0) {
    Write-Host "Backend exited with code $LASTEXITCODE." -ForegroundColor Red
}
