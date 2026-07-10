param(
  [Parameter(Mandatory=$true)][string]$LocalPath,
  [Parameter(Mandatory=$true)][string]$Remote,
  [switch]$DryRun,
  [string]$ReceiptPath = "DRIVE-UPLOAD-RECEIPT.json"
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
if (-not (Get-Command rclone -ErrorAction SilentlyContinue)) { throw "rclone is required." }
if (-not (Test-Path -LiteralPath $LocalPath)) { throw "LocalPath not found: $LocalPath" }
$resolved = (Resolve-Path -LiteralPath $LocalPath).Path
$files = if (Test-Path $resolved -PathType Container) { @(Get-ChildItem $resolved -Recurse -File) } else { @(Get-Item $resolved) }
$total = ($files | Measure-Object Length -Sum).Sum
if ($null -eq $total) { $total = 0 }
$args = @("copy",$resolved,$Remote,"--checksum","--progress")
if ($DryRun) { $args += "--dry-run" }
Write-Host "Running rclone copy <local> <remote> --checksum --progress$(if ($DryRun) { ' --dry-run' })"
& rclone @args
$code = $LASTEXITCODE
$receipt = [ordered]@{
  schema_version = 1; generated_at_utc = [datetime]::UtcNow.ToString("o")
  tool = "rclone"; operation = "copy"; dry_run = [bool]$DryRun
  local_path = $resolved; remote = $Remote; file_count = $files.Count
  total_bytes = [int64]$total; checksum_mode = $true; exit_code = $code
  status = if ($code -eq 0) { "completed" } else { "failed" }
}
$parent = Split-Path -Parent $ReceiptPath
if ($parent) { New-Item -ItemType Directory -Force -Path $parent | Out-Null }
$receipt | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $ReceiptPath -Encoding utf8
if ($code -ne 0) { throw "rclone failed with exit code $code" }
Write-Host "Wrote receipt: $ReceiptPath"