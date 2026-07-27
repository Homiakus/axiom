# Axiom performance and resilience baseline

This report is a reproducible CI baseline, not a hardware-independent service-level agreement.

- Generated: `2026-07-27T18:41:54Z`
- Go: `go1.26.5`
- Platform: GitHub-hosted `linux/amd64`
- Logical CPUs: `4`
- Concurrency: `8`, except replay
- Result: `0` operation errors; all state and replay invariants passed

| Scenario | Operations | Throughput, ops/s | p50 | p95 | p99 | Maximum |
|---|---:|---:|---:|---:|---:|---:|
| Go-first flow, distinct executions | 20,000 | 8,207 | 49.3 µs | 4.106 ms | 5.342 ms | 10.019 ms |
| Go-first flow, one contended execution | 20,000 | 819 | 8.788 ms | 20.516 ms | 25.120 ms | 33.517 ms |
| Compiled runtime, distinct executions | 20,000 | 48,062 | 15.8 µs | 0.564 ms | 3.425 ms | 16.740 ms |
| Compiled runtime, one contended execution | 20,000 | 46,795 | 14.7 µs | 1.132 ms | 1.529 ms | 10.973 ms |
| Compiled runtime, cold memory execution | 5,000 | 35,544 | 22.8 µs | 0.854 ms | 4.498 ms | 17.083 ms |
| Pebble NoSync, cold durable execution | 1,000 | 7,165 | 0.140 ms | 4.519 ms | 5.426 ms | 7.768 ms |
| Pebble Sync, cold durable execution | 250 | 1,038 | 7.538 ms | 10.872 ms | 11.945 ms | 12.998 ms |
| Replay of a 1,000-event history | 200 runs | 707 runs/s | 1.153 ms | 2.231 ms | 2.314 ms | 2.602 ms |

## Interpretation

The compiled runtime has the strongest memory-path tail latency. Serializing changes to one execution preserves linearizable state without materially degrading the compiled runtime: p99 remained about `1.53 ms` at eight concurrent callers.

The Go-first flow frontend currently copies and serializes its complete history on every save. This makes a long-lived, highly contended execution increasingly expensive: p99 reached `25.12 ms`. Distinct executions still have a p99 of `5.34 ms`. The result identifies history storage as the next optimization target for the flow frontend.

Pebble durability cost is dominated by fsync. NoSync p99 was `5.43 ms`; fully synchronized writes reached `11.95 ms`. Applications must choose the durability mode according to their recovery-point objective.

## Resilience coverage

The test suite now verifies:

- concurrent updates to one Go-first execution do not get lost;
- concurrent updates to one compiled execution do not get lost;
- parallel Pebble executions cannot observe another transaction's in-memory state;
- a failed Go-first effect does not commit state or history;
- default Pebble JSON storage preserves integer types across close and reopen;
- replay reconstructs the exact final state;
- a 16-worker, 8,000-operation flow soak preserves every update;
- critical packages pass Go's race detector.

## Reproduce

```bash
go run ./cmd/axiombench \
  -memory-ops 20000 \
  -pebble-ops 1000 \
  -replay-events 1000 \
  -replay-runs 200 \
  -concurrency 8 \
  -strict=true \
  -json benchmark-results.json \
  -markdown benchmark-results.md
```

For comparable trend data, use dedicated runners with pinned CPU frequency, storage and Go version. Shared GitHub runners are suitable for correctness and coarse regression detection, but not for tight latency budgets.
