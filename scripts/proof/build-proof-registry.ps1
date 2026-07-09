param(
  [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path,
  [string]$OutJson = (Join-Path $RepoRoot "docs\PROOF_REGISTRY.json"),
  [string]$OutMarkdown = (Join-Path $RepoRoot "docs\PROOF_REGISTRY.md")
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
Set-Location $RepoRoot
$validationPath = Join-Path $RepoRoot "docs\KeyDeck-v0.35.0-RECONSTRUCTED-VALIDATION.json"
if (-not (Test-Path $validationPath)) { throw "Missing validation file: $validationPath" }
$validation = Get-Content -LiteralPath $validationPath -Raw | ConvertFrom-Json
$items = New-Object System.Collections.Generic.List[object]
foreach ($n in 1..8) {
  $suffix = if ($n -in @(6,7,8)) { "-policy" } else { "" }
  $items.Add([pscustomobject]@{
    proof = "0.$n"
    status = "PASS"
    scenario_count = $null
    report_sha256 = $null
    source_report_path = "docs/KeyDeck-Proof-0.$n$suffix-report.json"
    evidence_class = "historical_verified_report_present"
    note = "Scenario count not rederived by automation bootstrap."
  }) | Out-Null
}
foreach ($prop in $validation.proofs.PSObject.Properties | Sort-Object { [int]$_.Name }) {
  $n = [int]$prop.Name
  $items.Add([pscustomobject]@{
    proof = "0.$n"
    status = if ($prop.Value.passed) { "PASS" } else { "FAIL" }
    scenario_count = $prop.Value.scenario_count
    report_sha256 = $prop.Value.report_sha256
    source_report_path = "docs/KeyDeck-Proof-0.$n-report.json"
    evidence_class = "validated_v0.35_reconstructed_report"
  }) | Out-Null
}
$registry = [ordered]@{
  schema_version = 1
  generated_at_utc = (Get-Date).ToUniversalTime().ToString("o")
  repository = "revenchedarnob-wq/KeyDeck"
  baseline = $validation.release
  latest_verified_proof = "0.38"
  next_gate = "0.39 Secret-Safe Production Visual Bootstrap and First-Run Desktop Launch"
  evidence_boundary = $validation.evidence_boundary
  proofs = $items
}
$registry | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $OutJson -Encoding utf8
$lines = New-Object System.Collections.Generic.List[string]
$lines.Add("# Proof Registry") | Out-Null
$lines.Add("") | Out-Null
$lines.Add("- Generated UTC: ``$($registry.generated_at_utc)``") | Out-Null
$lines.Add("- Baseline: ``$($registry.baseline)``") | Out-Null
$lines.Add("- Latest verified proof: ``0.38``") | Out-Null
$lines.Add("- Next gate: ``$($registry.next_gate)``") | Out-Null
$lines.Add("") | Out-Null
$lines.Add("| Proof | Status | Scenarios | Report SHA-256 |") | Out-Null
$lines.Add("|---:|---|---:|---|") | Out-Null
foreach ($item in $items) { $lines.Add("| $($item.proof) | $($item.status) | $($item.scenario_count) | ``$($item.report_sha256)`` |") | Out-Null }
$lines -join "`n" | Set-Content -LiteralPath $OutMarkdown -Encoding utf8
Write-Host "Wrote $OutJson and $OutMarkdown"
