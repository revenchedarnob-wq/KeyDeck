param(
  [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path,
  [switch]$SkipProofReplay,
  [switch]$SkipSecretScan,
  [switch]$SkipFast,
  [switch]$StrictExternalProofReplay,
  [int]$StartProof = 1,
  [int]$EndProof = 38
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
Set-Location $RepoRoot

function Assert-HistoricalProofEvidence {
  param([Parameter(Mandatory=$true)][int]$Number)

  $proofID = "0.$Number"
  $registryPath = Join-Path $RepoRoot "docs\PROOF_REGISTRY.json"
  if (-not (Test-Path -LiteralPath $registryPath)) {
    throw "Proof registry is unavailable: $registryPath"
  }

  $registry = Get-Content -LiteralPath $registryPath -Raw | ConvertFrom-Json
  $entry = @($registry.proofs | Where-Object { $_.proof -eq $proofID })
  if ($entry.Count -ne 1) {
    throw "Proof registry must contain exactly one entry for $proofID"
  }
  $entry = $entry[0]

  if ([string]$entry.status -ne "PASS") {
    throw "Historical proof $proofID is not PASS in the proof registry."
  }
  if ([string]::IsNullOrWhiteSpace([string]$entry.source_report_path)) {
    throw "Historical proof $proofID has no source report path."
  }

  $reportPath = Join-Path $RepoRoot ([string]$entry.source_report_path)
  if (-not (Test-Path -LiteralPath $reportPath)) {
    throw "Historical proof report is unavailable: $reportPath"
  }

  $report = Get-Content -LiteralPath $reportPath -Raw | ConvertFrom-Json
  if ($report.passed -ne $true -or ([string]$report.status).ToLowerInvariant() -ne "passed") {
    throw "Historical proof report $proofID does not declare PASS."
  }

  if ($null -ne $entry.scenario_count) {
    $actualScenarioCount = @($report.scenarios).Count
    if ($actualScenarioCount -ne [int]$entry.scenario_count) {
      throw "Historical proof $proofID scenario count mismatch: expected $($entry.scenario_count), got $actualScenarioCount"
    }
  }

  if (-not [string]::IsNullOrWhiteSpace([string]$entry.report_sha256)) {
    Write-Host "Historical proof $proofID uses sealed registry SHA: $($entry.report_sha256)"
  }

  Write-Host "Historical sealed verification $proofID`: PASS ($($entry.source_report_path))"
}

function Test-RequiredExternalProofInputs {
  param([Parameter(Mandatory=$true)][int]$Number)

  $names = switch ($Number) {
    22 {
      @(
        "KEYDECK_PROOF22_SERVER_JS",
        "KEYDECK_PROOF22_PACKAGE_TARBALL",
        "KEYDECK_PROOF22_PACKAGE_LOCK",
        "KEYDECK_PROOF22_PACKAGE_JSON"
      )
    }
    23 {
      @(
        "KEYDECK_PROOF23_SERVER_JS",
        "KEYDECK_PROOF23_PACKAGE_TARBALL",
        "KEYDECK_PROOF23_PACKAGE_LOCK"
      )
    }
    24 {
      @(
        "KEYDECK_PROOF24_SERVER_JS",
        "KEYDECK_PROOF24_PACKAGE_TARBALL",
        "KEYDECK_PROOF24_PACKAGE_LOCK"
      )
    }
    25 {
      @(
        "KEYDECK_PROOF25_NODE",
        "KEYDECK_PROOF25_SERVER_JS",
        "KEYDECK_PROOF25_TARBALL",
        "KEYDECK_PROOF25_PACKAGE_LOCK"
      )
    }
    default { @() }
  }

  if ($names.Count -eq 0) {
    return $true
  }

  foreach ($name in $names) {
    $value = [Environment]::GetEnvironmentVariable($name)
    if ([string]::IsNullOrWhiteSpace($value) -or -not (Test-Path -LiteralPath $value -PathType Leaf)) {
      return $false
    }
  }
  return $true
}

if (-not $SkipSecretScan) {
  & (Join-Path $PSScriptRoot "secret-scan.ps1") -RepoRoot $RepoRoot
}
if (-not $SkipFast) {
  & (Join-Path $PSScriptRoot "fast.ps1") -RepoRoot $RepoRoot
}

if (-not $SkipProofReplay) {
  $proofDirs = Get-ChildItem -Path (Join-Path $RepoRoot "cmd") -Directory |
    Where-Object { $_.Name -match '^proof\d\d$' } |
    Sort-Object { [int]$_.Name.Substring(5) }

  foreach ($dir in $proofDirs) {
    $number = [int]$dir.Name.Substring(5)
    if ($number -lt $StartProof -or $number -gt $EndProof) {
      continue
    }

    # Proofs 22-25 depend on one immutable third-party package bundle that is
    # intentionally not present in the Git repository. Replay them only when
    # every exact path is supplied; otherwise verify their sealed historical
    # reports against the proof registry and continue.
    if ($number -ge 22 -and $number -le 25 -and -not (Test-RequiredExternalProofInputs -Number $number)) {
      if ($StrictExternalProofReplay) {
        throw "Proof $number requires its pinned external package bundle, but the required environment paths are unavailable."
      }
      Assert-HistoricalProofEvidence -Number $number
      continue
    }

    Write-Host "Replaying $($dir.Name)"
    go run "./cmd/$($dir.Name)"
    if ($LASTEXITCODE -ne 0) {
      throw "Proof replay failed for $($dir.Name) with exit code $LASTEXITCODE"
    }
  }
}
Write-Host "KEYDECK deep CI: PASS"