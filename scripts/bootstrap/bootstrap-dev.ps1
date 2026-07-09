[CmdletBinding()]
param(
    [string]$ExpectedRemote = "https://github.com/revenchedarnob-wq/KeyDeck.git",
    [string]$ReceiptPath = "receipts/bootstrap-dev-receipt.json"
)
$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Set-Location $repoRoot
function Invoke-Capture {
    param([string]$FileName, [string[]]$Arguments)
    try {
        $output = & $FileName @Arguments 2>&1
        [pscustomobject]@{ ok = ($LASTEXITCODE -eq 0 -or $null -eq $LASTEXITCODE); output = (($output | Out-String).Trim()) }
    } catch {
        [pscustomobject]@{ ok = $false; output = $_.Exception.Message }
    }
}
function Require-Command {
    param([string]$Name)
    $cmd = Get-Command $Name -ErrorAction SilentlyContinue
    if (-not $cmd) { throw "$Name is required but was not found on PATH" }
    return $cmd.Source
}
$receiptDir = Split-Path -Parent $ReceiptPath
if ($receiptDir) { New-Item -ItemType Directory -Force -Path $receiptDir | Out-Null }
New-Item -ItemType Directory -Force -Path "local-only", "logs", "receipts" | Out-Null
$null = Require-Command git
$null = Require-Command gh
$null = Require-Command go
$remote = (Invoke-Capture git @("remote", "get-url", "origin")).output
if ($remote -ne $ExpectedRemote) { throw "unexpected origin remote: $remote" }
$ghAuth = Invoke-Capture gh @("auth", "status")
if (-not $ghAuth.ok -or $ghAuth.output -notmatch "revenchedarnob-wq") { throw "gh auth is not valid for revenchedarnob-wq" }
$status = Invoke-Capture git @("status", "--short")
$branch = (Invoke-Capture git @("branch", "--show-current")).output
$head = (Invoke-Capture git @("rev-parse", "HEAD")).output
$receipt = [ordered]@{
    timestamp_utc = (Get-Date).ToUniversalTime().ToString("o")
    repo_root = $repoRoot
    expected_remote = $ExpectedRemote
    remote = $remote
    branch = $branch
    head = $head
    git_status_short = $status.output
    powershell_version = $PSVersionTable.PSVersion.ToString()
    git_version = (Invoke-Capture git @("--version")).output
    gh_version = ((Invoke-Capture gh @("--version")).output -split "`r?`n")[0]
    go_version = (Invoke-Capture go @("version")).output
    gh_auth_status = $ghAuth.output -replace "gho_[A-Za-z0-9_]+", "gho_***redacted***"
    created_local_dirs = @("local-only", "logs", "receipts")
    success = $true
}
$receipt | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $ReceiptPath -Encoding UTF8
Write-Host "Bootstrap OK: $branch $head"
