package axiom

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkWelcomeFlowMemoryAggregate(b *testing.B) {
	benchmarkWelcomeFlow(b, false, TraceAggregate)
}

func BenchmarkWelcomeFlowPebbleAggregate(b *testing.B) {
	benchmarkWelcomeFlow(b, true, TraceAggregate)
}

func BenchmarkWelcomeFlowPebbleAggregateWarm(b *testing.B) {
	benchmarkWelcomeFlowWarmPebble(b, TraceAggregate)
}

func BenchmarkWelcomeFlowPebbleNoSyncWarm(b *testing.B) {
	benchmarkWelcomeFlowWarmPebbleOpts(b, TraceAggregate, PebbleNoSync())
}

func BenchmarkWelcomeFlowPebbleJSONNoSyncWarm(b *testing.B) {
	benchmarkWelcomeFlowWarmPebbleOpts(b, TraceAggregate, PebbleNoSync(), PebbleJSONCodec())
}

func BenchmarkWelcomeFlowPebbleGroupSyncWarm(b *testing.B) {
	benchmarkWelcomeFlowWarmPebbleOpts(b, TraceAggregate, PebbleSyncEvery(10*time.Millisecond))
}

func BenchmarkWelcomeSignalPebbleAggregateWarm(b *testing.B) {
	benchmarkWelcomeSignalWarmPebble(b)
}

func BenchmarkWelcomeSignalPebbleNoSyncWarm(b *testing.B) {
	benchmarkWelcomeSignalWarmPebbleOpts(b, PebbleNoSync())
}

func BenchmarkWelcomeSignalPebbleJSONNoSyncWarm(b *testing.B) {
	benchmarkWelcomeSignalWarmPebbleOpts(b, PebbleNoSync(), PebbleJSONCodec())
}

func BenchmarkWelcomeSignalPebbleGroupSyncWarm(b *testing.B) {
	benchmarkWelcomeSignalWarmPebbleOpts(b, PebbleSyncEvery(10*time.Millisecond))
}

func BenchmarkWelcomeFlowPebbleFullTrace(b *testing.B) {
	benchmarkWelcomeFlow(b, true, TraceFull)
}

func benchmarkWelcomeFlow(b *testing.B, pebble bool, trace TraceLevel) {
	module, err := Compile([]byte(welcomeRuntimeSource))
	if err != nil {
		b.Fatalf("Compile() error = %v", err)
	}
	for i := 0; i < b.N; i++ {
		var opts []Option
		if pebble {
			store, err := OpenPebble(b.TempDir())
			if err != nil {
				b.Fatalf("OpenPebble() error = %v", err)
			}
			defer store.Close()
			opts = append(opts, WithStore(store))
		}
		opts = append(opts,
			WithTraceLevel(trace),
			WithActivity("SendWelcomeEmail", func(ctx context.Context, input Input) (Output, error) {
				return Output{"sent": true}, nil
			}),
		)
		engine, err := New(module, opts...)
		if err != nil {
			b.Fatalf("New() error = %v", err)
		}
		ctx := context.Background()
		executionID := fmt.Sprintf("bench-%d", i)
		if err := engine.Start(ctx, executionID, nil); err != nil {
			b.Fatalf("Start() error = %v", err)
		}
		if err := engine.Signal(ctx, executionID, "UserRegistered", Input{"userId": "u1", "email": "user@example.com"}); err != nil {
			b.Fatalf("Signal() error = %v", err)
		}
		if err := engine.RunUntilIdle(ctx, executionID); err != nil {
			b.Fatalf("RunUntilIdle() error = %v", err)
		}
	}
}

func benchmarkWelcomeFlowWarmPebble(b *testing.B, trace TraceLevel) {
	benchmarkWelcomeFlowWarmPebbleOpts(b, trace)
}

func benchmarkWelcomeFlowWarmPebbleOpts(b *testing.B, trace TraceLevel, pebbleOpts ...PebbleOption) {
	module, err := Compile([]byte(welcomeRuntimeSource))
	if err != nil {
		b.Fatalf("Compile() error = %v", err)
	}
	store, err := OpenPebble(b.TempDir(), pebbleOpts...)
	if err != nil {
		b.Fatalf("OpenPebble() error = %v", err)
	}
	defer store.Close()
	engine, err := New(module,
		WithStore(store),
		WithTraceLevel(trace),
		WithActivity("SendWelcomeEmail", func(ctx context.Context, input Input) (Output, error) {
			return Output{"sent": true}, nil
		}),
	)
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		executionID := fmt.Sprintf("bench-warm-%d", i)
		if err := engine.Start(ctx, executionID, nil); err != nil {
			b.Fatalf("Start() error = %v", err)
		}
		if err := engine.Signal(ctx, executionID, "UserRegistered", Input{"userId": "u1", "email": "user@example.com"}); err != nil {
			b.Fatalf("Signal() error = %v", err)
		}
		if err := engine.RunUntilIdle(ctx, executionID); err != nil {
			b.Fatalf("RunUntilIdle() error = %v", err)
		}
	}
}

func benchmarkWelcomeSignalWarmPebble(b *testing.B) {
	benchmarkWelcomeSignalWarmPebbleOpts(b)
}

func benchmarkWelcomeSignalWarmPebbleOpts(b *testing.B, pebbleOpts ...PebbleOption) {
	module, err := Compile([]byte(welcomeRuntimeSource))
	if err != nil {
		b.Fatalf("Compile() error = %v", err)
	}
	store, err := OpenPebble(b.TempDir(), pebbleOpts...)
	if err != nil {
		b.Fatalf("OpenPebble() error = %v", err)
	}
	defer store.Close()
	engine, err := New(module,
		WithStore(store),
		WithTraceLevel(TraceAggregate),
		WithActivity("SendWelcomeEmail", func(ctx context.Context, input Input) (Output, error) {
			return Output{"sent": true}, nil
		}),
	)
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "bench-signal", nil); err != nil {
		b.Fatalf("Start() error = %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := engine.Signal(ctx, "bench-signal", "UserRegistered", Input{"userId": fmt.Sprintf("u%d", i), "email": fmt.Sprintf("u%d@example.com", i)}); err != nil {
			b.Fatalf("Signal() error = %v", err)
		}
	}
}
