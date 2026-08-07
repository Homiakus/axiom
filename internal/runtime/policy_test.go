package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/compiler"
)

func compilePolicyModule(t *testing.T, retry int, timeout, concurrency string) *compiler.Module {
	t.Helper()
	source := []byte(`domain Policies

signal Run

context State:
  done: Bool = false

policy workPolicy:
  retry: ` + itoa(retry) + `
  timeout: ` + timeout + `
  concurrency: ` + concurrency + `
  idempotency: optional

activity Work:
  output:
    ok: Bool
  effect: local
  policy: workPolicy

rule execute:
  on Run
  run: Work
  write:
    State.done = output.ok
`)
	module, err := compiler.Compile(source)
	if err != nil {
		t.Fatalf("compile policy module: %v", err)
	}
	return module
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

func TestActivityPolicyWrapperRunsOneHandlerAttemptPerLease(t *testing.T) {
	module := compilePolicyModule(t, 2, "1s", "parallel")
	var attempts atomic.Int32
	engine := NewEngine(module, nil, ActivityRegistry{
		"Work": func(context.Context, map[string]any) (map[string]any, error) {
			attempts.Add(1)
			return nil, errors.New("temporary")
		},
	})

	_, err := engine.activities["Work"](context.Background(), nil)
	if err == nil || err.Error() != "temporary" {
		t.Fatalf("activity error = %v, want temporary", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want exactly one handler attempt per task lease", attempts.Load())
	}
}

func TestActivityPolicyTimeoutIsEnforcedPerAttempt(t *testing.T) {
	module := compilePolicyModule(t, 0, "15ms", "parallel")
	engine := NewEngine(module, nil, ActivityRegistry{
		"Work": func(ctx context.Context, _ map[string]any) (map[string]any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	started := time.Now()
	_, err := engine.activities["Work"](context.Background(), nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}

func TestActivityPolicyOnceSerializesCalls(t *testing.T) {
	module := compilePolicyModule(t, 0, "1s", "once")
	var active atomic.Int32
	var maximum atomic.Int32
	engine := NewEngine(module, nil, ActivityRegistry{
		"Work": func(context.Context, map[string]any) (map[string]any, error) {
			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			active.Add(-1)
			return map[string]any{"ok": true}, nil
		},
	})

	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			if _, err := engine.activities["Work"](context.Background(), nil); err != nil {
				t.Errorf("activity returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	if maximum.Load() != 1 {
		t.Fatalf("maximum concurrent handlers = %d, want 1", maximum.Load())
	}
}

func TestActivityPolicyReturnsParentCancellationWithoutRetryLoop(t *testing.T) {
	module := compilePolicyModule(t, 5, "1s", "parallel")
	var attempts atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	engine := NewEngine(module, nil, ActivityRegistry{
		"Work": func(context.Context, map[string]any) (map[string]any, error) {
			attempts.Add(1)
			cancel()
			return nil, errors.New("temporary")
		},
	})

	_, err := engine.activities["Work"](ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1", attempts.Load())
	}
}
