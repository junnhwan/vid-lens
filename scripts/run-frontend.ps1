$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath (Join-Path $repoRoot 'frontend')
Write-Host 'VidLens frontend: npm run dev -- -p 5173' -ForegroundColor Cyan
& npm.cmd run dev -- -p 5173
if ($LASTEXITCODE -ne 0) {
    Write-Host "Frontend exited with code $LASTEXITCODE." -ForegroundColor Red
}
