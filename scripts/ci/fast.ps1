[CmdletBinding()]
param([switch]$SkipGo, [string]$ReceiptPath = "receipts/ci-fast-receipt.json")
$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Set-Location $repoRoot
function Run-Step([string]$Name, [scriptblock]$Script) {
    Write-Host "==> $Name"
    try { & $Script; [pscustomobject]@{ name = $Name; success = $true; detail = "ok" } }
    catch { [pscustomobject]@{ name = $Name; success = $false; detail = $_.Exception.Message } }
}
$steps = @()
$steps += (Run-Step "git status" { git status --short | Out-Host; if ($LASTEXITCODE -ne 0) { throw "git status failed" } })
$steps += (Run-Step "git diff --check" { git diff --check; if ($LASTEXITCODE -ne 0) { throw "git diff --check failed" } })
$steps += (Run-Step "secret scan" { & .\scripts\ci\secret-scan.ps1; if ($LASTEXITCODE -ne 0) { throw "secret scan failed" } })
$steps += (Run-Step "required files" { foreach ($p in @("go.mod", "README.md", "cmd", "internal", "docs")) { if (-not (Test-Path $p)) { throw "missing $p" } } })
if (-not $SkipGo) {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw "Go is required for fast CI; install Go 1.23+ or run with -SkipGo only for bootstrap diagnosis" }
    $steps += (Run-Step "go version" { go version; if ($LASTEXITCODE -ne 0) { throw "go version failed" } })
    $steps += (Run-Step "go env" { go env; if ($LASTEXITCODE -ne 0) { throw "go env failed" } })
    $steps += (Run-Step "go test deterministic packages" { go test ./internal/... ./cmd/proof01 ./cmd/proof02 ./cmd/proof03 ./cmd/proof04 ./cmd/proof05 ./cmd/proof06 ./cmd/proof07 ./cmd/proof08 ./cmd/proof09 ./cmd/proof10 ./cmd/proof11 ./cmd/proof12 ./cmd/proof13 ./cmd/proof14 ./cmd/proof15 ./cmd/proof18 ./cmd/proof19 ./cmd/proof20 ./cmd/proof21 ./cmd/proof22 ./cmd/proof23 ./cmd/proof26 ./cmd/proof27 ./cmd/proof28 ./cmd/proof29 ./cmd/proof30 ./cmd/proof31 ./cmd/proof32 ./cmd/proof33 ./cmd/proof34 ./cmd/proof35 ./cmd/proof36 ./cmd/proof37 ./cmd/proof38; if ($LASTEXITCODE -ne 0) { throw "go test failed" } })
    $steps += (Run-Step "go vet" { go vet ./...; if ($LASTEXITCODE -ne 0) { throw "go vet failed" } })
$steps += (Run-Step "compile key commands" { foreach ($pkg in @("./cmd/keydeck-core", "./cmd/keydeck-desktop-shell", "./cmd/keydeck-desktop-ui", "./cmd/keydeck-desktop", "./cmd/proof38")) { go build $pkg; if ($LASTEXITCODE -ne 0) { throw "go build $pkg failed" } } })
}
$receiptDir = Split-Path -Parent $ReceiptPath
if ($receiptDir) { New-Item -ItemType Directory -Force -Path $receiptDir | Out-Null }
$failed = @($steps | Where-Object { -not $_.success })
$receipt = [ordered]@{ timestamp_utc = (Get-Date).ToUniversalTime().ToString("o"); tier = "fast"; skip_go = [bool]$SkipGo; steps = @($steps); success = ($failed.Count -eq 0) }
$receipt | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $ReceiptPath -Encoding UTF8
if ($failed.Count -gt 0) { exit 1 }
Write-Host "Fast CI OK"
