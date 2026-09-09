# Production Reference Application (DeployGuard / Incident Remediation)

Status: **active reference implementation**  
Location: [`examples/production-refapp/`](../examples/production-refapp/)  
Test Suite: [`examples/production-refapp/app_test.go`](../examples/production-refapp/app_test.go)

This document describes the canonical end-to-end production reference application for Axiom and ADGO. It demonstrates how all 14 advanced runtime, coordination, resilience, and persistence algorithms compose into a unified, crash-safe distributed pipeline.

---

## 1. Composition Overview

The reference scenario models an **Automated Incident Remediation and Safe Production Deployment Pipeline**:

```
[StartOrLoad]
      │
      ▼
┌──────────────────┐
│ FetchTelemetry   │  ◄── Transient Retry (attempt 1 fails, attempt 2 succeeds)
└─────────┬────────┘
          │
          ▼
┌──────────────────┐
│ AcquireAdmission │  ◄── AdmissionController & Rate Limiting (concurrency + burst quota)
└─────────┬────────┘
          │
          ▼
┌─────────────────────┐
│ AnalyzeDiagnostics  │  ◄── Pure Result Cache + Speculative Hedging (Pure=true enforced)
└─────────┬───────────┘
          │
          ▼
┌──────────────────┐
│  GeneratePatch   │  ◄── Adaptive Provider Fallback + Targeted Repair Loop (Anchor)
└─────────┬────────┘
          │
          ▼
┌──────────────────┐
│   QualityGate    │  ◄── Hard Floors (Confidence >= 0.90, MaxCriticalErrors: 0)
└─────────┬────────┘
          │ (Passes on iteration 2 after repair)
          ▼
┌──────────────────┐
│   AuditSubflow   │  ◄── Child Workflow Delegation (NodeSubflow)
└─────────┬────────┘
          │
          ▼
┌──────────────────┐
│ ProvisionCanary  │  ◄── External Effect + Idempotency Key + Saga Compensation (RollbackCanary)
└─────────┬────────┘
          │
          ▼
┌──────────────────┐
│  HumanApproval   │  ◄── High-Risk Decision Pause (NodeHuman, ResolveHuman)
└─────────┬────────┘
          │ (Approved by operator)
          ▼
┌──────────────────┐
│ WaitVerification │  ◄── Durable Event Signal (NodeWait, VerificationSignal)
└─────────┬────────┘
          │ (Signal delivered)
          ▼
┌──────────────────┐
│ PromoteProduction│  ◄── Final Promotion & State Finalization
└─────────┬────────┘
          │
          ▼
    [Completed]
```

---

## 2. Tested Algorithmic Invariants

| Capability | Implementation & Mechanism | Conformance Verification |
|---|---|---|
| **1. Durable Start / Restart** | `profile.DurableSingleNode` + `PebbleStore` + `StartOrLoad` | `TestProductionRefApp_CrashRecoveryCheckpoint`: mid-flight process termination, reopening Pebble WAL, monotonic state preserved. |
| **2. Worker Lease & Fencing** | `WorkerSpec{LeaseTTL: ...}` + fencing counter | `TestProductionRefApp_WorkerFencing`: stale worker token fails completion with `ErrStaleTask`. |
| **3. Transient Retry** | `RetryPolicy{MaxAttempts: 3, BaseDelay: 10ms}` + `FailureTransient` | `state.TelemetryAttempts >= 2` assertion in E2E suite. |
| **4. Rate Limits & Admission** | `AdmissionController.Acquire(...)` with `MaxConcurrent` and `Burst` | `state.AdmissionCalls >= 1` in E2E suite. |
| **5. Adaptive Routing & Fallback** | `adgo.AdaptiveRouter` with `Resolve(...)` and health ranking | Health scoring and fallback provider invocation during `GeneratePatch`. |
| **6. Pure Result Cache** | `adgo.ActivityCache` (`NewMemoryActivityCache` / `NewFileActivityCache`) | Cache lease claims and hit on subsequent calls. |
| **7. Hedged / Ensemble Speculation** | `adgo.NewHedgedActivity` with `SpeculationPolicy{Pure: true}` | Fastest variant wins; conservative budget draining. |
| **8. Quality Gate + Repair Loop** | `QualityGateSpec` with `RepairFrom` + `LoopBound` | Iteration 1 produces low confidence (0.75), repaired to 0.98 in iteration 2. |
| **9. Human Approval Pause** | `NodeHuman` + `ResolveHuman(HumanApprove)` | Workflow enters `StatusHuman`, halts execution until explicitly resolved. |
| **10. External Effect & Compensation** | `ExternalEffect: true`, idempotency token, `RollbackCanary` | `TestProductionRefApp_CompensationOnAbort`: `Cancel` unwinds reverse saga stack. |
| **11. Durable Timer / Signal** | `NodeWait` + `Engine.Signal(VerificationSignal)` | Paused execution resumes upon durable signal delivery. |
| **12. Child Workflow (Subflow)** | `NodeSubflow` + `Registry.Subflow("AuditSubflow", ...)` | Delegated execution produces durable audit receipt. |
| **13. Metrics, Diagnostics & Budgets** | `Engine.Diagnostics` + `BudgetLimit{MaxCost: ...}` | Bounded budget assertion: total cost <= limit ($1.05 <= $10.00). |
| **14. Retention Lifecycle** | `adgo.CollectExecutions` with `RetentionPolicy` | `TestProductionRefApp_RetentionPruning`: terminal executions collected cleanly. |

---

## 3. Safety Shield: Speculation Invariant

Speculative hedging against activities with external side effects is mathematically dangerous because multiple variants could execute concurrently. ADGO strictly rejects speculation unless `Pure: true` is explicitly configured:

```go
_, err := adgo.NewHedgedActivity(variants, adgo.SpeculationPolicy{
    Pure: false, // Rejected with error: "adgo: hedged execution requires Pure=true"
})
```
Verified in `TestProductionRefApp_NoUnsafeSideEffectSpeculation`.

---

## 4. Expected History Lifecycle

A successful run produces an immutable sequence of audit events:
1. `execution_started`
2. `activity_enqueued` / `task_claimed` / `activity_completed` (`FetchTelemetry` attempt 1: failure transient)
3. `activity_retry` -> `activity_completed` (`FetchTelemetry` attempt 2: success)
4. `activity_completed` (`AcquireAdmission`)
5. `activity_completed` (`AnalyzeDiagnostics`)
6. `activity_completed` (`GeneratePatch` iteration 1)
7. `repair_planned` (`QualityGate` triggers targeted repair loop)
8. `activity_completed` (`GeneratePatch` iteration 2)
9. `subflow_completed` (`AuditSubflow`)
10. `activity_completed` (`ProvisionCanary`, compensation registered)
11. `human_wait` (`human_approval`, execution pauses at `StatusHuman`)
12. `human_resolved` (`operator@axiom.internal` approves)
13. `event_wait` (`wait_verification`)
14. `event_ingested` / `node_completed` (`VerificationSignal` delivered)
15. `activity_completed` (`PromoteProduction`)
16. `execution_completed`

All steps are verified deterministically in `TestProductionRefApp_DeterministicEndToEnd`.
