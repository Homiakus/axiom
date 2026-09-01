package runtime

import (
	"fmt"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/compiler"
)

func compileRetryDelayCharacterizationModule(t *testing.T, backoff string) *compiler.Module {
	t.Helper()
	source := []byte(fmt.Sprintf(`domain RetryDelayCharacterization

signal Run

context State:
  done: Bool = false

policy resilient:
  retry: 3
  backoff: %s
  timeout: 1s
  concurrency: parallel
  idempotency: optional

activity Work:
  output:
    ok: Bool
  effect: local
  policy: resilient

rule execute:
  on Run
  run: Work
  write:
    State.done = output.ok
`, backoff))
	module, err := compiler.Compile(source)
	if err != nil {
		t.Fatalf("compiler.Compile() error = %v", err)
	}
	return module
}

func TestRetryDelayCharacterizationDefaultExponentialAndCap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 100 * time.Millisecond},
		{attempt: 1, want: 100 * time.Millisecond},
		{attempt: 2, want: 200 * time.Millisecond},
		{attempt: 3, want: 400 * time.Millisecond},
		{attempt: 9, want: 25600 * time.Millisecond},
		{attempt: 10, want: 30 * time.Second},
		{attempt: 100, want: 30 * time.Second},
	}
	for _, tt := range tests {
		if got := retryDelay(nil, "Work", tt.attempt); got != tt.want {
			t.Fatalf("retryDelay(nil, Work, %d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestRetryDelayCharacterizationFixedDelayIsHardCapped(t *testing.T) {
	t.Parallel()

	module := compileRetryDelayCharacterizationModule(t, "fixed(45s)")
	for _, attempt := range []int{0, 1, 2, 100} {
		if got := retryDelay(module, "Work", attempt); got != 30*time.Second {
			t.Fatalf("retryDelay(fixed(45s), attempt=%d) = %v, want 30s", attempt, got)
		}
	}
}

func TestRetryDelayCharacterizationExponentialUsesExactDurationArithmetic(t *testing.T) {
	t.Parallel()

	module := compileRetryDelayCharacterizationModule(t, "exponential(3ns)")
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 3 * time.Nanosecond},
		{attempt: 1, want: 3 * time.Nanosecond},
		{attempt: 2, want: 6 * time.Nanosecond},
		{attempt: 3, want: 12 * time.Nanosecond},
		{attempt: 4, want: 24 * time.Nanosecond},
		{attempt: 5, want: 48 * time.Nanosecond},
	}
	for _, tt := range tests {
		if got := retryDelay(module, "Work", tt.attempt); got != tt.want {
			t.Fatalf("retryDelay(exponential(3ns), attempt=%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestRetryDelayCharacterizationExponentialCapsBeforeOverflow(t *testing.T) {
	t.Parallel()

	module := compileRetryDelayCharacterizationModule(t, "exponential(7s)")
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: 7 * time.Second},
		{attempt: 2, want: 14 * time.Second},
		{attempt: 3, want: 28 * time.Second},
		{attempt: 4, want: 30 * time.Second},
		{attempt: 1_000_000, want: 30 * time.Second},
	}
	for _, tt := range tests {
		if got := retryDelay(module, "Work", tt.attempt); got != tt.want {
			t.Fatalf("retryDelay(exponential(7s), attempt=%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}
