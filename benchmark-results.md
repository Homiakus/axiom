# Axiom performance and resilience report

Generated: `2026-09-02T05:46:50Z`  
Go: `go1.26.5`  
Platform: `windows/amd64`, CPUs: `8`

| Scenario | Ops | C | Throughput ops/s | p50 µs | p95 µs | p99 µs | Max µs | Errors | Invariant |
|---|---:|---:|---:|---:|---:|---:|---:|---:|:---:|
| flow_memory_distinct | 10000 | 8 | 709527 | 0.0 | 0.0 | 503.8 | 2010.6 | 0 | PASS |
| flow_memory_same_execution | 10000 | 8 | 568822 | 0.0 | 0.0 | 546.0 | 1506.8 | 0 | PASS |
| runtime_memory_distinct | 10000 | 8 | 70313 | 0.0 | 1001.4 | 1575.0 | 10880.1 | 0 | PASS |
| runtime_memory_same_execution | 10000 | 8 | 71968 | 0.0 | 1004.0 | 1995.7 | 3520.2 | 0 | PASS |
| runtime_memory_cold | 2500 | 8 | 53954 | 0.0 | 1004.6 | 1504.6 | 2184.7 | 0 | PASS |
| runtime_pebble_nosync_cold | 500 | 8 | 11351 | 0.0 | 2603.2 | 4053.5 | 5007.5 | 0 | PASS |
| runtime_pebble_sync_cold | 125 | 8 | 1705 | 4527.5 | 6032.7 | 6778.0 | 7344.0 | 0 | PASS |
| pebble_reopen_durable | 100 | 1 | 144 | 5347.5 | 8062.0 | 62020.8 | 77427.0 | 0 | PASS |
| replay_history | 50 | 1 | 2728 | 517.2 | 620.1 | 1000.8 | 1000.8 | 0 | PASS |
| adgo_memory_workflow | 1000 | 8 | 502 | 8599.5 | 47677.6 | 53811.2 | 59118.1 | 0 | PASS |

## Invariants

- `flow_memory_distinct`: expected `10000`, actual `10000` — **true**.
- `flow_memory_same_execution`: expected `10000`, actual `10000` — **true**.
- `runtime_memory_distinct`: expected `10000`, actual `10000` — **true**.
- `runtime_memory_same_execution`: expected `10000`, actual `10000` — **true**.
- `replay_history`: expected `500`, actual `500` — **true**.
