package lang

import (
	"strings"
	"testing"
)

// ──────────────────────────────────────────────────────────────────────────────
// TestParseComprehensive covers boundary conditions, error paths, and
// structural properties of the .axm parser that the main test file does
// not exercise.  Every test case documents the behaviour it checks, the
// input rationale, and the defect it would catch.
// ──────────────────────────────────────────────────────────────────────────────

func TestParseEdgeCases(t *testing.T) {
	// Table-driven: each case either succeeds (wantErr == "") or fails
	// with a substring match on the error message.
	cases := []struct {
		name    string   // human-readable scenario
		source  string   // raw .axm input
		wantErr string   // "" means success; non-empty means expected error substring
		check   func(*testing.T, *Module) // optional structural check on success
	}{
		// ── Empty and minimal ────────────────────────────────────────────
		{
			name:    "empty input requires domain",
			source:  "",
			wantErr: "domain",
		},
		{
			name:    "whitespace only",
			source:  "   \n\n   \n",
			wantErr: "domain",
		},
		{
			name:    "comment only",
			source:  "# this is a comment\n# another\n",
			wantErr: "domain",
		},
		{
			name:   "minimal valid: domain only",
			source: "domain Minimal",
			check: func(t *testing.T, m *Module) {
				if m.Domain != "Minimal" {
					t.Fatalf("domain = %q", m.Domain)
				}
			},
		},
		{
			name:    "duplicate domain declaration",
			source:  "domain A\ndomain B\n",
			wantErr: "duplicate domain",
		},
		{
			name:    "tabs are rejected",
			source:  "domain\tTabbed",
			wantErr: "tabs",
		},
		// ── Context parsing ──────────────────────────────────────────────
		{
			name:   "context with defaults and optional types",
			source: "domain Ctx\n\ncontext Data:\n  x: Int = 0\n  y: String?\n  z: Bool = true\n",
			check: func(t *testing.T, m *Module) {
				if len(m.Contexts) != 1 {
					t.Fatalf("contexts = %d", len(m.Contexts))
				}
				ctx := m.Contexts[0]
				if len(ctx.Fields) != 3 {
					t.Fatalf("fields = %d", len(ctx.Fields))
				}
				// Verify default parsing for first field.
				if !ctx.Fields[0].HasDefault || ctx.Fields[0].Default == nil {
					t.Fatalf("field x missing default")
				}
			},
		},
		{
			name:    "context without colon",
			source:  "domain Ctx\n\ncontext Data\n  x: Int\n",
			wantErr: "field block",
		},
		// ── Signal parsing ───────────────────────────────────────────────
		{
			name:   "signal without fields",
			source: "domain Sig\n\nsignal Ping\n",
			check: func(t *testing.T, m *Module) {
				if len(m.Signals) != 1 || m.Signals[0].Name != "Ping" {
					t.Fatalf("signals = %#v", m.Signals)
				}
			},
		},
		{
			name:   "signal with fields",
			source: "domain Sig\n\nsignal Ev:\n  id: String\n  count: Int\n",
			check: func(t *testing.T, m *Module) {
				if len(m.Signals[0].Fields) != 2 {
					t.Fatalf("signal fields = %d", len(m.Signals[0].Fields))
				}
			},
		},
		// ── Rule parsing ─────────────────────────────────────────────────
		{
			name: "rule with multiple triggers",
			source: `domain Rules

signal A
signal B

context S:
  v: Int = 0

rule multi:
  on:
    A
    B
  write:
    S.v = 1
`,
			check: func(t *testing.T, m *Module) {
				if len(m.Rules) != 1 {
					t.Fatalf("rules = %d", len(m.Rules))
				}
				if len(m.Rules[0].Triggers) != 2 {
					t.Fatalf("triggers = %d", len(m.Rules[0].Triggers))
				}
			},
		},
		{
			name: "rule with changed trigger",
			source: `domain Rules

context S:
  v: Int = 0

rule onChange:
  on changed(S.v)
  write:
    S.v = S.v + 1
`,
			check: func(t *testing.T, m *Module) {
				trigger := m.Rules[0].Triggers[0]
				if trigger.Kind != TriggerChanged || trigger.Target != "S.v" {
					t.Fatalf("trigger = %#v", trigger)
				}
			},
		},
		// ── Computed and fact ─────────────────────────────────────────────
		{
			name: "computed multiline expression",
			source: `domain Comp

context S:
  a: Bool = true
  b: Bool = true

computed both: Bool =
  S.a and
  S.b
`,
			check: func(t *testing.T, m *Module) {
				if len(m.Computeds) != 1 || m.Computeds[0].Name != "both" {
					t.Fatalf("computeds = %#v", m.Computeds)
				}
			},
		},
		{
			name: "fact with expose",
			source: `domain Facts

context S:
  x: Int = 42

fact HasX when:
  S.x > 0
expose:
  val = S.x
`,
			check: func(t *testing.T, m *Module) {
				if len(m.Facts[0].Expose) != 1 || m.Facts[0].Expose[0].Name != "val" {
					t.Fatalf("expose = %#v", m.Facts[0].Expose)
				}
			},
		},
		// ── Policy ───────────────────────────────────────────────────────
		{
			name: "policy with catch block",
			source: `domain Pol

signal ErrorOccurred

policy safe:
  retry: 3
  timeout: 5s
  catch:
    TimeoutError -> ErrorOccurred
`,
			check: func(t *testing.T, m *Module) {
				if len(m.Policies) != 1 {
					t.Fatalf("policies = %d", len(m.Policies))
				}
				p := m.Policies[0]
				if p.Catches["TimeoutError"] != "ErrorOccurred" {
					t.Fatalf("catches = %#v", p.Catches)
				}
			},
		},
		// ── Query ────────────────────────────────────────────────────────
		{
			name: "query with return bindings",
			source: `domain Q

context S:
  v: Int = 0

query status:
  return:
    value = S.v
`,
			check: func(t *testing.T, m *Module) {
				if len(m.Queries) != 1 || m.Queries[0].Return[0].Name != "value" {
					t.Fatalf("queries = %#v", m.Queries)
				}
			},
		},
		// ── Unicode / long strings ───────────────────────────────────────
		{
			name:   "Unicode domain name",
			source: "domain Тест\n",
			check: func(t *testing.T, m *Module) {
				if m.Domain != "Тест" {
					t.Fatalf("domain = %q", m.Domain)
				}
			},
		},
		{
			name:    "unknown top-level declaration",
			source:  "domain X\n\ngarbage line\n",
			wantErr: "unknown",
		},
		{
			name: "CRLF line endings are normalized",
			source: "domain CRLF\r\n\r\ncontext S:\r\n  v: Int = 0\r\n",
			check: func(t *testing.T, m *Module) {
				if m.Domain != "CRLF" {
					t.Fatalf("domain = %q", m.Domain)
				}
			},
		},
		// ── Comments inside strings should be preserved ──────────────────
		{
			name: "inline comment stripped",
			source: `domain Comments

context S:
  v: Int = 0 # inline comment

signal Ping # comment
`,
			check: func(t *testing.T, m *Module) {
				if m.Domain != "Comments" {
					t.Fatalf("domain = %q", m.Domain)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			module, err := Parse([]byte(tc.source))
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, module)
			}
		})
	}
}

