$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repoRoot
Write-Host 'VidLens backend: go run ./cmd/server' -ForegroundColor Cyan
& go run ./cmd/server
if ($LASTEXITCODE -ne 0) {
    Write-Host "Backend exited with code $LASTEXITCODE." -ForegroundColor Red
}
