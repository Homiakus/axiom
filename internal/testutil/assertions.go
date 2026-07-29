package testutil

import (
	"runtime"
	"testing"
	"time"
)

// ── Allocation assertions ────────────────────────────────────────────────────

// AssertAllocsBelow runs fn 'runs' times via testing.AllocsPerRun and
// fails the test if the average exceeds maxAllocs.  The threshold
// should be set from measured baselines, not guesses.
//
// Example usage:
//
//	testutil.AssertAllocsBelow(t, 5.0, 100, func() {
//	    engine.Signal(ctx, id, "Ping", nil)
//	})
func AssertAllocsBelow(t *testing.T, maxAllocs float64, runs int, fn func()) {
	t.Helper()
	if runs <= 0 {
		runs = 100
	}
	allocs := testing.AllocsPerRun(runs, fn)
	if allocs > maxAllocs {
		t.Errorf("allocs/op = %.2f, want <= %.2f (measured over %d runs)", allocs, maxAllocs, runs)
	}
}

// ── Goroutine leak detection ─────────────────────────────────────────────────

// GoroutineLeakCheck takes a snapshot of the goroutine count, runs fn,
// waits briefly for background goroutines to settle, and fails if the
// count increased by more than allowed.
//
// The 'allowed' parameter accommodates runtime goroutines that may be
// legitimately created (e.g., GC finalizers, signal handlers).
// A value of 2 is usually safe.
//
// Usage:
//
//	testutil.GoroutineLeakCheck(t, 2, func() {
//	    // test code that should not leak goroutines
//	})
func GoroutineLeakCheck(t *testing.T, allowed int, fn func()) {
	t.Helper()

	// Force GC to finalize any pending goroutines from previous tests.
	runtime.GC()
	time.Sleep(10 * time.Millisecond)

	before := runtime.NumGoroutine()
	fn()

	// Give background goroutines a moment to exit.
	// We poll up to 500 ms with increasing back-off.
	var after int
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
		after = runtime.NumGoroutine()
		if after <= before+allowed {
			return // no leak detected
		}
	}
	after = runtime.NumGoroutine()
	if after > before+allowed {
		t.Errorf("goroutine leak: before=%d, after=%d, allowed delta=%d", before, after, allowed)
	}
}

// ── Environment capture ──────────────────────────────────────────────────────

// Environment captures metadata about the benchmark execution environment
// for inclusion in reports.  All fields are informational.
type Environment struct {
	GoVersion string
	OS        string
	Arch      string
	NumCPU    int
	GOMAXPROCS int
}

// CaptureEnvironment snapshots the current runtime environment.
func CaptureEnvironment() Environment {
	return Environment{
		GoVersion:  runtime.Version(),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		NumCPU:     runtime.NumCPU(),
		GOMAXPROCS: runtime.GOMAXPROCS(0),
	}
}