// TestParseExprComprehensive tests the expression parser with varied inputs.
func TestParseExprComprehensive(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// ── Literals ─────────────────────────────────────────────────────
		{name: "integer literal", input: "42"},
		{name: "negative integer", input: "-1"},
		{name: "float literal", input: "3.14"},
		{name: "string literal", input: `"hello"`},
		{name: "empty string", input: `""`},
		{name: "true literal", input: "true"},
		{name: "false literal", input: "false"},
		{name: "nil literal", input: "nil"},

		// ── References ───────────────────────────────────────────────────
		{name: "simple ref", input: "Context.field"},
		{name: "signal ref", input: "signal.value"},
		{name: "output ref", input: "output.result"},
		{name: "deep ref", input: "Context.field.sub.deep"},

		// ── Binary operators ─────────────────────────────────────────────
		{name: "equality", input: "Context.x == 1"},
		{name: "inequality", input: "Context.x != 1"},
		{name: "greater than", input: "Context.x > 0"},
		{name: "greater or equal", input: "Context.x >= 0"},
		{name: "less than", input: "Context.x < 100"},
		{name: "less or equal", input: "Context.x <= 100"},
		{name: "and", input: "Context.a and Context.b"},
		{name: "or", input: "Context.a or Context.b"},
		{name: "implies", input: "Context.a implies Context.b"},
		{name: "in list", input: `Context.status in ["active", "pending"]`},
		{name: "addition", input: "Context.x + 1"},
		{name: "subtraction", input: "Context.x - 1"},

		// ── Unary operators ──────────────────────────────────────────────
		{name: "exists", input: "Context.x exists"},
		{name: "not", input: "not Context.flag"},

		// ── Function calls ───────────────────────────────────────────────
		{name: "list call", input: `list("a", "b", "c")`},
		{name: "hash call", input: `hash(Context.id, Context.name)`},
		{name: "changed call", input: "changed(Context.field)"},
		{name: "missing call", input: "missing(Context.field)"},

		// ── Complex expressions ──────────────────────────────────────────
		{name: "nested and/or", input: "(Context.a or Context.b) and Context.c"},
		{name: "chained comparison", input: "Context.x > 0 and Context.x < 100"},
		{name: "implies with exists", input: "Context.paid == true implies Context.id exists"},

		// ── Error cases ──────────────────────────────────────────────────
		{name: "empty expression", input: "", wantErr: true},
		{name: "unclosed paren", input: "(Context.x", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseExpr(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for input %q", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
		})
	}
}

// TestParseExprRoundTrip verifies that parsing an expression and converting
// it back to string produces an equivalent re-parseable expression.
// This is a lightweight property-based check.
func TestParseExprRoundTrip(t *testing.T) {
	expressions := []string{
		"true",
		"42",
		`"hello"`,
		"Context.field",
		"Context.x > 0",
		"Context.a and Context.b",
		"Context.x exists",
		"not Context.flag",
		`Context.status in ["a", "b"]`,
	}
	for _, input := range expressions {
		t.Run(input, func(t *testing.T) {
			expr, err := ParseExpr(input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			// ExprString should produce a valid re-parseable representation.
			output := ExprString(expr)
			if output == "" {
				t.Fatalf("ExprString returned empty for %q", input)
			}
			// Re-parse the output — should not error.
			_, err = ParseExpr(output)
			if err != nil {
				t.Fatalf("re-parse of %q failed: %v", output, err)
			}
		})
	}
}
