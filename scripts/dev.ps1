param([string]$MediaRoot = '', [int]$Port = 8080)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
if (-not $MediaRoot) { $MediaRoot = Join-Path $projectRoot 'media' }
if (-not (Test-Path -LiteralPath $MediaRoot)) { New-Item -ItemType Directory -Path $MediaRoot | Out-Null }
$resolvedMedia = (Resolve-Path -LiteralPath $MediaRoot).Path
Push-Location (Join-Path $projectRoot 'web')
try {
    if (-not (Test-Path -LiteralPath 'node_modules')) { npm.cmd ci; if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' } }
    npm.cmd run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed' }
} finally { Pop-Location }
Push-Location $projectRoot
try {
    $env:MEDIA_ROOT = $resolvedMedia
    $env:DATA_DIR = Join-Path $projectRoot 'data'
    $env:WEB_DIR = Join-Path $projectRoot 'web/dist'
    $env:LISTEN_ADDR = ":$Port"
    Write-Host "MyTiktok: http://localhost:$Port"
    go run ./cmd/mytiktok
    if ($LASTEXITCODE -ne 0) { throw 'Backend failed' }
} finally { Pop-Location }
