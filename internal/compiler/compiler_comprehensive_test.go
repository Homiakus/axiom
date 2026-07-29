package compiler

import (
	"strings"
	"testing"
)

// ──────────────────────────────────────────────────────────────────────────────
// TestCompileComprehensive tests the compiler with table-driven scenarios
// for diagnostic codes (AX001-AX306), cycle detection, and index generation.
// ──────────────────────────────────────────────────────────────────────────────

func TestCompileDiagnostics(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		wantCode string // expected diagnostic code, e.g. "AX001"
	}{
		{
			name: "AX001 unresolved signal trigger reference",
			source: `domain AX001Test

rule badRule:
  on NonExistentSignal
  write:
    State.x = 1
`,
			wantCode: "AX001",
		},
		{
			name: "AX001 unresolved context in changed trigger",
			source: `domain AX001Test2

rule badRule:
  on changed(NonExistent.field)
  write:
    State.x = 1
`,
			wantCode: "AX001",
		},
		{
			name: "AX002 duplicate declaration",
			source: `domain AX002Test

signal Ping
signal Ping
`,
			wantCode: "AX002",
		},
		{
			name: "AX003 duplicate field in context",
			source: `domain AX003Test

context State:
  field1: Int
  field1: String
`,
			wantCode: "AX003",
		},
		{
			name: "AX201 cyclic computed dependency",
			source: `domain AX201Test

context State:
  x: Int = 0

computed a: Bool = b
computed b: Bool = a
`,
			wantCode: "AX201",
		},
		{
			name: "AX202 cyclic fact dependency",
			source: `domain AX202Test

fact FactA when:
  FactB

fact FactB when:
  FactA
`,
			wantCode: "AX202",
		},
		{
			name: "AX204 signal ref outside signal rule",
			source: `domain AX204Test

context State:
  x: Int = 0

rule badSignalRef:
  on changed(State.x)
  write:
    State.x = signal.something
`,
			wantCode: "AX204",
		},
		{
			name: "AX301 invalid write target",
			source: `domain AX301Test

signal Ping

rule badWrite:
  on Ping
  write:
    NonExistentContext.field = 1
`,
			wantCode: "AX301",
		},
		{
			name: "AX302 non-existent output field",
			source: `domain AX302Test

signal Ping

context State:
  x: Int = 0

activity DoWork:
  output:
    realField: Int
  effect: local

rule runWork:
  on Ping
  run: DoWork
  write:
    State.x = output.fakeField
`,
			wantCode: "AX302",
		},
		{
			name: "AX303 rule run non-activity",
			source: `domain AX303Test

signal Ping

context State:
  x: Int = 0

rule runNonActivity:
  on Ping
  run: NonExistentActivity
  write:
    State.x = 1
`,
			wantCode: "AX303",
		},
		{
			name: "AX304 external activity missing policy",
			source: `domain AX304Test

activity ExternalNoPolicy:
  output:
    ok: Bool
  effect: external
`,
			wantCode: "AX304",
		},
		{
			name: "AX305 external activity idempotency key missing",
			source: `domain AX305Test

policy requireIdem:
  idempotency: required

activity ExternalNoKey:
  output:
    ok: Bool
  effect: external
  policy: requireIdem
`,
			wantCode: "AX305",
		},
		{
			name: "AX306 catch target signal non-existent",
			source: `domain AX306Test

policy badCatch:
  catch:
    SomeError -> NonExistentSignal
`,
			wantCode: "AX306",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile([]byte(tc.source))
			if err == nil {
				t.Fatalf("expected diagnostic error containing %s, got nil", tc.wantCode)
			}
			diags, ok := err.(Diagnostics)
			if !ok {
				t.Fatalf("error type = %T, want Diagnostics", err)
			}
			found := false
			for _, d := range diags {
				if d.Code == tc.wantCode {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("diagnostics = %#v, want code %s", diags, tc.wantCode)
			}
		})
	}
}

// TestCompileSuccessStructure verifies that a valid compilation populates
// all ID tables, dependency indexes, and hashes correctly.
func TestCompileSuccessStructure(t *testing.T) {
	source := []byte(`domain StructureTest

signal Trigger

context Data:
  value: Int = 0

computed isPositive: Bool =
  Data.value > 0

fact PositiveFact when:
  isPositive

rule increment:
  on Trigger
  write:
    Data.value = Data.value + 1

claim nonNegative:
  always:
    Data.value >= 0
`)

	module, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	// ── Hashes ───────────────────────────────────────────────────────────
	if module.SourceHash == "" {
		t.Fatal("SourceHash is empty")
	}
	if module.CompiledHash == "" {
		t.Fatal("CompiledHash is empty")
	}

	// ── ID Tables ────────────────────────────────────────────────────────
	if len(module.IDs.Fields) != 1 || module.IDs.Fields[0] != "Data.value" {
		t.Fatalf("FieldIDs = %#v", module.IDs.Fields)
	}
	if len(module.IDs.Signals) != 1 || module.IDs.Signals[0] != "Trigger" {
		t.Fatalf("SignalIDs = %#v", module.IDs.Signals)
	}
	if len(module.IDs.Rules) != 1 || module.IDs.Rules[0] != "increment" {
		t.Fatalf("RuleIDs = %#v", module.IDs.Rules)
	}

	// ── Indexes ──────────────────────────────────────────────────────────
	if len(module.Indexes.SignalIndex["Trigger"]) != 1 {
		t.Fatalf("SignalIndex[Trigger] = %#v", module.Indexes.SignalIndex["Trigger"])
	}
	if len(module.Indexes.WriteTargetIndex["Data.value"]) != 1 {
		t.Fatalf("WriteTargetIndex[Data.value] = %#v", module.Indexes.WriteTargetIndex["Data.value"])
	}
	if len(module.Indexes.ClaimIndex["Data.value"]) != 1 {
		t.Fatalf("ClaimIndex[Data.value] = %#v", module.Indexes.ClaimIndex["Data.value"])
	}
}

// TestCompileHashDeterminism verifies that compiling identical sources
// produces identical CompiledHash values.
func TestCompileHashDeterminism(t *testing.T) {
	source := []byte(`domain Determinism

context S:
  v: Int = 0
`)

	m1, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}

	if m1.CompiledHash != m2.CompiledHash {
		t.Fatalf("CompiledHash mismatch: %q != %q", m1.CompiledHash, m2.CompiledHash)
	}
	if m1.SourceHash != m2.SourceHash {
		t.Fatalf("SourceHash mismatch: %q != %q", m1.SourceHash, m2.SourceHash)
	}
}

// TestCompileInvalidSyntax verifies handling of invalid parser input.
func TestCompileInvalidSyntax(t *testing.T) {
	_, err := Compile([]byte("not a valid .axm file\t\twith tabs"))
	if err == nil {
		t.Fatal("expected error for invalid syntax")
	}
	if !strings.Contains(err.Error(), "AX000") {
		t.Fatalf("error = %q, want code AX000", err.Error())
	}
}
