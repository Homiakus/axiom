package lang

import "testing"

// ── Fuzz tests for the DSL parser ────────────────────────────────────────────
// These tests verify that arbitrary byte input never causes a panic in the
// parser. The parser should either return a valid *Module or a non-nil error.
// Any panic, deadlock, or OOM on a finite input is a defect.

// FuzzParse feeds arbitrary bytes to the full .axm module parser.
// Primary goal: no panics. Secondary goal: no hangs.
func FuzzParse(f *testing.F) {
	// Seed corpus with valid and semi-valid inputs to guide the fuzzer
	// toward interesting code paths.
	f.Add([]byte("domain Test"))
	f.Add([]byte("domain T\n\ncontext S:\n  x: Int = 0\n"))
	f.Add([]byte("domain T\n\nsignal Ping\n"))
	f.Add([]byte("domain T\n\nsignal Ev:\n  id: String\n"))
	f.Add([]byte("domain T\n\nrule r:\n  on Ping\n  write:\n    S.x = 1\n"))
	f.Add([]byte("domain T\n\nfact F when:\n  S.x > 0\n"))
	f.Add([]byte("domain T\n\ncomputed c: Bool = true\n"))
	f.Add([]byte("domain T\n\nclaim cl:\n  always:\n    true\n"))
	f.Add([]byte("domain T\n\nquery q:\n  return:\n    v = 1\n"))
	f.Add([]byte("")) // empty input
	f.Add([]byte("not a valid module at all!!!"))
	f.Add([]byte("domain\n\n\n\n"))
	f.Add([]byte("domain X\n\ncontext :\n  : \n"))
	f.Add([]byte("domain X\n\t\ttabs here"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// The only contract: no panic, no hang. Errors are expected.
		_, _ = Parse(data)
	})
}

// FuzzParseExpr feeds arbitrary strings to the expression parser.
// The expression parser is a particularly rich attack surface because it
// handles user-provided expressions with nested parentheses, operators,
// and function calls.
func FuzzParseExpr(f *testing.F) {
	f.Add("true")
	f.Add("false")
	f.Add("42")
	f.Add(`"hello world"`)
	f.Add("Context.field")
	f.Add("Context.x > 0 and Context.y < 100")
	f.Add("Context.a implies Context.b")
	f.Add(`Context.status in ["a", "b", "c"]`)
	f.Add("Context.x exists")
	f.Add("not Context.flag")
	f.Add("missing(Context.field)")
	f.Add("changed(Context.field)")
	f.Add(`hash(Context.id, "salt")`)
	f.Add(`list("a", "b")`)
	f.Add("(((())))")
	f.Add("")
	f.Add("))))(((")
	f.Add(`"unterminated string`)
	f.Add("Context.x ++++++ Context.y")

	f.Fuzz(func(t *testing.T, input string) {
		// No panic, no hang. Errors are fine.
		_, _ = ParseExpr(input)
	})
}
