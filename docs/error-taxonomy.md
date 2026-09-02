# Axiom Error Taxonomy and Classification Contract

Status: **Canonical Architectural Specification (T-051 / ERR-003)**  
Scope: `axiom` Core + `adgo` Distributed Engine + Storage Backends + Compilers

---

## 1. Overview and Design Principles

Axiom separates errors into four explicit categories according to their operational and semantic intent:

1. **Deterministic Domain & Diagnostic Errors (`diag.Error`)**:
   - Structured compiler, parser, validation, and domain rule errors carrying machine-parseable error codes (e.g. `AX505`).
   - Safe for programmatic inspection and branching.

2. **Durable Control-Flow Sentinels (`errors.Is`)**:
   - Sentinels representing normal state machine transitions or lifecycle events (e.g. `ErrRetryScheduled`, `ErrNoWork`, `ErrStaleTask`).
   - Must be preserved across error wrapping chains via standard `errors.Is`.

3. **Typed Failure Classes (`adgo.FailureClass`)**:
   - Structured activity failure classification for distributed workers (`FailureTransient`, `FailureRateLimit`, `FailurePermanent`, `FailureInvalidInput`, `FailureQuality`, `FailureAmbiguousSideEffect`).
   - Determines automated retry policies, backoff calculation, and circuit breaker health decay.

4. **Fail-Closed Persistence & Infrastructure Errors**:
   - Storage format mismatches, unrecoverable corruption, concurrent CAS conflicts (`ErrConflict`), and fatal IO failures.
   - Guaranteed to abort transactions without partial state corruption.

---

## 2. Comprehensive Taxonomy Matrix

| Category | Identifier / Type | Scope | Caller Branching Safety | Transaction Policy | Behavior & Rationale |
|---|---|---|---|---|---|
| **Compilation & Syntax** | `diag.Error`, `compiler.Diagnostics` | Core DSL / AXM | **Stable** (`Code`, `Message`, `Line`) | N/A (Pre-execution) | Emitted during AST parsing, type checking, or TRIZ normalization. |
| **Retryable Activity (Core)** | `diag.Error{Code: "AX505"}` / `ErrRetryScheduled` | Core Runtime | **Stable** (`errors.Is(err, ErrRetryScheduled)`) | **Commit State** (`ShouldCommitState() == true`) | Indicates activity entered durable backoff queue; execution state and task attempt are committed. |
| **Transient Activity (ADGO)** | `FailureTransient`, `FailureRateLimit` | ADGO Workers | **Stable** (`adgo.Fail(FailureTransient, err)`) | **Commit State** (Records failure and schedules retry) | Triggers exponential backoff with deterministic jitter; updates provider health. |
| **Terminal Activity (ADGO)** | `FailurePermanent`, `FailureInvalidInput`, `FailureQuality` | ADGO Workers | **Stable** (`adgo.Fail(FailurePermanent, err)`) | **Commit Failure** (Halts node or executes compensation) | Non-retryable error; fails node permanently without burning retry budgets. |
| **Ambiguous Side Effect** | `FailureAmbiguousSideEffect` | ADGO Workers | **Stable** | **Failsafe Wait / Compensation** | Worker crashed after external action before acknowledgement; requires idempotency key. |
| **Optimistic Concurrency** | `adgo.ErrConflict` | Core & ADGO Store | **Stable** (`errors.Is(err, ErrConflict)`) | **Rollback & Retry** | CAS version mismatch during concurrent execution mutation; caller retries with refreshed version. |
| **Stale Lease & Fencing** | `ErrExternalActivityClaimStale`, `adgo.ErrStaleTask` | External Workers | **Stable** (`errors.Is(err, ErrStaleTask)`) | **Reject Mutation** | Worker lease expired or attempt was reissued to a replacement worker; protects against split-brain writes. |
| **Format & Schema Mismatch** | `axiom pebble: unsupported store schema`, `incomplete persisted format marker` | Storage (`pebble`) | **Fatal** | **Fail-Closed (Abort Open)** | Database schema version or codec mismatch; store refuses to open to prevent data corruption. |
| **Durability Capability** | `ErrNoSyncRejected`, `ErrBufferedPebbleRejected` | Production Mode | **Stable** | **Fail Fast** | Production mode rejects un-synced or non-durable store configurations. |
| **Budget & Resource Limit** | `adgo.ErrBudgetExceeded`, `adgo.ErrDeadlock` | Coordinator | **Stable** (`errors.Is`) | **Terminal State** | Execution exceeded configured cost/token budget or reached unresolvable dependency cycle. |
| **Admission Control** | `adgo.ErrAdmissionDenied` | ADGO Admission | **Stable** (`errors.Is(err, ErrAdmissionDenied)`) | **Reject Request** | Concurrency or rate-limit ceiling reached; caller should back off and retry. |

---

## 3. Standard Inspection and Branching Contracts

### 3.1. Testing for Sentinels
Callers should always use Go standard `errors.Is`:

```go
if errors.Is(err, axiom.ErrExternalActivityClaimStale) {
    // Handle lease loss / worker fencing
}
if errors.Is(err, adgo.ErrConflict) {
    // Refresh snapshot and retry optimistic commit
}
```

### 3.2. Inspecting Diagnostic Codes
For Core diagnostic errors, extract `diag.Error`:

```go
var diagErr diag.Error
if errors.As(err, &diagErr) {
    switch diagErr.Code {
    case "AX505":
        // Retryable activity timeout / transient error
    default:
        // Other domain rule violations
    }
}
```

### 3.3. Classifying Activity Failures in ADGO
Workers classify errors explicitly or rely on default classification:

```go
return adgo.Fail(adgo.FailureTransient, fmt.Errorf("remote service unavailable"))
```
