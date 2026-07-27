package axiom

import (
	"context"
	"testing"
)

func TestPebbleJSONReopenPreservesIntegerStateForNextDispatch(t *testing.T) {
	module, err := Compile([]byte(resilienceCounterSource))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store, err := OpenPebble(dir)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module, WithStore(store), WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Execution("counter").Dispatch(ctx, resilienceIncrement{By: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenPebble(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := New(module, WithStore(reopened), WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.Execution("counter").Dispatch(ctx, resilienceIncrement{By: 1}); err != nil {
		t.Fatal(err)
	}
	var state resilienceState
	if err := recovered.Execution("counter").State(ctx, &state); err != nil {
		t.Fatal(err)
	}
	if state.Value != 2 {
		t.Fatalf("state.Value = %d, want 2", state.Value)
	}
}
