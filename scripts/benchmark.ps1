# PowerShell Script to run the complete Axiom benchmark and test suite
# Usage: .\scripts\benchmark.ps1 [-Full] [-BenchTime 3s]

param (
    [switch]$Full,
    [string]$BenchTime = "3s"
)

$ErrorActionPreference = "Stop"

Write-Host "============================================================" -ForegroundColor Cyan
Write-Host "  Axiom Go Library — Automated Test & Benchmark Suite" -ForegroundColor Cyan
Write-Host "============================================================" -ForegroundColor Cyan
Write-Host ""

# 1. Run standard unit and integration tests
Write-Host "[1/4] Running unit and integration tests..." -ForegroundColor Yellow
go test ./... -count=1 -short
if ($LASTEXITCODE -ne 0) {
    Write-Error "Unit tests failed!"
    exit 1
}
Write-Host "  --> Unit tests PASSED" -ForegroundColor Green
Write-Host ""

# 2. Run race detector
Write-Host "[2/4] Running race detector..." -ForegroundColor Yellow
go test -race -count=1 ./...
if ($LASTEXITCODE -ne 0) {
    Write-Error "Race detector failed!"
    exit 1
}
Write-Host "  --> Race detector PASSED" -ForegroundColor Green
Write-Host ""

# 3. Run benchmarks
Write-Host "[3/4] Running benchmark suite (benchtime=$BenchTime)..." -ForegroundColor Yellow
go test ./... -run '^$' -bench . -benchmem -benchtime=$BenchTime -count=3 | Tee-Object -FilePath "benchmark_results.txt"
Write-Host "  --> Benchmark results saved to benchmark_results.txt" -ForegroundColor Green
Write-Host ""

# 4. Optional full stress & latency tests
if ($Full) {
    Write-Host "[4/4] Running full stress & latency suite..." -ForegroundColor Yellow
    go test -v -count=1 ./... -run "TestStress|TestLatency"
    Write-Host "  --> Full suite COMPLETED" -ForegroundColor Green
} else {
    Write-Host "[4/4] Skipping full stress suite (pass -Full to enable)." -ForegroundColor Yellow
}

Write-Host ""
Write-Host "============================================================" -ForegroundColor Cyan
Write-Host "  All tasks completed successfully!" -ForegroundColor Cyan
Write-Host "============================================================" -ForegroundColor Cyan
