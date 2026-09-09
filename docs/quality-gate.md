# Integration-Completeness Quality Gate

Status: **Canonical Process Specification (T-086 / GATE-001)**  
Scope: All existing and future production features in Axiom.  
Enforcing CI Test: [`algorithm_integration_matrix_test.go`](../algorithm_integration_matrix_test.go) (`TestAlgorithmIntegrationMatrixIntegrity`)  
Inventory: [`docs/algorithm-integration-matrix.json`](algorithm-integration-matrix.json)

---

## 1. Purpose & Motivation

During early iterations of complex systems, implementation often advances faster than public integration, documentation, and operational ergonomics. This creates an "integration gap": features exist in internal packages, but users cannot easily discover, configure, observe, or safely operate them.

The **Integration-Completeness Quality Gate (T-086)** establishes a non-negotiable rule:
> **No production feature may be marked `DONE` in `MASTER_PLAN.md` without satisfying and mechanically proving all eight dimensions of integration completeness.**

---

## 2. The Eight Mandatory Gate Requirements

Every production feature in [`docs/algorithm-integration-matrix.json`](algorithm-integration-matrix.json) must explicitly define and pass verification on the following 8 dimensions:

| Dimension | Field in Matrix | Requirement & Validation Standard |
|---|---|---|
| **1. API Status & Tier** | `api_status` | Must be classified into a canonical tier (`stable_facade`, `advanced`, or `extension_spi`) and registered in [`docs/api-tiering.json`](api-tiering.json). |
| **2. Activation Policy** | `mode` | Must declare whether it is active by `default` or requires `opt-in` configuration. |
| **3. Durable-State Impact** | `durable_state_impact` | Non-empty description of exact persistence effects: tables, keys, CAS versions, WAL records, and replay guarantees. |
| **4. Failure Semantics** | `failure_semantics` | Non-empty explicit contract: `fail_closed`, `retry_with_backoff`, `unwind_compensation_stack`, `circuit_break`, or `rate_limit_throttle`. |
| **5. Observability** | `observability_refs` | At least one valid path to documentation or tests covering Prometheus metrics, cardinality guards, or health probes. |
| **6. Automated Tests** | `test_refs` | At least one valid path to automated unit, integration, or chaos test files exercising the feature. |
| **7. Architectural Documentation** | `doc_refs` | At least one valid path to an architectural guide, runbook, or specification explaining the algorithm. |
| **8. Reference Usage** | `reference_usage_refs` | At least one valid path to a runnable standalone example or production reference application (`examples/`). |

---

## 3. Mechanical CI Verification

The gate is not an informal checklist; it is enforced mechanically by automated tests in CI:

1. **`algorithm_integration_matrix_test.go`**:
   - Asserts that all 24 canonical features exist.
   - Validates that `api_status` is one of `stable_facade`, `advanced`, `extension_spi`.
   - Validates that `durable_state_impact` and `failure_semantics` are non-empty.
   - Validates that every referenced file across `implementation_refs`, `public_api_refs`, `test_refs`, `doc_refs`, `example_refs`, `observability_refs`, and `reference_usage_refs` exists on disk and is a valid file.

2. **`api_tiering_test.go`**:
   - Asserts that 100% of public exported symbols in the compatibility manifest are classified.
   - Validates that low-level types map to the high-level feature requiring them.

3. **`api_compatibility_test.go`**:
   - Enforces backward compatibility against `testdata/compat/public_api_manifest.txt`.

4. **`docs_integrity_test.go`**:
   - Enforces UTF-8 cleanliness and cross-references between all canonical documentation files.

---

## 4. Definition of Done Checklist for New Features

When adding or expanding a feature in Axiom:

- [ ] Core implementation placed in `internal/` or `pkg/`.
- [ ] Exported facade provided in high-level package (`profile`, `model`, `axiom`, or `adgo`).
- [ ] Symbol added and classified in [`docs/api-tiering.json`](api-tiering.json).
- [ ] Conformance tests written and passing uncached in CI.
- [ ] State mutations, failure semantics, and cardinality bounds documented.
- [ ] Feature entry added to [`docs/algorithm-integration-matrix.json`](algorithm-integration-matrix.json) with all 8 fields populated.
- [ ] `go test -v ./...` passes cleanly with zero missing file warnings.
