package axiom

import (
	"testing"
)

// ──────────────────────────────────────────────────────────────────────────────
// FuzzCompile feeds arbitrary bytes to the public Compile() function.
// The compiler pipeline: Parse → CompileAST → buildPlan → should never panic.
// ──────────────────────────────────────────────────────────────────────────────

func FuzzCompile(f *testing.F) {
	f.Add([]byte("domain Test"))
	f.Add([]byte(welcomeRuntimeSource))
	f.Add([]byte(checkoutRuntimeSource))
	f.Add([]byte(""))
	f.Add([]byte("domain\n\n"))
	f.Add([]byte("domain X\n\ncontext S:\n  x: Int = 0\n\nsignal P\n\nrule r:\n  on P\n  write:\n    S.x = S.x + 1\n"))
	f.Add([]byte("domain X\n\tTABS"))
	f.Add([]byte("not valid at all"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Compile(data)
	})
}

// FuzzCompileAny tests the auto-detecting compiler that handles both
// v0 Axiom and TRIZ DSL input.
func FuzzCompileAny(f *testing.F) {
	f.Add([]byte("domain Test"))
	f.Add([]byte("system Switcher\nstate S:\n  x: Int = 0\n"))
	f.Add([]byte(""))
	f.Add([]byte("garbled input {{{{"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = CompileAny(data)
	})
}
