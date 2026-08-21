#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

target="${1:-check}"

case "$target" in
  fmt)
    go fmt ./...
    ;;
  tidy)
    go mod tidy
    git diff --exit-code -- go.mod go.sum
    ;;
  vet)
    go vet ./...
    ;;
  lint)
    golangci-lint run ./...
    ;;
  test)
    mkdir -p test-artifacts
    go test -v ./...
    ;;
  race)
    go test -race . ./internal/runtime/... ./internal/store/...
    ;;
  examples)
    mkdir -p test-artifacts
    for ex in model go-first order axiom-files table triz; do
      echo "==> Running example: $ex"
      go run "./examples/$ex" > "test-artifacts/$ex.log" 2>&1
    done
    go run ./examples/coffee-machine > test-artifacts/coffee-machine.log 2>&1
    grep -F 'принято:    350,00 ₽' test-artifacts/coffee-machine.log >/dev/null
    grep -F 'возвращено: 120,00 ₽' test-artifacts/coffee-machine.log >/dev/null
    grep -F 'выручка:    230,00 ₽' test-artifacts/coffee-machine.log >/dev/null
    echo "==> Examples passed."
    ;;
  fuzz)
    go test ./internal/lang -run=^$ -fuzz=^FuzzParse$ -fuzztime=5s
    go test ./internal/triz -run=^$ -fuzz=^FuzzNormalize$ -fuzztime=5s
    ;;
  consumer)
    bash scripts/test_consumer.sh
    ;;
  build)
    mkdir -p bin
    go build -o bin/axiomgen ./cmd/axiomgen
    go build -o bin/axiombench ./cmd/axiombench
    ;;
  bench)
    mkdir -p artifacts
    go run ./cmd/axiombench \
      -memory-ops 20000 \
      -pebble-ops 1000 \
      -replay-events 1000 \
      -replay-runs 200 \
      -concurrency 8 \
      -strict=true \
      -json artifacts/benchmark-results.json \
      -markdown artifacts/benchmark-results.md
    ;;
  check)
    "$0" tidy
    "$0" vet
    "$0" lint
    "$0" test
    ;;
  ci)
    "$0" tidy
    "$0" vet
    "$0" lint
    "$0" test
    "$0" race
    "$0" examples
    "$0" fuzz
    "$0" consumer
    "$0" build
    echo "==> All CI stages passed successfully!"
    ;;
  *)
    echo "Usage: $0 {fmt|tidy|vet|lint|test|race|examples|fuzz|consumer|build|bench|check|ci}"
    exit 1
    ;;
esac
