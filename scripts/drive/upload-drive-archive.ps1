param(
  [Parameter(Mandatory=$true)][string]$LocalPath,
  [Parameter(Mandatory=$true)][string]$Remote,
  [switch]$DryRun
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
if (-not (Get-Command rclone -ErrorAction SilentlyContinue)) { throw "rclone is required for Drive archive upload." }
$args = @("copy", $LocalPath, $Remote, "--checksum", "--progress")
if ($DryRun) { $args += "--dry-run" }
Write-Host "Running rclone $($args -join ' ')"
& rclone @args
