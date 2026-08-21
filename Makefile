# Axiom - Build & CI Automation Makefile
.DEFAULT_GOAL := help

SHELL := /bin/bash
GO ?= go
GOLANGCI_LINT ?= golangci-lint
BIN_DIR := ./bin
ARTIFACTS_DIR := ./artifacts
TEST_ARTIFACTS_DIR := ./test-artifacts

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.0-dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "unknown")
LDFLAGS := -s -w -X 'main.Version=$(VERSION)' -X 'main.Commit=$(COMMIT)' -X 'main.BuildDate=$(BUILD_DATE)'

.PHONY: help
help: ## Show this help message
	@echo "Axiom Engineering Tasks:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: fmt
fmt: ## Format Go source code
	@echo "==> Formatting source code..."
	$(GO) fmt ./...

.PHONY: fmt-check
fmt-check: ## Check Go source formatting without modifying
	@echo "==> Checking source code formatting..."
	@test -z "$$($(GO) fmt ./...)" || (echo "Code is not formatted. Run 'make fmt'" && exit 1)

.PHONY: tidy
tidy: ## Verify go.mod and go.sum consistency
	@echo "==> Verifying module consistency..."
	$(GO) mod tidy
	git diff --exit-code -- go.mod go.sum

.PHONY: vet
vet: ## Run go vet on all packages
	@echo "==> Running go vet..."
	$(GO) vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	@echo "==> Running golangci-lint..."
	$(GOLANGCI_LINT) run ./...

.PHONY: test
test: ## Run unit tests on all packages
	@echo "==> Running unit tests..."
	@mkdir -p $(TEST_ARTIFACTS_DIR)
	$(GO) test -v ./...

.PHONY: test-coverage
test-coverage: ## Run unit tests and generate coverage profile
	@echo "==> Running unit tests with coverage..."
	@mkdir -p $(TEST_ARTIFACTS_DIR)
	$(GO) test -coverprofile=$(TEST_ARTIFACTS_DIR)/coverage.out -covermode=atomic ./...
	$(GO) tool cover -func=$(TEST_ARTIFACTS_DIR)/coverage.out

.PHONY: race
race: ## Run race detector on critical runtime and store packages
	@echo "==> Running race detector..."
	$(GO) test -race . ./internal/runtime/... ./internal/store/...

.PHONY: examples
examples: ## Run and verify all public and internal examples
	@echo "==> Running public examples..."
	@mkdir -p $(TEST_ARTIFACTS_DIR)
	@for ex in model go-first order axiom-files table triz; do \
		echo "--> Running example: $$ex"; \
		$(GO) run "./examples/$$ex" > "$(TEST_ARTIFACTS_DIR)/$$ex.log" 2>&1 || exit 1; \
	done
	@echo "--> Running coffee-machine example..."
	$(GO) run ./examples/coffee-machine > $(TEST_ARTIFACTS_DIR)/coffee-machine.log 2>&1
	@grep -F 'принято:    350,00 ₽' $(TEST_ARTIFACTS_DIR)/coffee-machine.log >/dev/null
	@grep -F 'возвращено: 120,00 ₽' $(TEST_ARTIFACTS_DIR)/coffee-machine.log >/dev/null
	@grep -F 'выручка:    230,00 ₽' $(TEST_ARTIFACTS_DIR)/coffee-machine.log >/dev/null
	@echo "==> All examples verified successfully."

.PHONY: fuzz-smoke
fuzz-smoke: ## Run short fuzzing smoke tests on parsers and normalizers
	@echo "==> Running fuzz smoke tests..."
	$(GO) test ./internal/lang -run=^$$ -fuzz=^FuzzParse$$ -fuzztime=5s
	$(GO) test ./internal/triz -run=^$$ -fuzz=^FuzzNormalize$$ -fuzztime=5s

.PHONY: consumer-test
consumer-test: ## Test importing public packages from an isolated external Go module
	@echo "==> Running external consumer isolation test..."
	@bash scripts/test_consumer.sh

.PHONY: build
build: ## Build all CLI binaries (axiomgen, axiombench)
	@echo "==> Building CLI binaries..."
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/axiomgen ./cmd/axiomgen
	$(GO) build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/axiombench ./cmd/axiombench
	@echo "==> Binaries built in $(BIN_DIR)/"

.PHONY: bench
bench: ## Run standard performance benchmark suite
	@echo "==> Running performance benchmarks..."
	@mkdir -p $(ARTIFACTS_DIR)
	$(GO) run ./cmd/axiombench \
		-memory-ops 20000 \
		-pebble-ops 1000 \
		-replay-events 1000 \
		-replay-runs 200 \
		-concurrency 8 \
		-strict=true \
		-json $(ARTIFACTS_DIR)/benchmark-results.json \
		-markdown $(ARTIFACTS_DIR)/benchmark-results.md

.PHONY: check
check: tidy vet lint test ## Run fast local validation suite (tidy, vet, lint, test)
	@echo "==> Fast check completed successfully."

.PHONY: ci
ci: check race examples fuzz-smoke consumer-test build ## Run full CI pipeline locally
	@echo "==> Full local CI validation passed!"

.PHONY: clean
clean: ## Clean build and test artifacts
	@echo "==> Cleaning artifacts and binaries..."
	@rm -rf $(BIN_DIR) $(ARTIFACTS_DIR) $(TEST_ARTIFACTS_DIR) coverage.out
