param(
  [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
Set-Location $RepoRoot
$extensions = @(".go", ".ps1", ".psm1", ".md", ".json", ".yml", ".yaml", ".txt", ".mod", ".sum", ".csv")
$excludedRootNames = @(".git", "dist", "_artifacts")
$patterns = [ordered]@{
  "GitHub token" = "github_pat_[A-Za-z0-9_]{20,}|ghp_[A-Za-z0-9]{20,}"
  "OpenAI style key" = "sk-[A-Za-z0-9]{24,}"
  "Slack token" = "xox[baprs]-[A-Za-z0-9-]{20,}"
  "AWS access key" = "AKIA[0-9A-Z]{16}"
  "Private key block" = "-----BEGIN (RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----"
}
$findings = New-Object System.Collections.Generic.List[string]
$files = Get-ChildItem -Path $RepoRoot -Recurse -File | Where-Object { $extensions -contains $_.Extension.ToLowerInvariant() }
foreach ($file in $files) {
  $rel = [IO.Path]::GetRelativePath($RepoRoot, $file.FullName)
  $top = ($rel -split '[\\/]+' )[0]
  if ($excludedRootNames -contains $top) { continue }
  $text = Get-Content -LiteralPath $file.FullName -Raw -ErrorAction Stop
  foreach ($name in $patterns.Keys) {
    if ($text -match $patterns[$name]) { $findings.Add("$rel :: $name") | Out-Null }
  }
}
if ($findings.Count -gt 0) {
  Write-Error ("Secret scan failed:`n" + ($findings -join "`n"))
}
Write-Host "KEYDECK secret scan: PASS"
