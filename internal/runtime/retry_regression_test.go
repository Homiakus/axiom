package runtime

import (
	"fmt"
	"math"
	"testing"
)

// CE-016: Wrapping a retryable AX505 error in fmt.Errorf must preserve its retryable classification.
func TestRetryClassificationWrapping(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "exact AX505 code",
			err:       fmt.Errorf("AX505"),
			retryable: true,
		},
		{
			name:      "AX505 with prefix",
			err:       fmt.Errorf("AX505: database timeout"),
			retryable: true,
		},
		{
			name:      "wrapped AX505 with error format %w",
			err:       fmt.Errorf("activity execution failed: %w", fmt.Errorf("AX505: connection reset")),
			retryable: true,
		},
		{
			name:      "terminal non-retryable error",
			err:       fmt.Errorf("AX400: invalid argument"),
			retryable: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isRetry := isRetryableActivityFailure(tc.err.Error())
			if isRetry != tc.retryable {
				t.Fatalf("expected isRetryableActivityFailure(%q) to be %v, got %v", tc.err.Error(), tc.retryable, isRetry)
			}
		})
	}
}

// TestFloatTypeCheckRejectsNonFinite tests that context Float field rejects NaN and +/- Inf.
func TestFloatTypeCheckRejectsNonFinite(t *testing.T) {
	if valueMatchesType(math.NaN(), "Float") {
		t.Fatalf("expected NaN to be rejected by Float type check")
	}
	if valueMatchesType(math.Inf(1), "Float") {
		t.Fatalf("expected +Inf to be rejected by Float type check")
	}
	if valueMatchesType(math.Inf(-1), "Float") {
		t.Fatalf("expected -Inf to be rejected by Float type check")
	}
	if !valueMatchesType(3.14, "Float") {
		t.Fatalf("expected finite float 3.14 to be accepted by Float type check")
	}
}
