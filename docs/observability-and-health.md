# Observability, Metrics Cardinality & Health Probes Contract

Status: **Canonical Specification (T-070 / OPS-001 / OPS-002)**  
Scope: `axiom` Core + `adgo` Distributed Engine + Storage Backends + Health Endpoints

---

## 1. Metric Cardinality Contract (OPS-001)

High-cardinality label explosions in metrics collectors (e.g. Prometheus, OpenTelemetry) degrade monitoring performance and risk out-of-memory crashes. Axiom strictly regulates allowed and prohibited metric label dimensions across all engines and stores.

### 1.1. Bounded Label Dimensions (Permitted)

| Label Key | Value Domain | Maximum Cardinality | Description |
|---|---|---|---|
| `state` | `initialized`, `running`, `suspended`, `completed`, `failed`, `canceled` | 6 | Execution lifecycle state. |
| `failure_class` | `transient`, `rate_limit`, `invalid_input`, `quality`, `permanent`, `ambiguous_side_effect` | 6 | Standard ADGO failure taxonomy. |
| `store_type` | `memory`, `file`, `pebble` | 3 | Storage backend driver. |
| `activity_name` | Static module activity identifiers (e.g. `Charge`, `Notify`) | Bounded by compiled DSL/Plan definition ($< 100$) | Activity name from module schema. |
| `operation` | `get`, `put`, `commit`, `rollback`, `claim`, `fence`, `outbox_drain` | 7 | Core store / engine operation name. |
| `status_code` | `ok`, `retry_scheduled`, `stale_task`, `conflict`, `admission_denied`, `error` | 6 | Coarse result status code. |

### 1.2. Unbounded Dimensions (Strictly Prohibited)

The following parameters must **NEVER** be used as metric label dimensions:
- ❌ **Execution IDs** (`execution_id` / `id` / UUIDs);
- ❌ **Task IDs** (`task_id`);
- ❌ **User Data / Payload Fields** (e.g. customer IDs, order amounts, JSON keys);
- ❌ **Arbitrary Error Strings** (use normalized `failure_class` or `status_code` instead);
- ❌ **Dynamic Filepaths / URIs** (e.g. user-supplied paths).

---

## 2. Standard Engine Metrics

| Metric Identifier | Type | Labels | Description |
|---|---|---|---|
| `axiom_executions_total` | Counter | `state` | Total executions started / transitioned. |
| `axiom_execution_duration_seconds` | Histogram | `state` | End-to-end execution wall-clock duration. |
| `axiom_activity_attempts_total` | Counter | `activity_name`, `status_code` | Number of activity task attempts executed. |
| `axiom_activity_failures_total` | Counter | `activity_name`, `failure_class` | Activity failures categorized by failure class. |
| `axiom_store_operations_total` | Counter | `store_type`, `operation`, `status` | Storage backend operations. |
| `axiom_store_latency_seconds` | Histogram | `store_type`, `operation` | Storage commit and read latency percentiles (p50, p95, p99). |
| `axiom_outbox_backlog_size` | Gauge | `store_type` | Number of pending durable Flow outbox effects. |
| `axiom_lease_fencing_events_total` | Counter | `store_type`, `reason` | Worker lease expirations and fencing rejections. |
| `axiom_admission_denials_total` | Counter | `limit_type` | Tasks rejected due to rate limits or concurrency ceilings. |

---

## 3. Liveness versus Readiness Separation (OPS-002)

To prevent cascading restart storms in orchestration clusters (e.g. Kubernetes, Nomad), Axiom strictly decouples **Liveness** from **Readiness**.

```text
┌─────────────────────────────────────────────────────────────┐
│                       Host Process                          │
│                                                             │
│   ┌─────────────────────┐       ┌───────────────────────┐   │
│   │   Liveness Probe    │       │    Readiness Probe    │   │
│   │  (Process Health)   │       │  (Work Acceptance)    │   │
│   └──────────┬──────────┘       └───────────┬───────────┘   │
│              │                              │               │
│              ▼                              ▼               │
│      Process running?              Store connected?         │
│      Deadlock-free?                Schema compatible?       │
│      Heap within limits?           Lease healthy?           │
│                                    Outbox draining?         │
│                                    Capacity available?      │
└─────────────────────────────────────────────────────────────┘
```

### 3.1. Liveness Probe (`/healthz` or `Engine.Live()`)
- **Intent**: Answers *"Is this process alive and responsive, or has it deadlocked / crashed?"*
- **Action on Failure**: Supervisor restarts / terminates container.
- **Evaluation**:
  - Engine internal event loops and scheduler routines are unblocked.
  - Heartbeat timestamp is actively updating.
- **Invariant**: **Never fail liveness on external dependency failure** (e.g. remote database timeout, worker queue full). A degraded process must remain alive to drain in-flight operations.

### 3.2. Readiness Probe (`/ready` or `Engine.Ready()`)
- **Intent**: Answers *"Can this node safely accept and execute new execution tasks right now?"*
- **Action on Failure**: Traffic load balancer removes node from active routing until ready.
- **Evaluation Criteria**:
  1. **Storage Connectivity & Sync**: Store is open, non-corrupt, and responding within timeout.
  2. **Schema & Codec Compatibility**: Valid format markers (`meta/axiom-store-schema` = `"1"`); no unknown future schemas.
  3. **Lease / Fencing Health**: If operating as coordinator/worker, local clock is monotonically synchronized and lease lock is held.
  4. **Outbox Backlog Level**: Durable outbox queue depth is below the critical backpressure threshold.
  5. **Admission Capacity**: Concurrency and memory budgets have available headroom.
