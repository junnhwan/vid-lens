param(
    [switch]$InfraOnly
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repoRoot

function Require-Command([string]$Name) {
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Command '$Name' was not found. Install it and add it to PATH."
    }
}

function Test-ListeningPort([int]$Port) {
    return @(
        Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue
    ).Count -gt 0
}

function Get-ConfiguredEnvValue([string]$Name) {
    $value = [Environment]::GetEnvironmentVariable($Name)
    if (-not [string]::IsNullOrWhiteSpace($value)) {
        return $value.Trim()
    }

    $dotenvPath = Join-Path $repoRoot '.env'
    if (Test-Path -LiteralPath $dotenvPath) {
        $pattern = '^\s*' + [regex]::Escape($Name) + '\s*=\s*(.*?)\s*$'
        $dotenvValue = Get-Content -LiteralPath $dotenvPath | Where-Object { $_ -match $pattern } | Select-Object -First 1
        if ($null -ne $dotenvValue -and $dotenvValue -match $pattern) {
            $value = $Matches[1].Trim()
            if (($value.StartsWith('"') -and $value.EndsWith('"')) -or
                ($value.StartsWith("'") -and $value.EndsWith("'"))) {
                $value = $value.Substring(1, $value.Length - 2)
            }
            return $value
        }
    }

    return $null
}

function Resolve-ExecutablePath([string]$Configured, [string]$Fallback) {
    $candidate = $Configured
    if ([string]::IsNullOrWhiteSpace($candidate)) {
        $candidate = $Fallback
    }
    if ([System.IO.Path]::IsPathRooted($candidate)) {
        if (Test-Path -LiteralPath $candidate) {
            return [System.IO.Path]::GetFullPath($candidate)
        }
        return $null
    }
    $command = Get-Command $candidate -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $command) {
        return $null
    }
    return $command.Source
}

function Resolve-DataRoot {
    $configured = Get-ConfiguredEnvValue 'VIDLENS_DATA_ROOT'
    if (-not [string]::IsNullOrWhiteSpace($configured)) {
        if ([System.IO.Path]::IsPathRooted($configured)) {
            return [System.IO.Path]::GetFullPath($configured)
        }
        return [System.IO.Path]::GetFullPath((Join-Path $repoRoot $configured))
    }

    $historical = [System.IO.Path]::GetFullPath((Join-Path $repoRoot '..\vid-lens-727\data'))
    if ((Test-Path -LiteralPath (Join-Path $historical 'minio')) -and
        (Test-Path -LiteralPath (Join-Path $historical 'postgres'))) {
        return $historical
    }

    return [System.IO.Path]::GetFullPath((Join-Path $repoRoot 'data'))
}

Require-Command 'docker'
$dataRoot = Resolve-DataRoot
$env:VIDLENS_DATA_ROOT = $dataRoot.Replace('\', '/')

foreach ($requiredPath in @(
    (Join-Path $dataRoot 'minio'),
    (Join-Path $dataRoot 'postgres'),
    (Join-Path $repoRoot '.env'),
    (Join-Path $repoRoot 'config.yaml')
)) {
    if (-not (Test-Path -LiteralPath $requiredPath)) {
        throw "Preflight failed: '$requiredPath' was not found. The script stopped to avoid starting an empty data instance."
    }
}

if (-not $InfraOnly) {
    Require-Command 'go'
    Require-Command 'npm'
    $nodeModulesPath = Join-Path $repoRoot 'frontend\node_modules'
    if (-not (Test-Path -LiteralPath $nodeModulesPath)) {
        throw "Preflight failed: '$nodeModulesPath' was not found. The script stopped."
    }
    $ffmpegPath = Resolve-ExecutablePath (Get-ConfiguredEnvValue 'VIDLENS_FFMPEG_PATH') 'ffmpeg'
    if ($null -eq $ffmpegPath) {
        throw 'Preflight failed: FFmpeg was not found. Install it on PATH or set VIDLENS_FFMPEG_PATH in .env.'
    }
}

Write-Host "Using data root: $dataRoot" -ForegroundColor DarkCyan
Write-Host 'Starting vid-lens-core (reusing the selected PostgreSQL / MinIO data)...' -ForegroundColor Cyan
& docker compose up -d
if ($LASTEXITCODE -ne 0) {
    throw 'Core Docker services failed to start.'
}

Write-Host 'Waiting for PostgreSQL, Redis, RabbitMQ and MinIO...' -ForegroundColor DarkCyan
$ready = $false
for ($attempt = 0; $attempt -lt 60; $attempt++) {
    $pgHealth = [string](& docker inspect --format '{{.State.Health.Status}}' vidlens-postgres 2>$null)
    if (
        $pgHealth.Trim() -eq 'healthy' -and
        (Test-ListeningPort 6379) -and
        (Test-ListeningPort 5672) -and
        (Test-ListeningPort 9000)
    ) {
        $ready = $true
        break
    }
    Start-Sleep -Seconds 1
}
if (-not $ready) {
    & docker compose ps
    throw 'Core dependencies did not become ready within 60 seconds. Run make status for details.'
}

if ($InfraOnly) {
    Write-Host 'Core infrastructure is ready.' -ForegroundColor Green
    exit 0
}

& powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot 'dev-start-apps.ps1')
if ($LASTEXITCODE -ne 0) {
    throw 'Application processes failed to start.'
}
