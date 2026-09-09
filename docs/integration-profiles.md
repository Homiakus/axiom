# Supported Integration Profiles

Axiom provides three canonical integration profiles to simplify adoption while preserving strict separation of underlying engine semantics:

1. **Embedded** — In-process, memory-backed execution runtime with zero infrastructure dependencies.
2. **Durable Single Node** — Synchronous Pebble-backed durable runtime for standalone services requiring restart and crash recovery.
3. **Distributed Production** — Enterprise runtime featuring coordinator and worker separation, lease fencing, adaptive routing, and admission control.

---

## 1. Quick Comparison Matrix

| Attribute | Profile 1: Embedded | Profile 2: Durable Single Node | Profile 3: Distributed Production |
|---|---|---|---|
| **Package** | `github.com/Homiakus/axiom/profile` | `github.com/Homiakus/axiom/profile` | `github.com/Homiakus/axiom/profile` |
| **Constructor** | `profile.NewEmbedded()` / `OpenEmbedded` | `profile.OpenDurableSingleNode(cfg)` | `profile.OpenDistributedProduction(plan, reg, cfg)` |
| **Primary Backend** | In-Memory (`MemoryStore` / `MemoryFlowStore`) | Local Pebble DB (`PebbleStore` / `PebbleFlowStore`) | Distributed/Shared Store (`PebbleStore` / future PostgreSQL) |
| **Durability Level** | `StoreDurabilityEphemeral` | `StoreDurabilitySynchronous` (fsync WAL) | Synchronous with leased fences |
| **State Latency** | Sub-microsecond (< 5 µs) | Microsecond to sub-millisecond | Network/disk bound (1–10 ms) |
| **Crash Recovery** | None (ephemeral) | Full state & outbox recovery upon restart | Worker crash reclamation & coordinator failover |
| **Concurrency** | In-process goroutines | Multi-goroutine, single-process lock | Horizontally scalable coordinator & worker pools |
| **Target Use Case** | Unit testing, CLIs, local embedded agents | Edge daemons, single-host durable microservices | Production clusters, high-scale mission-critical workflows |

---

## 2. Profile 1: Embedded

The **Embedded** profile is designed for applications where maximum speed, zero disk footprint, and immediate setup are required.

### Canonical Constructor
```go
import (
    "github.com/Homiakus/axiom"
    "github.com/Homiakus/axiom/profile"
)

emb := profile.NewEmbedded()
defer emb.Close()

// Run Flow
engine, err := profile.OpenEmbeddedFlow(emb, myFlow)

// Or run declarative .axm
axmEngine, err := emb.CompileAndNew(axmSource)
```

### Guarantees
- **Sub-microsecond latency:** In-memory transitions execute with zero I/O overhead.
- **Zero dependencies:** No filesystem directories, external daemons, or network ports required.
- **Thread safety:** Safe for concurrent invocations across goroutines.
- **Deterministic ordering:** Exact event sequencing and claim validation matching durable semantics.

### Non-Guarantees
- **No crash persistence:** All execution state, events, and pending effects are lost when the process terminates.
- **No multi-host dispatch:** Cannot distribute tasks to remote worker nodes.
- **No persistent outbox:** External side-effects are executed inline and not journaled across restarts.

---

## 3. Profile 2: Durable Single Node

The **Durable Single Node** profile provides synchronous Pebble-backed persistence. It is the recommended default for production microservices running on a single host or container with persistent storage volumes.

### Canonical Constructor
```go
import (
    "github.com/Homiakus/axiom"
    "github.com/Homiakus/axiom/profile"
)

p, err := profile.OpenDurableSingleNode(profile.DurableSingleNodeConfig{
    Dir:        "/var/lib/axiom/data",
    SyncWrites: true, // synchronous fsync on commit
})
if err != nil {
    log.Fatal(err)
}
defer p.Close()

// Open durable Flow with transactional outbox
flowEngine, err := profile.OpenDurableFlow(p, myFlow)

// Or open durable declarative engine
axmEngine, err := p.CompileAndNew(axmSource)
```

### Guarantees
- **Crash boundary survival:** All committed state transitions, history entries, and pending outbox intents survive process crashes, SIGKILL, and system restarts.
- **Synchronous WAL durability:** Writes are flushed to disk before acknowledgement (`StoreDurabilitySynchronous`).
- **Monotonic state sequencing:** Versions and event sequence numbers are strictly monotonic and idempotent on replay.
- **Single-writer mutual exclusion:** Pebble file locks prevent dual-writer corruptions if multiple processes attempt to access the same directory.

### Non-Guarantees
- **No multi-host concurrent writers:** Active-active multi-node writes to the same local directory are unsupported and blocked by Pebble file locks.
- **No automatic failover:** Requires external disk replication or backup strategies for hardware node disaster recovery.

---

## 4. Profile 3: Distributed Production

The **Distributed Production** profile separates orchestration coordinators from task workers. It is built for multi-process, horizontally scalable environments.

### Canonical Constructor
```go
import (
    "github.com/Homiakus/axiom/adgo"
    "github.com/Homiakus/axiom/profile"
)

cfg := profile.DefaultDistributedProductionConfig("node-alpha-01", "/shared/axiom/data")
cfg.PollInterval = 50 * time.Millisecond

prod, err := profile.OpenDistributedProduction(plan, registry, cfg)
if err != nil {
    log.Fatal(err)
}
defer prod.Close()

// Run coordinator and worker services concurrently
err := prod.Serve(ctx, adgo.WorkerSpec{
    ID:          "worker-pool-1",
    Activities:  []string{"charge_payment", "send_receipt"},
    Concurrency: 16,
    LeaseTTL:    30 * time.Second,
})
```

### Guarantees
- **Decoupled coordinator & workers:** Coordinators manage DAG scheduling while workers independently poll, execute, and scale.
- **Leased task fencing:** Workers acquire time-bounded fencing tokens. Stale, slow, or network-partitioned workers cannot commit results after lease expiration.
- **Automatic work reclamation:** Crashed workers that stop sending heartbeats have their tasks automatically recovered and reassigned.
- **Adaptive provider routing:** Integrated circuit breaking, EWMA latency tracking, and error penalty scoring.
- **Global admission control:** Rate limits and concurrency quotas protect downstream services from overload.

### Non-Guarantees
- **External side-effect idempotency:** Network calls to external third-party systems cannot be automatically rolled back; applications must provide compensation handlers or idempotency keys.
- **Shared disk multi-master:** When backed by Pebble, nodes require partitioned directories or dedicated coordinator nodes. A distributed network store (e.g. PostgreSQL backend in T-084) is required for shared multi-host state access.

---

## 5. Migration Paths Between Profiles

All three profiles share identical domain modeling semantics:
- Moving from **Embedded** to **Durable Single Node** requires only specifying a persistent directory (`profile.DurableSingleNodeConfig{Dir: ...}`); business logic and Flow reducers remain 100% unchanged.
- Moving from **Durable Single Node** to **Distributed Production** decouples activity handlers into `adgo.WorkerSpec` worker processes and introduces lease-based fencing and adaptive provider routing.
