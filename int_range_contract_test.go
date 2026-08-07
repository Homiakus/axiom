package axiom

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestAXMIntAcceptsUnsignedValuesWithinSigned64Range(t *testing.T) {
	module, err := Compile([]byte(`domain IntegerRange

context State:
  value: Int = 0
`))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "one", nil); err != nil {
		t.Fatal(err)
	}

	const maxSigned = uint64(math.MaxInt64)
	if err := engine.Patch(ctx, "one", Patch{"State.value": maxSigned}); err != nil {
		t.Fatalf("patch safe uint64: %v", err)
	}

	var state struct {
		Value int64 `json:"value"`
	}
	if err := engine.Execution("one").State(ctx, &state); err != nil {
		t.Fatal(err)
	}
	if state.Value != math.MaxInt64 {
		t.Fatalf("state.Value = %d, want %d", state.Value, int64(math.MaxInt64))
	}
}

func TestAXMIntRejectsUnsignedValueAboveSigned64Range(t *testing.T) {
	module, err := Compile([]byte(`domain IntegerRange

context State:
  value: Int = 0
`))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "one", nil); err != nil {
		t.Fatal(err)
	}

	tooLarge := uint64(math.MaxInt64) + 1
	err = engine.Patch(ctx, "one", Patch{"State.value": tooLarge})
	if err == nil {
		t.Fatal("expected signed Int range error")
	}
	if !strings.Contains(err.Error(), "AX406") || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}
