package axiom

import (
	"context"
	"testing"

	"github.com/Homiakus/axiom/internal/testutil"
)

func TestRunStateMapReturnsIsolatedState(t *testing.T) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module)
	if err != nil {
		t.Fatal(err)
	}
	run := engine.Execution("state-map")
	if err := run.Signal(context.Background(), "Ping", nil); err != nil {
		t.Fatal(err)
	}

	state, status, err := run.StateMap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusWaiting {
		t.Fatalf("status = %s, want %s", status, StatusWaiting)
	}
	state["Counter"]["count"] = 999

	fresh, _, err := run.StateMap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fresh["Counter"]["count"] != 1 {
		t.Fatalf("stored state was mutated through StateMap: %#v", fresh)
	}
}
