package triz

import "testing"

// ── Fuzz test for the TRIZ normalizer ────────────────────────────────────────
// The normalizer accepts user-provided TRIZ DSL text and produces v0 Axiom
// source. It must never panic on arbitrary input.

func FuzzNormalize(f *testing.F) {
	// Seed corpus: valid TRIZ, partial TRIZ, v0 Axiom source, empty, garbage.
	f.Add([]byte(`system Switcher

state System:
  running: Bool = false

event Start(zone: Int)

profile critical:
  timeout: 10s
  retry: 0
  once
  idempotent

function TurnOn(zone: Int) -> { ok: Bool }

rule ApplyTurnOn when:
  Start
do critical:
  result = TurnOn(event.zone)
then:
  set System.running = result.ok

always RunningIsBool:
  not (System.running == true and System.running == false)

view Dashboard:
  running = System.running
`))
	f.Add([]byte("system Empty"))
	f.Add([]byte(""))
	f.Add([]byte("not triz at all"))
	f.Add([]byte("system\n\n"))
	f.Add([]byte("system X\nstate S:\n  x: Int\nevent E(v: Int)\nrule R when:\n  E\nthen:\n  set S.x = event.v\n"))
	f.Add([]byte("system X\nstate S[zone]:\n  value: Int = 0\n"))
	f.Add([]byte("system X\ncondition Ready when:\n  true\n"))
	f.Add([]byte("system X\nfunction F(a: Int, b: Int) -> { sum: Int }\n"))
	f.Add([]byte("system X\nview V:\n  v = 1\n"))
	f.Add([]byte("system X\nalways Safe:\n  true\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Contract: never panic. Errors and nil results are fine.
		_, _ = Normalize(data)
	})
}

// FuzzLooksLike ensures the detection heuristic doesn't panic.
func FuzzLooksLike(f *testing.F) {
	f.Add([]byte("system X"))
	f.Add([]byte("domain X"))
	f.Add([]byte(""))
	f.Add([]byte("garbage"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_ = LooksLike(data)
	})
}
