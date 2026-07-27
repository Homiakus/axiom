# Axiom performance and resilience baseline

This report is a reproducible CI baseline, not a hardware-independent service-level agreement.

- Generated: `2026-07-27T18:44:59Z`
- Go: `go1.26.5`
- Platform: GitHub-hosted `linux/amd64`
- Logical CPUs: `4`
- Concurrency: `8`, except replay
- Result: `0` operation errors; all state and replay invariants passed in strict mode

| Scenario | Operations | Throughput, ops/s | p50 | p95 | p99 | Maximum |
|---|---:|---:|---:|---:|---:|---:|
| Go-first flow, distinct executions | 20,000 | 9,028 | 53.1 µs | 3.841 ms | 4.788 ms | 10.045 ms |
| Go-first flow, one contended execution | 20,000 | 772 | 9.746 ms | 20.777 ms | 24.880 ms | 38.045 ms |
| Compiled runtime, distinct executions | 20,000 | 55,011 | 16.6 µs | 0.505 ms | 3.011 ms | 12.855 ms |
| Compiled runtime, one contended execution | 20,000 | 50,938 | 14.5 µs | 1.085 ms | 1.437 ms | 10.787 ms |
| Compiled runtime, cold memory execution | 5,000 | 40,239 | 22.9 µs | 0.800 ms | 4.058 ms | 10.879 ms |
| Pebble NoSync, cold durable execution | 1,000 | 8,773 | 0.108 ms | 3.904 ms | 5.061 ms | 5.658 ms |
| Pebble Sync, cold durable execution | 250 | 1,437 | 5.706 ms | 8.688 ms | 10.225 ms | 10.428 ms |
| Replay of a 1,000-event history | 200 runs | 761 runs/s | 1.081 ms | 1.977 ms | 2.541 ms | 2.760 ms |

## Interpretation

The compiled runtime has the strongest memory-path tail latency. Serializing changes to one execution preserves linearizable state without materially degrading the compiled runtime: p99 remained about `1.44 ms` at eight concurrent callers.

The Go-first flow frontend currently copies and serializes its complete history on every save. This makes a long-lived, highly contended execution increasingly expensive: p99 reached `24.88 ms`. Distinct executions had a p99 of `4.79 ms`. The result identifies append-only or chunked history storage as the next optimization target for the flow frontend.

Pebble durability cost is dominated by fsync. NoSync p99 was `5.06 ms`; fully synchronized writes reached `10.23 ms`. Applications must choose the durability mode according to their recovery-point objective.

## Resilience coverage

The test suite now verifies:

- concurrent updates to one Go-first execution do not get lost;
- concurrent updates to one compiled execution do not get lost;
- parallel Pebble executions cannot observe another transaction's in-memory state;
- a failed Go-first effect does not commit state or history;
- typed Go event integers remain integers at the runtime boundary;
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
