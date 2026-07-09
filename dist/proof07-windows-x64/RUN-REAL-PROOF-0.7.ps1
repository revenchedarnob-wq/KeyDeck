$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot

Write-Host ''
Write-Host 'KeyDeck Proof 0.7 - API Mid-Answer Exhaustion -> Real Codex -> Resume -> API' -ForegroundColor Cyan
Write-Host 'The API side is fully synthetic and spends no API credit.' -ForegroundColor Gray
Write-Host 'Real Codex uses your existing official ChatGPT authentication.' -ForegroundColor Gray
Write-Host ''

# Preserve the proven Windows compatibility path from Proofs 0.5 and 0.6.
# Prefer the real packaged Codex runtime so its bundled sandbox resources resolve.
$pkgRoot = Join-Path $env:USERPROFILE '.codex\packages\standalone\current'
$pkgBin = Join-Path $pkgRoot 'bin'
$pkgCodex = Join-Path $pkgBin 'codex.exe'
$pkgResources = Join-Path $pkgRoot 'codex-resources'
$setupHelper = Join-Path $pkgResources 'codex-windows-sandbox-setup.exe'

if (Test-Path -LiteralPath $pkgCodex) {
    Write-Host 'Using Codex standalone runtime directly:' -ForegroundColor DarkCyan
    Write-Host "  $pkgCodex" -ForegroundColor Gray
    $env:Path = "$pkgBin;$pkgResources;$env:Path"

    if (Test-Path -LiteralPath $setupHelper) {
        Write-Host 'Bundled Windows sandbox helper found.' -ForegroundColor Green
    } else {
        Write-Host 'Warning: bundled Windows sandbox helper not found at expected path.' -ForegroundColor Yellow
        Write-Host "  $setupHelper" -ForegroundColor Gray
    }
} elseif (-not (Get-Command codex -ErrorAction SilentlyContinue)) {
    Write-Host 'Codex CLI was not found.' -ForegroundColor Red
    Write-Host 'Install the official Codex CLI and sign in with ChatGPT, then rerun this script.' -ForegroundColor Yellow
    exit 1
} else {
    Write-Host 'Standalone package path not found; using Codex from PATH.' -ForegroundColor Yellow
}

$resolvedCodex = (Get-Command codex -ErrorAction Stop).Source
Write-Host 'Active Codex command for child processes:' -ForegroundColor DarkCyan
Write-Host "  $resolvedCodex" -ForegroundColor Gray
Write-Host ''

& "$PSScriptRoot\proof07-real.exe"
$code = $LASTEXITCODE

Write-Host ''
if ($code -eq 0) {
    Write-Host 'PASS - Proof 0.7 completed.' -ForegroundColor Green
} else {
    Write-Host "Proof exited with code $code." -ForegroundColor Red
    Write-Host 'The disposable project is preserved for exact failure inspection.' -ForegroundColor Yellow
}
exit $code
