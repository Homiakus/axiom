package runtime

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Homiakus/axiom/internal/diag"
)

func TestIsRetryableActivityFailure_StrictClassification(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		msg       string
		retryable bool
	}{
		{
			name:      "exact AX505",
			msg:       "AX505",
			retryable: true,
		},
		{
			name:      "AX505 prefix with colon",
			msg:       "AX505: database connection timeout",
			retryable: true,
		},
		{
			name:      "wrapped AX505 in error chain",
			msg:       "step failed: AX505: network reset",
			retryable: true,
		},
		{
			name:      "diag.Error AX505",
			err:       diag.Error{Code: "AX505", Message: "timeout"},
			msg:       "AX505: timeout",
			retryable: true,
		},
		{
			name:      "substring false positive TAX5050",
			msg:       "invalid tax code TAX5050 in payload",
			retryable: false,
		},
		{
			name:      "substring false positive AX5050",
			msg:       "AX5050: different error code",
			retryable: false,
		},
		{
			name:      "substring false positive prefix inside word",
			msg:       "transaction FAX505 rejected",
			retryable: false,
		},
		{
			name:      "different diagnostic AX400",
			msg:       "AX400: bad input format",
			retryable: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isRetryableActivityFailure(tc.msg)
			if got != tc.retryable {
				t.Errorf("isRetryableActivityFailure(%q) = %v; want %v", tc.msg, got, tc.retryable)
			}
		})
	}
}

func TestShouldCommitTransactionError_Classification(t *testing.T) {
	// 1. RetryScheduledError should commit state
	retryErr := &RetryScheduledError{
		TaskID:       "t-1",
		ExecutionID:  "exec-1",
		ActivityName: "Process",
	}
	if !shouldCommitTransactionError(retryErr) {
		t.Errorf("expected RetryScheduledError to commit transaction state")
	}

	// 2. Wrapped RetryScheduledError should commit state
	wrappedRetry := fmt.Errorf("scheduling retry: %w", retryErr)
	if !shouldCommitTransactionError(wrappedRetry) {
		t.Errorf("expected wrapped RetryScheduledError to commit transaction state")
	}

	// 3. diag.Error with AX505 should commit state
	diagErr := &diag.Error{
		Code:    "AX505",
		Entity:  "Process",
		Message: "activity failed",
	}
	if !shouldCommitTransactionError(diagErr) {
		t.Errorf("expected diag.Error AX505 to commit transaction state")
	}

	// 4. diag.Error value with AX505 should commit state
	if !shouldCommitTransactionError(*diagErr) {
		t.Errorf("expected diag.Error value AX505 to commit transaction state")
	}

	// 5. Normal critical errors must NOT commit state (must rollback)
	normalErr := errors.New("database connection refused")
	if shouldCommitTransactionError(normalErr) {
		t.Errorf("expected database error NOT to commit transaction state")
	}

	// 6. Non-retryable diag.Error (e.g. AX400) must NOT commit state
	badInputErr := &diag.Error{
		Code:    "AX400",
		Entity:  "Process",
		Message: "invalid input",
	}
	if shouldCommitTransactionError(badInputErr) {
		t.Errorf("expected AX400 diag.Error NOT to commit transaction state")
	}
}
