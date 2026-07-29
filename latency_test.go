package axiom

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/testutil"
)

// ──────────────────────────────────────────────────────────────────────────────
// Latency percentile tests (p50/p90/p95/p98/p99) for key engine operations.
// These tests produce a human-readable report and fail if p99 exceeds
// a generous upper bound (to catch severe regressions, not micro-optimise).
// ──────────────────────────────────────────────────────────────────────────────

func TestLatencySignalSimpleModule(t *testing.T) {
	if testing.Short() {
		t.Skip("latency test skipped in short mode")
	}

	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "latency-signal", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	const samples = 5000
	collector := testutil.NewLatencyCollector(samples)

	// Warm up — 100 signals before measuring.
	for i := 0; i < 100; i++ {
		_ = engine.Signal(ctx, "latency-signal", "Ping", nil)
	}

	for i := 0; i < samples; i++ {
		start := time.Now()
		if err := engine.Signal(ctx, "latency-signal", "Ping", nil); err != nil {
			t.Fatalf("Signal() error at sample %d: %v", i, err)
		}
		collector.Record(time.Since(start))
	}

	report := collector.Report()
	t.Logf("Signal(Simple) latency:\n%s", report.String())

	// Sanity check: p99 < 5ms on in-memory store.
	// This is a very generous bound — typical p99 is <200µs.
	if report.P99 > 5*time.Millisecond {
		t.Errorf("Signal p99 = %s, exceeds 5ms threshold", report.P99)
	}
}

func TestLatencyPatchSimpleModule(t *testing.T) {
	if testing.Short() {
		t.Skip("latency test skipped in short mode")
	}

	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "latency-patch", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	const samples = 5000
	collector := testutil.NewLatencyCollector(samples)

	// Warm up.
	for i := 0; i < 100; i++ {
		_ = engine.Patch(ctx, "latency-patch", Patch{"Counter.count": i})
	}

	for i := 0; i < samples; i++ {
		start := time.Now()
		if err := engine.Patch(ctx, "latency-patch", Patch{"Counter.count": i}); err != nil {
			t.Fatalf("Patch() error at sample %d: %v", i, err)
		}
		collector.Record(time.Since(start))
	}

	report := collector.Report()
	t.Logf("Patch(Simple) latency:\n%s", report.String())

	if report.P99 > 5*time.Millisecond {
		t.Errorf("Patch p99 = %s, exceeds 5ms threshold", report.P99)
	}
}

func TestLatencyCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("latency test skipped in short mode")
	}

	source := []byte(welcomeRuntimeSource)
	const samples = 1000
	collector := testutil.NewLatencyCollector(samples)

	// Warm up.
	for i := 0; i < 10; i++ {
		_, _ = Compile(source)
	}

	for i := 0; i < samples; i++ {
		start := time.Now()
		_, err := Compile(source)
		collector.Record(time.Since(start))
		if err != nil {
			t.Fatalf("Compile() error at sample %d: %v", i, err)
		}
	}

	report := collector.Report()
	t.Logf("Compile(WelcomeModule) latency:\n%s", report.String())
}

func TestLatencyRunUntilIdle(t *testing.T) {
	if testing.Short() {
		t.Skip("latency test skipped in short mode")
	}

	module, err := Compile([]byte(welcomeRuntimeSource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	const samples = 500
	collector := testutil.NewLatencyCollector(samples)

	for i := 0; i < samples; i++ {
		engine, err := New(module,
			WithTraceLevel(TraceMinimal),
			WithActivity("SendWelcomeEmail", func(ctx context.Context, input Input) (Output, error) {
				return Output{"sent": true}, nil
			}),
		)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		ctx := context.Background()
		id := fmt.Sprintf("latency-idle-%d", i)
		if err := engine.Start(ctx, id, nil); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if err := engine.Signal(ctx, id, "UserRegistered", Input{"userId": "u1", "email": "a@b.com"}); err != nil {
			t.Fatalf("Signal() error = %v", err)
		}

		start := time.Now()
		if err := engine.RunUntilIdle(ctx, id); err != nil {
			t.Fatalf("RunUntilIdle() error at sample %d: %v", i, err)
		}
		collector.Record(time.Since(start))
	}

	report := collector.Report()
	t.Logf("RunUntilIdle(Welcome) latency:\n%s", report.String())
}
