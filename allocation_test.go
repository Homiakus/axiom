package axiom

import (
	"context"
	"testing"

	"github.com/Homiakus/axiom/internal/testutil"
)

// Allocation tests assert bounded allocation counts for hot operations.
// Ceilings deliberately retain headroom for compiler/runtime variation while
// remaining close enough to the measured baseline to catch meaningful
// regressions. When a ceiling needs to move, record the new measured baseline
// in the change rather than widening it pre-emptively.
const (
	maxCompileMinimalAllocs = 200.0 // measured baseline: ~142
	maxNewEngineAllocs      = 50.0  // measured baseline: ~27
	maxSignalSimpleAllocs   = 160.0 // measured baseline: ~117
	maxPatchSimpleAllocs    = 100.0 // measured baseline: ~66
	maxQueryStateAllocs     = 50.0  // measured baseline: ~26
)

func TestAllocsCompileMinimalModule(t *testing.T) {
	source := []byte(testutil.MinimalModule)
	// Warm up — first compile may trigger lazy initializations.
	_, _ = Compile(source)

	allocs := testing.AllocsPerRun(50, func() {
		_, _ = Compile(source)
	})
	t.Logf("Compile(MinimalModule) allocs/op = %.1f", allocs)

	if allocs > maxCompileMinimalAllocs {
		t.Errorf("Compile(MinimalModule) allocs = %.1f, want <= %.0f", allocs, maxCompileMinimalAllocs)
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

	if allocs > maxNewEngineAllocs {
		t.Errorf("New(MinimalModule) allocs = %.1f, want <= %.0f", allocs, maxNewEngineAllocs)
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

	if allocs > maxSignalSimpleAllocs {
		t.Errorf("Signal(Ping) allocs = %.1f, want <= %.0f", allocs, maxSignalSimpleAllocs)
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

	if allocs > maxPatchSimpleAllocs {
		t.Errorf("Patch allocs = %.1f, want <= %.0f", allocs, maxPatchSimpleAllocs)
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

	if allocs > maxQueryStateAllocs {
		t.Errorf("Query(state) allocs = %.1f, want <= %.0f", allocs, maxQueryStateAllocs)
	}
}
