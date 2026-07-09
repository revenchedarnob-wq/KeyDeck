param(
  [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
Set-Location $RepoRoot
$extensions = @(".go", ".ps1", ".psm1", ".md", ".json", ".yml", ".yaml", ".txt", ".mod", ".sum")
$excluded = @("\.git\", "\dist\", "\_artifacts\")
$patterns = [ordered]@{
  "GitHub token" = "github_pat_[A-Za-z0-9_]{20,}|ghp_[A-Za-z0-9]{20,}"
  "OpenAI style key" = "sk-[A-Za-z0-9]{24,}"
  "Slack token" = "xox[baprs]-[A-Za-z0-9-]{20,}"
  "AWS access key" = "AKIA[0-9A-Z]{16}"
  "Private key block" = "-----BEGIN (RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----"
}
$findings = New-Object System.Collections.Generic.List[string]
Get-ChildItem -Path $RepoRoot -Recurse -File | Where-Object {
  $extensions -contains $_.Extension.ToLowerInvariant()
} | Where-Object {
  $full = $_.FullName
  -not ($excluded | Where-Object { $full -match [regex]::Escape([IO.Path]::DirectorySeparatorChar) + $_.Trim("\") })
} | ForEach-Object {
  $rel = [IO.Path]::GetRelativePath($RepoRoot, $_.FullName)
  $text = Get-Content -LiteralPath $_.FullName -Raw -ErrorAction Stop
  foreach ($name in $patterns.Keys) {
    if ($text -match $patterns[$name]) { $findings.Add("$rel :: $name") | Out-Null }
  }
}
if ($findings.Count -gt 0) {
  Write-Error ("Secret scan failed:`n" + ($findings -join "`n"))
}
Write-Host "KEYDECK secret scan: PASS"
