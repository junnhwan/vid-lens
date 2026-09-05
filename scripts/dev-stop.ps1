$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repoRoot
$statePath = Join-Path $repoRoot '.local\vidlens-dev\state.json'

if (Test-Path -LiteralPath $statePath) {
    $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    foreach ($item in @(
        @{ Pid = 'backend_pid'; Started = 'backend_started_at'; Label = 'backend' },
        @{ Pid = 'frontend_pid'; Started = 'frontend_started_at'; Label = 'frontend' }
    )) {
        $pidValue = $state.($item.Pid)
        if ($null -eq $pidValue) { continue }

        $process = Get-Process -Id ([int]$pidValue) -ErrorAction SilentlyContinue
        if ($null -eq $process) { continue }

        $expectedStart = [DateTime]::Parse($state.($item.Started))
        if ($process.StartTime -lt $expectedStart.AddSeconds(-2)) {
            Write-Host "$($item.Label) PID $pidValue was reused by another process; skipping stop." -ForegroundColor Yellow
            continue
        }

        Write-Host "Stopping $($item.Label) process tree PID $pidValue..." -ForegroundColor Cyan
        & taskkill.exe /PID ([int]$pidValue) /T /F | Out-Null
    }
    Remove-Item -LiteralPath $statePath -Force
}

Write-Host 'Stopping vid-lens-core (container data and volumes are preserved)...' -ForegroundColor Cyan
& docker compose stop
if ($LASTEXITCODE -ne 0) {
    throw 'Core Docker services failed to stop.'
}

Write-Host 'Development environment stopped.' -ForegroundColor Green
