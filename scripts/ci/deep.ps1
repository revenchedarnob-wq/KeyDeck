[CmdletBinding()]
param([switch]$SkipGo, [string]$ReceiptPath = "receipts/ci-deep-receipt.json")
$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Set-Location $repoRoot
& .\scripts\ci\fast.ps1 -SkipGo:$SkipGo
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$steps = @()
function Run-Step([string]$Name, [scriptblock]$Script) {
    Write-Host "==> $Name"
    try { & $Script; [pscustomobject]@{ name = $Name; success = $true; detail = "ok" } }
    catch { [pscustomobject]@{ name = $Name; success = $false; detail = $_.Exception.Message } }
}
$steps += (Run-Step "proof registry" { & .\scripts\proof\build-proof-registry.ps1; if ($LASTEXITCODE -ne 0) { throw "proof registry failed" } })
$steps += (Run-Step "artifact manifest smoke" { & .\scripts\artifacts\new-manifest.ps1 -Path docs -OutputPath receipts/docs-manifest.csv; if ($LASTEXITCODE -ne 0) { throw "manifest failed" } })
if (-not $SkipGo) {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw "Go is required for deep CI; install Go 1.23+ or run with -SkipGo only for bootstrap diagnosis" }
    $steps += (Run-Step "complete deterministic tests" { go test ./...; if ($LASTEXITCODE -ne 0) { throw "go test ./... failed" } })
    $steps += (Run-Step "race tests internal" { go test -race ./internal/...; if ($LASTEXITCODE -ne 0) { throw "go test -race ./internal/... failed" } })
}
$receiptDir = Split-Path -Parent $ReceiptPath
if ($receiptDir) { New-Item -ItemType Directory -Force -Path $receiptDir | Out-Null }
$failed = @($steps | Where-Object { -not $_.success })
$receipt = [ordered]@{ timestamp_utc = (Get-Date).ToUniversalTime().ToString("o"); tier = "deep"; skip_go = [bool]$SkipGo; steps = @($steps); success = ($failed.Count -eq 0) }
$receipt | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $ReceiptPath -Encoding UTF8
if ($failed.Count -gt 0) { exit 1 }
Write-Host "Deep CI OK"
