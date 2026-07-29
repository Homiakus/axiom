package axiom

import (
	"context"
	"fmt"
	"testing"

	"github.com/Homiakus/axiom/internal/testutil"
)

// ──────────────────────────────────────────────────────────────────────────────
// Scaling benchmarks: measure throughput at different scale points (10, 100,
// 1K, 10K) for core engine operations. All benchmarks use b.ReportAllocs()
// to track allocation regressions.
// ──────────────────────────────────────────────────────────────────────────────

// BenchmarkCompileMinimal measures compilation of the smallest valid module.
func BenchmarkCompileMinimal(b *testing.B) {
	source := []byte(testutil.MinimalModule)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(source); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompileMedium measures compilation of a medium-complexity module.
func BenchmarkCompileMedium(b *testing.B) {
	source := []byte(testutil.MediumModule)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(source); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompileWide_10 through _100 test how compilation scales with
// the number of context fields and rules.
func BenchmarkCompileWide_10(b *testing.B) {
	benchCompileWide(b, 10)
}

func BenchmarkCompileWide_50(b *testing.B) {
	benchCompileWide(b, 50)
}

func BenchmarkCompileWide_100(b *testing.B) {
	benchCompileWide(b, 100)
}

func benchCompileWide(b *testing.B, n int) {
	source := []byte(testutil.GenerateWideModule(n))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(source); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSignalMemory_1K sends 1000 signals to a simple counter module
// with an in-memory store.
func BenchmarkSignalMemory_1K(b *testing.B) {
	benchmarkSignalN(b, 1000, false)
}

func BenchmarkSignalMemory_10K(b *testing.B) {
	benchmarkSignalN(b, 10000, false)
}

// BenchmarkSignalPebble_1K sends 1000 signals with a Pebble store.
func BenchmarkSignalPebble_1K(b *testing.B) {
	benchmarkSignalN(b, 1000, true)
}

func benchmarkSignalN(b *testing.B, n int, usePebble bool) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		var opts []Option
		opts = append(opts, WithTraceLevel(TraceMinimal))
		if usePebble {
			store, err := OpenPebble(b.TempDir(), PebbleNoSync())
			if err != nil {
				b.Fatal(err)
			}
			opts = append(opts, WithStore(store))
			defer store.Close()
		}
		engine, err := New(module, opts...)
		if err != nil {
			b.Fatal(err)
		}
		ctx := context.Background()
		id := fmt.Sprintf("bench-scale-%d", iter)
		if err := engine.Start(ctx, id, nil); err != nil {
			b.Fatal(err)
		}
		for i := 0; i < n; i++ {
			if err := engine.Signal(ctx, id, "Ping", nil); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkNewEngineMinimal measures the cost of creating a new Engine.
func BenchmarkNewEngineMinimal(b *testing.B) {
	module, err := Compile([]byte(testutil.MinimalModule))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := New(module); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStartExecution measures Start() cost.
func BenchmarkStartExecution(b *testing.B) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		b.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("bench-start-%d", i)
		if err := engine.Start(ctx, id, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkQueryState measures Query("state") cost.
func BenchmarkQueryState(b *testing.B) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		b.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "bench-query", nil); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Query(ctx, "bench-query", "state"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPatchMinimal measures Patch cost on a simple module.
func BenchmarkPatchMinimal(b *testing.B) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		b.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "bench-patch", nil); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := engine.Patch(ctx, "bench-patch", Patch{"Counter.count": i}); err != nil {
			b.Fatal(err)
		}
	}
}
