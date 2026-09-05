$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repoRoot

function Test-ListeningPort([int]$Port) {
    return @(
        Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue
    ).Count -gt 0
}

$runtimeDir = Join-Path $repoRoot '.local\vidlens-dev'
New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null
$state = [ordered]@{
    backend_pid = $null
    backend_started_at = $null
    frontend_pid = $null
    frontend_started_at = $null
    created_at = (Get-Date).ToString('o')
}

if (Test-ListeningPort 8080) {
    Write-Host 'Backend port 8080 is already in use; skipping backend start.' -ForegroundColor Yellow
} else {
    $backend = Start-Process -FilePath 'powershell.exe' `
        -WorkingDirectory $repoRoot `
        -ArgumentList @('-NoLogo', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-NoExit', '-File', (Join-Path $PSScriptRoot 'run-backend.ps1')) `
        -PassThru
    $state.backend_pid = $backend.Id
    $state.backend_started_at = $backend.StartTime.ToString('o')
}

if (Test-ListeningPort 5173) {
    Write-Host 'Frontend port 5173 is already in use; skipping frontend start.' -ForegroundColor Yellow
} else {
    $frontend = Start-Process -FilePath 'powershell.exe' `
        -WorkingDirectory (Join-Path $repoRoot 'frontend') `
        -ArgumentList @('-NoLogo', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-NoExit', '-File', (Join-Path $PSScriptRoot 'run-frontend.ps1')) `
        -PassThru
    $state.frontend_pid = $frontend.Id
    $state.frontend_started_at = $frontend.StartTime.ToString('o')
}

$state | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $runtimeDir 'state.json') -Encoding UTF8

Write-Host ''
Write-Host 'VidLens development environment started.' -ForegroundColor Green
Write-Host 'Frontend: http://127.0.0.1:5173'
Write-Host 'Backend health: http://127.0.0.1:8080/healthz'
Write-Host 'Use make status to inspect status; use make stop to stop this app and the core containers.'
