$ErrorActionPreference = 'Continue'

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repoRoot

Write-Host '--- vid-lens-core ---' -ForegroundColor Cyan
& docker compose ps

Write-Host '--- vid-lens-observability ---' -ForegroundColor Cyan
& docker compose -f docker-compose.observability.yml ps

Write-Host '--- application ports ---' -ForegroundColor Cyan
foreach ($item in @(
    @{ Port = 8080; Label = 'backend' },
    @{ Port = 5173; Label = 'frontend' }
)) {
    $listening = @(
        Get-NetTCPConnection -State Listen -LocalPort $item.Port -ErrorAction SilentlyContinue
    ).Count -gt 0
    $status = if ($listening) { 'LISTENING' } else { 'STOPPED' }
    Write-Host ("{0} {1}: {2}" -f $item.Label, $item.Port, $status)
}
