# Axiom - Windows PowerShell Developer Runner
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('fmt', 'tidy', 'vet', 'lint', 'test', 'race', 'examples', 'fuzz', 'consumer', 'build', 'bench', 'check', 'ci', 'clean', 'help')]
    [string]$Target = 'check'
)

$ErrorActionPreference = 'Stop'
$RepoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $RepoRoot

function Show-Help {
    Write-Host "Axiom Developer PowerShell Commands:" -ForegroundColor Cyan
    Write-Host "  .\scripts\dev.ps1 fmt       - Format Go source files"
    Write-Host "  .\scripts\dev.ps1 tidy      - Verify go.mod and go.sum"
    Write-Host "  .\scripts\dev.ps1 vet       - Run go vet"
    Write-Host "  .\scripts\dev.ps1 lint      - Run golangci-lint"
    Write-Host "  .\scripts\dev.ps1 test      - Run unit tests"
    Write-Host "  .\scripts\dev.ps1 race      - Run race detector on critical packages"
    Write-Host "  .\scripts\dev.ps1 examples  - Run and verify all public examples"
    Write-Host "  .\scripts\dev.ps1 fuzz      - Run parser and normalizer fuzz smoke tests"
    Write-Host "  .\scripts\dev.ps1 build     - Build axiomgen and axiombench binaries"
    Write-Host "  .\scripts\dev.ps1 bench     - Run performance benchmarks"
    Write-Host "  .\scripts\dev.ps1 check     - Run fast checks (tidy, vet, lint, test)"
    Write-Host "  .\scripts\dev.ps1 ci        - Run full CI pipeline locally"
    Write-Host "  .\scripts\dev.ps1 clean     - Clean test and build artifacts"
}

switch ($Target) {
    'help' {
        Show-Help
    }
    'fmt' {
        Write-Host "==> Formatting code..." -ForegroundColor Cyan
        go fmt ./...
    }
    'tidy' {
        Write-Host "==> Verifying module consistency..." -ForegroundColor Cyan
        go mod tidy
        $diff = git diff --exit-code -- go.mod go.sum
        if ($LASTEXITCODE -ne 0) {
            throw "go.mod or go.sum was modified by go mod tidy"
        }
    }
    'vet' {
        Write-Host "==> Running go vet..." -ForegroundColor Cyan
        go vet ./...
    }
    'lint' {
        Write-Host "==> Running golangci-lint..." -ForegroundColor Cyan
        golangci-lint run ./...
    }
    'test' {
        Write-Host "==> Running unit tests..." -ForegroundColor Cyan
        if (-not (Test-Path "test-artifacts")) { New-Item -ItemType Directory -Path "test-artifacts" | Out-Null }
        go test -v ./...
    }
    'race' {
        Write-Host "==> Running race detector..." -ForegroundColor Cyan
        go test -race . ./internal/runtime/... ./internal/store/...
    }
    'examples' {
        Write-Host "==> Running public examples..." -ForegroundColor Cyan
        if (-not (Test-Path "test-artifacts")) { New-Item -ItemType Directory -Path "test-artifacts" | Out-Null }
        $examples = @('model', 'go-first', 'order', 'axiom-files', 'table', 'triz')
        foreach ($ex in $examples) {
            Write-Host "--> Running examples/$ex"
            go run "./examples/$ex" 2>&1 | Tee-Object "test-artifacts/$ex.log"
        }
        Write-Host "--> Running examples/coffee-machine"
        go run ./examples/coffee-machine 2>&1 | Tee-Object "test-artifacts/coffee-machine.log"
        $coffeeLog = Get-Content "test-artifacts/coffee-machine.log" -Raw
        if ($coffeeLog -notmatch '350,00' -or $coffeeLog -notmatch '230,00') {
            throw "Coffee machine example validation failed"
        }
        Write-Host "==> All examples verified." -ForegroundColor Green
    }
    'fuzz' {
        Write-Host "==> Running fuzz smoke tests..." -ForegroundColor Cyan
        go test ./internal/lang -run=^$ -fuzz=^FuzzParse$ -fuzztime=5s
        go test ./internal/triz -run=^$ -fuzz=^FuzzNormalize$ -fuzztime=5s
    }
    'build' {
        Write-Host "==> Building CLI binaries..." -ForegroundColor Cyan
        if (-not (Test-Path "bin")) { New-Item -ItemType Directory -Path "bin" | Out-Null }
        go build -o bin/axiomgen.exe ./cmd/axiomgen
        go build -o bin/axiombench.exe ./cmd/axiombench
        Write-Host "==> Binaries created in bin/" -ForegroundColor Green
    }
    'bench' {
        Write-Host "==> Running benchmark suite..." -ForegroundColor Cyan
        if (-not (Test-Path "artifacts")) { New-Item -ItemType Directory -Path "artifacts" | Out-Null }
        go run ./cmd/axiombench `
            -memory-ops 20000 `
            -pebble-ops 1000 `
            -replay-events 1000 `
            -replay-runs 200 `
            -concurrency 8 `
            -strict=true `
            -json artifacts/benchmark-results.json `
            -markdown artifacts/benchmark-results.md
    }
    'check' {
        & $PSCommandPath tidy
        & $PSCommandPath vet
        & $PSCommandPath lint
        & $PSCommandPath test
        Write-Host "==> Fast checks passed!" -ForegroundColor Green
    }
    'ci' {
        & $PSCommandPath tidy
        & $PSCommandPath vet
        & $PSCommandPath lint
        & $PSCommandPath test
        & $PSCommandPath race
        & $PSCommandPath examples
        & $PSCommandPath fuzz
        & $PSCommandPath build
        Write-Host "==> Full local CI suite passed!" -ForegroundColor Green
    }
    'clean' {
        Write-Host "==> Cleaning artifacts..." -ForegroundColor Cyan
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue bin, artifacts, test-artifacts, coverage.out
    }
}
