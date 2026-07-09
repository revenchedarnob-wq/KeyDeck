[CmdletBinding()]
param([string]$ReceiptPath = "receipts/secret-scan-receipt.json")
$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Set-Location $repoRoot
function Redact([string]$Value) {
    if ($Value.Length -le 8) { return "***" }
    return $Value.Substring(0, 4) + "..." + $Value.Substring($Value.Length - 4)
}
$allowPathPatterns = @("^docs/KeyDeck-Proof-.*-report\.json$", "^testdata/", "^docs/PROOF_.*\.md$")
$binaryExtensions = "\.(exe|dll|zip|tgz|tar|gz|png|jpg|jpeg|ico)$"
$patterns = @(
    @{ name = "github_token"; regex = "(?<![A-Za-z0-9_])gh[pousr]_[A-Za-z0-9_]{36,}" },
    @{ name = "openai_key"; regex = "(?<![A-Za-z0-9_-])sk-(?:proj-)?[A-Za-z0-9_-]{20,}" },
    @{ name = "aws_access_key"; regex = "AKIA[0-9A-Z]{16}" },
    @{ name = "private_key"; regex = "-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----" },
    @{ name = "bearer_token"; regex = "(?i)bearer\s+[A-Za-z0-9._~+/-]{24,}" },
    @{ name = "oauth_secret_assignment"; regex = "(?i)(client_secret|oauth[_-]?secret)\s*[:=]\s*[`"'']?[A-Za-z0-9._~+/-]{16,}" },
    @{ name = "api_key_assignment"; regex = "(?i)(api[_-]?key|apikey|access[_-]?token|auth[_-]?token|password)\s*[:=]\s*[`"'']?[A-Za-z0-9._~+/-]{16,}" },
    @{ name = "connection_string_password"; regex = "(?i)(postgres|mysql|sqlserver|mongodb)://[^\s`"'']+:[^\s`"'']+@" }
)
$files = @()
$tracked = & git ls-files 2>$null
if ($LASTEXITCODE -ne 0) { throw "git ls-files failed" }
$files += $tracked
$stagedNames = & git diff --cached --name-only 2>$null
if ($LASTEXITCODE -eq 0) { $files += $stagedNames }
$files = $files | Sort-Object -Unique
$findings = @()
foreach ($file in $files) {
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { continue }
    $normalized = $file -replace "\\", "/"
    if ($normalized -match $binaryExtensions) { continue }
    $allowedFixture = $false
    foreach ($allow in $allowPathPatterns) { if ($normalized -match $allow) { $allowedFixture = $true; break } }
    if ((Get-Item -LiteralPath $file).Length -gt 5MB) { continue }
    $lines = Get-Content -LiteralPath $file -ErrorAction SilentlyContinue
    for ($i = 0; $i -lt $lines.Count; $i++) {
        $line = [string]$lines[$i]
        foreach ($p in $patterns) {
            $m = [regex]::Match($line, $p.regex)
            if ($m.Success) {
                $knownFake = $m.Value -match "(?i)TEST_VALUE|SUPERSECRET|example|placeholder|dummy|fake"
                if ($knownFake) { continue }
                $findings += [pscustomobject]@{
                    file = $normalized
                    line = $i + 1
                    type = $p.name
                    redacted = (Redact $m.Value)
                    credible_live_secret = (-not $allowedFixture)
                    fixture_allowed = $allowedFixture
                }
            }
        }
    }
}
$receiptDir = Split-Path -Parent $ReceiptPath
if ($receiptDir) { New-Item -ItemType Directory -Force -Path $receiptDir | Out-Null }
$credibleFindings = @($findings | Where-Object { $_.credible_live_secret })
$receipt = [ordered]@{
    timestamp_utc = (Get-Date).ToUniversalTime().ToString("o")
    files_scanned = $files.Count
    findings = @($findings)
    credible_live_secret_count = $credibleFindings.Count
    success = ($credibleFindings.Count -eq 0)
}
$receipt | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $ReceiptPath -Encoding UTF8
if ($credibleFindings.Count -gt 0) {
    $credibleFindings | Format-Table file,line,type,redacted -AutoSize | Out-String | Write-Error
    exit 1
}
Write-Host "Secret scan OK: $($files.Count) files scanned"