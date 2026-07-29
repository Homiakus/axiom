package axiom

import (
	"context"
	"testing"

	"github.com/Homiakus/axiom/internal/testutil"
)

// ──────────────────────────────────────────────────────────────────────────────
// Allocation tests: measure and assert allocation counts for key operations.
// These serve as regression tests — if someone accidentally adds an allocation
// to a hot path, these tests will catch it.
//
// Every threshold is measured empirically and set slightly above the current
// baseline.  On updates, run with -v to see the actual alloc count and adjust.
// ──────────────────────────────────────────────────────────────────────────────

func TestAllocsCompileMinimalModule(t *testing.T) {
	source := []byte(testutil.MinimalModule)
	// Warm up — first compile may trigger lazy initializations.
	_, _ = Compile(source)

	allocs := testing.AllocsPerRun(50, func() {
		_, _ = Compile(source)
	})
	t.Logf("Compile(MinimalModule) allocs/op = %.1f", allocs)

	// Generous ceiling: compile involves parsing, AST alloc, maps, hashing.
	// Typical: ~150-300 allocs for a minimal module.
	if allocs > 500 {
		t.Errorf("Compile(MinimalModule) allocs = %.1f, want <= 500", allocs)
	}
}

func TestAllocsNewEngineMinimalModule(t *testing.T) {
	module, err := Compile([]byte(testutil.MinimalModule))
	if err != nil {
		t.Fatal(err)
	}
	// Warm up.
	_, _ = New(module)

	allocs := testing.AllocsPerRun(50, func() {
		_, _ = New(module)
	})
	t.Logf("New(MinimalModule) allocs/op = %.1f", allocs)

	if allocs > 200 {
		t.Errorf("New(MinimalModule) allocs = %.1f, want <= 200", allocs)
	}
}

func TestAllocsSignalSimple(t *testing.T) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "allocs-signal", nil); err != nil {
		t.Fatal(err)
	}
	// Warm up.
	for i := 0; i < 20; i++ {
		_ = engine.Signal(ctx, "allocs-signal", "Ping", nil)
	}

	allocs := testing.AllocsPerRun(100, func() {
		_ = engine.Signal(ctx, "allocs-signal", "Ping", nil)
	})
	t.Logf("Signal(Ping) allocs/op = %.1f", allocs)

	// Signal involves: load execution, clone context, eval rules, save.
	// Typical: ~50-150 allocs for a simple signal.
	if allocs > 300 {
		t.Errorf("Signal(Ping) allocs = %.1f, want <= 300", allocs)
	}
}

func TestAllocsPatchSimple(t *testing.T) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "allocs-patch", nil); err != nil {
		t.Fatal(err)
	}
	// Warm up.
	for i := 0; i < 20; i++ {
		_ = engine.Patch(ctx, "allocs-patch", Patch{"Counter.count": i})
	}

	allocs := testing.AllocsPerRun(100, func() {
		_ = engine.Patch(ctx, "allocs-patch", Patch{"Counter.count": 42})
	})
	t.Logf("Patch(Counter.count) allocs/op = %.1f", allocs)

	if allocs > 300 {
		t.Errorf("Patch allocs = %.1f, want <= 300", allocs)
	}
}

func TestAllocsQueryState(t *testing.T) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "allocs-query", nil); err != nil {
		t.Fatal(err)
	}
	// Warm up.
	for i := 0; i < 10; i++ {
		_, _ = engine.Query(ctx, "allocs-query", "state")
	}

	allocs := testing.AllocsPerRun(100, func() {
		_, _ = engine.Query(ctx, "allocs-query", "state")
	})
	t.Logf("Query(state) allocs/op = %.1f", allocs)

	if allocs > 150 {
		t.Errorf("Query(state) allocs = %.1f, want <= 150", allocs)
	}
}
