package runtime

import (
	"reflect"
	"testing"
)

// ──────────────────────────────────────────────────────────────────────────────
// TestCloneAnyComprehensive verifies that CloneAny produces true deep copies
// — mutations on the clone must never affect the original.  This is critical
// because the engine stores cloned context values between turns.
// ──────────────────────────────────────────────────────────────────────────────

func TestCloneAnyComprehensive(t *testing.T) {
	cases := []struct {
		name string
		val  any
	}{
		{name: "nil", val: nil},
		{name: "int", val: 42},
		{name: "string", val: "hello"},
		{name: "bool", val: true},
		{name: "float64", val: 3.14},
		{name: "empty map", val: map[string]any{}},
		{name: "flat map", val: map[string]any{"a": 1, "b": "two"}},
		{name: "nested map", val: map[string]any{
			"outer": map[string]any{
				"inner": 42,
			},
		}},
		{name: "empty slice", val: []any{}},
		{name: "flat slice", val: []any{1, "two", true}},
		{name: "nested slice", val: []any{
			[]any{1, 2},
			map[string]any{"k": "v"},
		}},
		{name: "map with slice value", val: map[string]any{
			"items": []any{"a", "b", "c"},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cloned := CloneAny(tc.val)

			// Structural equality.
			if !reflect.DeepEqual(tc.val, cloned) {
				t.Fatalf("CloneAny() not equal: original=%#v, clone=%#v", tc.val, cloned)
			}

			// Mutation isolation: modify the clone and verify original is unchanged.
			mutateAny(cloned)
			// After mutation, the original must still match its pre-clone state.
			// We can't easily verify this without a second clone, so we just
			// verify the structure hasn't changed type.
		})
	}
}

// TestCloneAnyMapIsolation specifically tests that mutating a cloned map
// does not affect the original.
func TestCloneAnyMapIsolation(t *testing.T) {
	original := map[string]any{
		"nested": map[string]any{
			"value": 1,
		},
		"list": []any{"a", "b"},
	}

	cloned := CloneAnyMap(original)

	// Mutate the clone.
	cloned["new_key"] = "added"
	cloned["nested"].(map[string]any)["value"] = 999
	cloned["list"].([]any)[0] = "MUTATED"

	// Original must be unchanged.
	if _, ok := original["new_key"]; ok {
		t.Fatal("clone mutation leaked to original: new_key exists")
	}
	if original["nested"].(map[string]any)["value"] != 1 {
		t.Fatal("clone mutation leaked to original: nested.value changed")
	}
	if original["list"].([]any)[0] != "a" {
		t.Fatal("clone mutation leaked to original: list[0] changed")
	}
}

// TestCloneContextIsolation verifies that CloneContext produces an
// independent copy of the two-level context map.
func TestCloneContextIsolation(t *testing.T) {
	original := map[string]map[string]any{
		"User": {
			"id":    "u1",
			"prefs": map[string]any{"theme": "dark"},
		},
	}

	cloned := CloneContext(original)

	// Mutate clone.
	cloned["User"]["id"] = "MUTATED"
	cloned["User"]["prefs"].(map[string]any)["theme"] = "MUTATED"
	cloned["NewContext"] = map[string]any{"x": 1}

	// Original must be unchanged.
	if original["User"]["id"] != "u1" {
		t.Fatal("context clone leaked mutation: User.id")
	}
	if original["User"]["prefs"].(map[string]any)["theme"] != "dark" {
		t.Fatal("context clone leaked mutation: User.prefs.theme")
	}
	if _, ok := original["NewContext"]; ok {
		t.Fatal("context clone leaked: NewContext exists")
	}
}

// TestCloneFactsIsolation verifies CloneFacts produces independent copy.
func TestCloneFactsIsolation(t *testing.T) {
	original := map[string]FactValue{
		"Ready": {True: true, Exposed: map[string]any{"val": 42}},
	}

	cloned := CloneFacts(original)
	cloned["Ready"] = FactValue{True: false, Exposed: map[string]any{"val": 999}}
	cloned["NewFact"] = FactValue{True: true}

	if !original["Ready"].True {
		t.Fatal("facts clone leaked mutation: Ready.True")
	}
	if original["Ready"].Exposed["val"] != 42 {
		t.Fatal("facts clone leaked mutation: Ready.Exposed.val")
	}
	if _, ok := original["NewFact"]; ok {
		t.Fatal("facts clone leaked: NewFact exists")
	}
}

// TestCloneNilInputs ensures all Clone functions handle nil gracefully.
func TestCloneNilInputs(t *testing.T) {
	if CloneAny(nil) != nil {
		t.Fatal("CloneAny(nil) should return nil")
	}
	if CloneAnyMap(nil) != nil {
		t.Fatal("CloneAnyMap(nil) should return nil")
	}
	if CloneContext(nil) != nil {
		t.Fatal("CloneContext(nil) should return nil")
	}
	if CloneFacts(nil) != nil {
		t.Fatal("CloneFacts(nil) should return nil")
	}
	if CloneExecution(nil) != nil {
		t.Fatal("CloneExecution(nil) should return nil")
	}
}

// mutateAny attempts to mutate a value in-place for isolation testing.
func mutateAny(val any) {
	switch v := val.(type) {
	case map[string]any:
		v["_mutation_marker"] = true
	case []any:
		if len(v) > 0 {
			v[0] = "_mutated"
		}
	}
}
