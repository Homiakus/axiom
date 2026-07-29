package testutil

import "fmt"

// ── Pre-built .axm sources of varying complexity ─────────────────────────────

// MinimalModule is the smallest valid .axm source: just a domain and one
// context field. Useful for testing the compiler fast-path and allocation
// baseline.
const MinimalModule = `domain Minimal

context State:
  value: Int = 0
`

// SimpleSignalModule adds a signal and a write-only rule — the simplest
// module that exercises Start → Signal → rule evaluation → write.
const SimpleSignalModule = `domain Simple

signal Ping

context Counter:
  count: Int = 0

rule onPing:
  on Ping
  write:
    Counter.count = Counter.count + 1
`

// MediumModule has multiple contexts, a computed, a fact, a claim, an
// activity, and two rules — representative of a real-world domain.
const MediumModule = `domain Medium

signal Start
signal Update:
  value: Int

context State:
  started: Bool = false
  total: Int = 0
  processed: Int = 0

computed ready: Bool =
  State.started

fact IsReady when:
  ready

policy fast:
  retry: 0
  timeout: 5s
  concurrency: once
  idempotency: required

activity Process:
  require:
    IsReady
  input:
    amount = State.total
  output:
    result: Int
  effect: external
  idempotencyKey: hash(State.total)
  policy: fast

rule captureStart:
  on Start
  write:
    State.started = true

rule processUpdate:
  on Update
  when:
    State.started
  write:
    State.total = State.total + signal.value

claim totalNonNegative:
  always:
    State.total >= 0
`

// GenerateWideModule creates a module with N context fields and N rules,
// each triggered by the same signal and writing to a different field.
// Useful for scaling benchmarks that test rule-queue and field-index performance.
func GenerateWideModule(numFields int) string {
	src := "domain Wide\n\nsignal Trigger\n\ncontext Data:\n"
	for i := 0; i < numFields; i++ {
		src += fmt.Sprintf("  field%d: Int = 0\n", i)
	}
	src += "\n"
	for i := 0; i < numFields; i++ {
		src += fmt.Sprintf("rule rule%d:\n  on Trigger\n  write:\n    Data.field%d = Data.field%d + 1\n\n", i, i, i)
	}
	return src
}

// GenerateDeepExprModule creates a module with a computed field that has
// a deeply nested boolean expression (chain of AND operations).  Useful
// for benchmarking expression evaluation and fast-plan compilation.
func GenerateDeepExprModule(depth int) string {
	src := "domain Deep\n\ncontext Data:\n"
	for i := 0; i < depth; i++ {
		src += fmt.Sprintf("  flag%d: Bool = true\n", i)
	}
	src += "\ncomputed allReady: Bool =\n  "
	for i := 0; i < depth; i++ {
		if i > 0 {
			src += " and "
		}
		src += fmt.Sprintf("Data.flag%d", i)
	}
	src += "\n"
	return src
}
