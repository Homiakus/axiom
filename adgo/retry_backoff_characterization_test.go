package adgo

import (
	"testing"
	"time"
)

func TestNormalizeRetryCharacterizationDefaultsAndJitterBounds(t *testing.T) {
	t.Parallel()

	if got, want := normalizeRetry(RetryPolicy{}), DefaultRetryPolicy(); got != want {
		t.Fatalf("normalizeRetry(zero) = %#v, want %#v", got, want)
	}

	partial := normalizeRetry(RetryPolicy{MaxAttempts: 2})
	if partial.MaxAttempts != 2 || partial.BaseDelay != time.Second || partial.MaxDelay != 30*time.Second || partial.MaxRetryDuration != 5*time.Minute || partial.JitterFraction != 0 {
		t.Fatalf("normalizeRetry(partial) = %#v", partial)
	}

	negativeJitter := normalizeRetry(RetryPolicy{
		MaxAttempts:      1,
		BaseDelay:        time.Nanosecond,
		MaxDelay:         time.Second,
		MaxRetryDuration: time.Minute,
		JitterFraction:   -0.25,
	})
	if negativeJitter.JitterFraction != 0 {
		t.Fatalf("negative jitter normalized to %v, want 0", negativeJitter.JitterFraction)
	}

	overfullJitter := normalizeRetry(RetryPolicy{
		MaxAttempts:      1,
		BaseDelay:        time.Nanosecond,
		MaxDelay:         time.Second,
		MaxRetryDuration: time.Minute,
		JitterFraction:   2,
	})
	if overfullJitter.JitterFraction != 1 {
		t.Fatalf("jitter > 1 normalized to %v, want 1", overfullJitter.JitterFraction)
	}
}

func TestBackoffCharacterizationAttemptsAndConfigurableCapWithoutJitter(t *testing.T) {
	t.Parallel()

	policy := RetryPolicy{
		BaseDelay:      3 * time.Nanosecond,
		MaxDelay:       20 * time.Nanosecond,
		JitterFraction: 0,
	}
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 3 * time.Nanosecond},
		{attempt: 1, want: 3 * time.Nanosecond},
		{attempt: 2, want: 6 * time.Nanosecond},
		{attempt: 3, want: 12 * time.Nanosecond},
		{attempt: 4, want: 20 * time.Nanosecond},
		{attempt: 1_000_000, want: 20 * time.Nanosecond},
	}
	for _, tt := range tests {
		if got := backoff(policy, tt.attempt, "exec/node"); got != tt.want {
			t.Fatalf("backoff(attempt=%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestBackoffCharacterizationDeterministicJitter(t *testing.T) {
	t.Parallel()

	policy := RetryPolicy{
		BaseDelay:      2 * time.Second,
		MaxDelay:       30 * time.Second,
		JitterFraction: 0.25,
	}
	const seed = "execution/node"
	got := backoff(policy, 3, seed)
	if repeat := backoff(policy, 3, seed); repeat != got {
		t.Fatalf("backoff with identical identity is not deterministic: first=%v repeat=%v", got, repeat)
	}

	nominal := 8 * time.Second
	lower := time.Duration(float64(nominal) * (1 - policy.JitterFraction))
	upper := time.Duration(float64(nominal) * (1 + policy.JitterFraction))
	if got < lower || got > upper {
		t.Fatalf("jittered backoff = %v, want within [%v, %v]", got, lower, upper)
	}
}

func TestBackoffCharacterizationFloatDurationPrecisionBoundary(t *testing.T) {
	t.Parallel()

	// time.Duration is an int64 nanosecond count, while ADGO's current backoff
	// arithmetic converts it to float64 before exponentiation. 2^53+1 is the
	// first positive integer that cannot be represented exactly by float64.
	base := time.Duration(1<<53) + 1
	policy := RetryPolicy{
		BaseDelay:      base,
		MaxDelay:       time.Duration(1 << 55),
		JitterFraction: 0,
	}

	if got, want := backoff(policy, 1, "precision"), base-time.Nanosecond; got != want {
		t.Fatalf("backoff at float precision boundary = %d ns, want %d ns", got, want)
	}
	if got, want := backoff(policy, 2, "precision"), 2*(base-time.Nanosecond); got != want {
		t.Fatalf("second backoff at float precision boundary = %d ns, want %d ns", got, want)
	}
}
