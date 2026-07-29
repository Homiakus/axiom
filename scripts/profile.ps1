# PowerShell Script to collect CPU and Memory profiles for Axiom benchmarks
# Usage: .\scripts\profile.ps1

param (
    [string]$BenchName = "BenchmarkSignalMemory_10K",
    [string]$OutputDir = "profiles"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir | Out-Null
}

$cpuProfile = Join-Path $OutputDir "cpu.pprof"
$memProfile = Join-Path $OutputDir "mem.pprof"

Write-Host "Collecting CPU and Memory profiles for $BenchName..." -ForegroundColor Cyan

go test ./... -run '^$' -bench $BenchName -benchmem -benchtime=5s -cpuprofile=$cpuProfile -memprofile=$memProfile

Write-Host ""
Write-Host "Profiles saved to:" -ForegroundColor Green
Write-Host "  CPU profile: $cpuProfile" -ForegroundColor Green
Write-Host "  Mem profile: $memProfile" -ForegroundColor Green
Write-Host ""
Write-Host "To analyze profiles, run:" -ForegroundColor Yellow
Write-Host "  go tool pprof -http=:8080 $cpuProfile" -ForegroundColor Yellow
Write-Host "  go tool pprof -http=:8081 $memProfile" -ForegroundColor Yellow
