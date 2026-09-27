$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath (Join-Path $repoRoot 'frontend')
$frontendPort = if ($env:VIDLENS_FRONTEND_PORT) { $env:VIDLENS_FRONTEND_PORT } else { '5173' }
Write-Host "VidLens frontend: npm run dev -- --port $frontendPort" -ForegroundColor Cyan
& npm.cmd run dev -- --port $frontendPort
if ($LASTEXITCODE -ne 0) {
    Write-Host "Frontend exited with code $LASTEXITCODE." -ForegroundColor Red
}
