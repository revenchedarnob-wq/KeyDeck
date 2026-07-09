$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot

Write-Host ''
Write-Host 'KeyDeck Proof 0.8 - Context Compiler Efficiency (v0.5.3)' -ForegroundColor Cyan
Write-Host 'No API provider or paid API key is used.' -ForegroundColor Gray
Write-Host 'Real Codex uses your existing official ChatGPT authentication.' -ForegroundColor Gray
Write-Host 'The structural engine is downloaded as one pinned release, verified, and kept inside the disposable proof workspace.' -ForegroundColor Gray
Write-Host 'This benchmark is static-analysis only; it does not require Go or another language toolchain inside the Codex sandbox.' -ForegroundColor Gray
Write-Host 'For this disposable benchmark only, Codex uses the official unelevated Windows sandbox fallback to avoid known elevated-sandbox process-launch instability.' -ForegroundColor Gray
Write-Host ''

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Write-Host 'Git was not found on PATH.' -ForegroundColor Red
    Write-Host 'Proof 0.8 requires Git to prove both benchmark repos start identically and source files remain unchanged.' -ForegroundColor Yellow
    exit 1
}

# Preserve the proven Windows compatibility path from Proofs 0.5-0.7.
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

& "$PSScriptRoot\proof08-real.exe"
$code = $LASTEXITCODE

Write-Host ''
if ($code -eq 0) {
    Write-Host 'PASS - Proof 0.8 completed.' -ForegroundColor Green
} else {
    Write-Host "Proof exited with code $code." -ForegroundColor Red
    Write-Host 'The disposable workspace and exact report are preserved for evidence-based inspection.' -ForegroundColor Yellow
    Write-Host 'A non-zero result can be a valid INCONCLUSIVE benchmark; it does not automatically mean KeyDeck or Codex is broken.' -ForegroundColor Yellow
}
exit $code
