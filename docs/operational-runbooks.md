# Production Incident Response & Operational Runbooks

Status: **Canonical Operational Guide (T-071 / OPS-003)**  
Scope: `axiom` Core + `adgo` Distributed Engine + Storage Backends (`pebble`, `file`)

---

## 1. Overview and Incident Management Principles

This document provides step-by-step troubleshooting, diagnosis, and remediation procedures for operators managing Axiom and ADGO orchestration runtimes in production.

All remediation actions adhere to the **Durable Safety Invariant**:
- Never bypass format verification or delete database records without a certified cold snapshot.
- Favor graceful drain and worker retirement over forceful process termination when possible.
- All schema/format changes must go through standard migration paths.

---

## 2. Emergency Operational Procedures Matrix

| Scenario | Severity | Trigger / Symptoms | Immediate Containment |
|---|---|---|---|
| **1. Stuck / Expired Leases** | High | Activity task pending with expired lease; worker unresponsive. | Verify worker death; trigger lease reassignment; check UTC wall clock monotonicity. |
| **2. Stale FileStore / Admission Locks** | Medium | `ErrAdmissionDenied` or lock acquisition timeout after unexpected host crash. | Verify PID ownership; clear orphaned lock file `.lock` if owner PID is deceased. |
| **3. Corrupted Pebble / JSON Records** | Critical | `axiom pebble: unsupported persisted format` or unparseable JSON error. | Isolate node (mark unready); verify disk integrity; restore from recent backup or rebuild snapshot. |
| **4. Future / Unsupported Schema Marker** | High | Engine refuses to start: `unsupported store schema: "99"`. | Check deployment version; rollback binary if an unreleased binary was mistakenly deployed. |
| **5. Retry Storm** | High | High CPU/IO; spike in `axiom_activity_attempts_total` with `AX505` or `FailureTransient`. | Activate circuit breaker via policy; increase backoff coefficient; inspect downstream dependency. |
| **6. Outbox Backlog / Poison Effect** | High | `axiom_outbox_backlog_size` continuously climbing; pending external effects stuck. | Inspect outbox diagnostics; identify failing destination; trigger outbox replay with fixed worker. |
| **7. Repair Exhaustion** | High | ADGO node reports `ErrDeadlock` / max repairs exceeded without converging. | Inspect node repair trace; emit human intervention request; update TRIZ domain rules. |
| **8. Retention / Truncation Mistakes** | Critical | History query fails on active execution; data pruned prematurely. | Freeze retention cleaner; restore missing WAL/history segments from backup. |
| **9. Failed Store Migration** | Critical | Migration script aborts midway; store left in partial state. | Roll back transaction; verify marker `meta/*`; do not force reopen on modified uncommitted schemas. |
| **10. Release Rollback** | High | Production regressions discovered post-deploy. | Verify storage format compatibility with previous binary; swap container image; monitor health probes. |
| **11. Backup / Restore Verification** | Operational | Periodic cold/hot store backup test. | Replay execution verification test against restored snapshot in isolated staging. |

---

## 3. Detailed Step-by-Step Runbooks

### 3.1. Stuck or Expired Leases (Worker Fencing)
**Symptoms**:
- `axiom_lease_fencing_events_total` increases.
- Tasks remain in `claimed` state without advancing.

**Action**:
1. Check if the original worker node is responsive:
   ```bash
   # Check worker heartbeat or process status
   ps aux | grep axiom-worker
   ```
2. If the worker process has died or lost network connectivity, the coordinator automatically reclaims the task once `deadline <= NowSource.Now()` (or `LeaseWallNow()`).
3. If split-brain worker attempts to commit after expiry, the engine returns `ErrExternalActivityClaimStale` or `adgo.ErrStaleTask` and rejects the write.

### 3.2. Stale FileStore / Admission Locks
**Symptoms**:
- Subprocess crashes leave `.lock` files in the data directory.
- New engines fail with admission denied or lock contention.

**Action**:
1. Inspect the lock owner PID stored inside the lock descriptor.
2. Confirm the process ID does not belong to a running Axiom instance.
3. Remove the orphaned `.lock` file safely.

### 3.3. Outbox Backlog Draining (Durable Flow)
**Symptoms**:
- Outbox gauge `axiom_outbox_backlog_size` exceeds configured alarm threshold ($> 500$).

**Action**:
1. Inspect outbox state via `engine.Outbox().Diagnostics(ctx)`.
2. Check for poison effects (malformed payloads or unresponsive external endpoints).
3. If an external endpoint was repaired, invoke `engine.Outbox().Drain(ctx)` to flush pending intents with deduplicated effect IDs.

### 3.4. Release Rollback Procedure
**Symptoms**:
- Critical regression in application logic after binary deployment.

**Action**:
1. Check `docs/versioning.md` to confirm the target rollback version uses the same schema version (`meta/axiom-store-schema` = `"1"`).
2. Drain active worker traffic by setting readiness probe to failing.
3. Switch container image to previous stable tag (e.g. `v0.1.0`).
4. Re-enable readiness probe and verify that `axiom_executions_total` resumes normally.
