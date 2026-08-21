# Axiom CI/CD Architecture & Operations Manual

## 1. Overview & Principles

Axiom follows a **DAG-based, reproducible, supply-chain secure** CI/CD methodology designed for deterministic business execution runtimes in Go.

### Core Objectives:
1. **Early Failure Feedback**: Static analysis, module hygiene, and fast unit checks provide signal within minutes.
2. **Local = CI Parity**: Every check that runs in GitHub Actions can be executed locally with identical behavior via `make` or `scripts/dev.ps1`.
3. **Software Supply Chain Hardening**: Cryptographic checksums (`SHA256SUMS`), CycloneDX/SPDX SBOM generation, official Go vulnerability analysis (`govulncheck`), SAST (`gosec`), and Gitleaks secret detection.
4. **Reproducible Releases**: Multi-arch cross-compiled CLI binaries (`axiomgen`, `axiombench`) and container images (`ghcr.io/homiakus/axiomgen`) with immutable commit references.

---

## 2. Pipeline DAG Architecture

```text
                                  ┌──> [lint] (GolangCI-Lint)
                                  │
                                  ├──> [module-hygiene] (go mod tidy diff)
                                  │
                                  ├──> [unit-tests] (Matrix: Linux, Win, macOS)
                                  │
         ┌── Fast Feedback ───────┼──> [race-detector] (go test -race critical packages)
         │                        │
         │                        ├──> [examples-suite] (Public examples & coffee-machine)
         │                        │
[Trigger]┤                        ├──> [fuzz-smoke] (Parser & TRIZ normalizer smoke fuzz)
(PR/Push)│                        │
         │                        ├──> [codegen-verify] (Build axiomgen & verify generation)
         │                        │
         │                        ├──> [consumer-test] (Isolated downstream module test)
         │                        │
         │                        └──> [bench-smoke] (Axiombench p50/p95/p99 regression)
         │                                       │
         └───────────── All Pass ────────────────┴──> [ci-gate] (Branch protection status)
```

---

## 3. Workflows Specification

| Workflow | File | Triggers | Purpose | Blocking |
| :--- | :--- | :--- | :--- | :--- |
| **CI** | `.github/workflows/ci.yml` | `push` [main], `pull_request` [main] | Fast DAG feedback: lint, hygiene, tests, race, examples, fuzzing, codegen, consumer, bench | **Yes** (via `ci-gate`) |
| **Security** | `.github/workflows/security.yml` | `push` [main], `pull_request` [main], Weekly cron | Secret scan (Gitleaks), Vulnerability scan (govulncheck), SAST (gosec) | **Yes** |
| **Release** | `.github/workflows/release.yml` | `push` [v*], `workflow_dispatch` | Cross-compile binaries, SHA256SUMS, CycloneDX SBOM, GHCR container, GitHub Release | **Yes** |
| **Nightly** | `.github/workflows/nightly.yml` | Nightly cron (02:00 UTC) | Extended fuzzing (60s+), full race tests on all packages, multi-OS matrix | No (Informational) |
| **Module Checksum** | `.github/workflows/module-checksum.yml` | `push` [main], `workflow_dispatch` | Compute pseudo-version and Go module proxy checksum verification | No |

---

## 4. Local Development & Parity Tooling

To ensure developers never face unexpected CI surprises, local workflows mirror CI targets directly.

### Linux / macOS (`Makefile` / `scripts/ci.sh`)

```bash
# Fast checks before commit (tidy, vet, lint, test)
make check

# Run individual stages
make fmt           # Format code
make lint          # Run golangci-lint
make test          # Run unit tests
make race          # Run race detector on critical runtime/store
make examples      # Run public examples verification
make fuzz-smoke    # Run short parser/compiler fuzzing
make consumer-test # Test importing as isolated Go module
make bench         # Run benchmark suite
make build         # Compile bin/axiomgen and bin/axiombench

# Full CI run locally
make ci
```

### Windows PowerShell (`scripts/dev.ps1`)

```powershell
# Fast check
.\scripts\dev.ps1 check

# Full CI suite
.\scripts\dev.ps1 ci

# Run specific target
.\scripts\dev.ps1 lint
.\scripts\dev.ps1 race
.\scripts\dev.ps1 examples
.\scripts\dev.ps1 build
```

---

## 5. Security & Supply Chain

### 1. Secret Scanning (Gitleaks)
Configured via `.gitleaks.toml`. Scans entire git commit history on PRs to prevent accidental credential leakage.

### 2. Dependency Vulnerability Analysis (govulncheck)
Analyzes Go dependencies and reachable code paths against the Go Vulnerability Database.

### 3. SAST (gosec)
Scans Go source code AST for security anti-patterns, integer overflows, insecure random sources, and unsafe file handling.

### 4. Software Bill of Materials (SBOM) & Checksums
- Generates **CycloneDX JSON** SBOM (`axiom-sbom.cdx.json`) for all release binaries.
- Generates cryptographic `SHA256SUMS` attached to each GitHub Release.

### 5. Multi-Arch Container Images
- `Dockerfile` utilizes minimal Google Distroless (`gcr.io/distroless/static-debian12:nonroot`) with non-root security context.
- Multi-arch builds (`linux/amd64`, `linux/arm64`) published to GitHub Container Registry (`ghcr.io/homiakus/axiomgen`).

---

## 6. Release Management & Procedures

Axiom follows Semantic Versioning (`vMAJOR.MINOR.PATCH`).

### Standard Release Workflow:
1. Ensure all changes are merged into `main` and CI is green.
2. Tag the release:
   ```bash
   git tag -a v0.1.0 -m "Release v0.1.0"
   git push origin v0.1.0
   ```
   *(Or trigger via **Actions -> release -> Run workflow** with version `v0.1.0`).*
3. The release workflow will automatically:
   - Verify candidate integrity and test suites.
   - Cross-compile `axiomgen` and `axiombench` for 5 OS/architecture pairs:
     - `linux/amd64`, `linux/arm64` (.tar.gz)
     - `darwin/amd64`, `darwin/arm64` (.tar.gz)
     - `windows/amd64` (.zip)
   - Compute `SHA256SUMS`.
   - Generate CycloneDX SBOM.
   - Build and publish multi-arch container to GHCR.
   - Create the GitHub Release with attached assets and release notes from `docs/releases/<version>.md`.

---

## 7. Rollback & Fault Tolerance

Because Axiom is a Go library and CLI toolset:
- **Go Module Consumers**: Can pin or revert to any prior SemVer tag or commit pseudo-version (e.g. `go get github.com/Homiakus/axiom@v0.1.0`).
- **Container Deployments**: Container tags are immutable (`ghcr.io/homiakus/axiomgen:v0.1.0` and `sha-<commit>`). If a regression occurs in a deployed service using the CLI container, roll back the image tag to the previous known-good tag.
- **GitHub Release Deprecation**: If a release contains critical bugs, mark it as `prerelease` or yank it via `gh release delete <tag>` while issuing a patch release `v0.1.1`.

---

## 8. Branch Protection Settings

Recommended GitHub repository rules for `main`:
1. **Require pull request before merging**:
   - Require at least 1 approval.
   - Dismiss stale pull request approvals when new commits are pushed.
2. **Require status checks to pass before merging**:
   - Required status check: `CI Completion Gate` (`ci-gate`).
   - Required status check: `Secret Scanning (Gitleaks)`.
   - Required status check: `Vulnerability Scan (govulncheck)`.
3. **Require branches to be up to date before merging**.
4. **Do not allow force pushes or branch deletion**.
