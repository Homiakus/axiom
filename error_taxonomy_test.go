package axiom

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Homiakus/axiom/adgo"
	"github.com/Homiakus/axiom/internal/diag"
	"github.com/Homiakus/axiom/internal/runtime"
)

func TestErrorTaxonomySentinelWrappingContracts(t *testing.T) {
	tests := []struct {
		name     string
		sentinel error
		wrapFn   func(error) error
	}{
		{
			name:     "ErrRetryScheduled wrapped with fmt.Errorf",
			sentinel: ErrRetryScheduled,
			wrapFn:   func(e error) error { return fmt.Errorf("outer failure: %w", e) },
		},
		{
			name:     "ErrExternalActivityClaimStale wrapped",
			sentinel: ErrExternalActivityClaimStale,
			wrapFn:   func(e error) error { return fmt.Errorf("task lease failure: %w", e) },
		},
		{
			name:     "ErrExternalActivityWorkerRequired wrapped",
			sentinel: ErrExternalActivityWorkerRequired,
			wrapFn:   func(e error) error { return fmt.Errorf("configuration error: %w", e) },
		},
		{
			name:     "adgo.ErrConflict wrapped",
			sentinel: adgo.ErrConflict,
			wrapFn:   func(e error) error { return fmt.Errorf("CAS mutation conflict: %w", e) },
		},
		{
			name:     "adgo.ErrStaleTask wrapped",
			sentinel: adgo.ErrStaleTask,
			wrapFn:   func(e error) error { return fmt.Errorf("worker attempt stale: %w", e) },
		},
		{
			name:     "adgo.ErrAdmissionDenied wrapped",
			sentinel: adgo.ErrAdmissionDenied,
			wrapFn:   func(e error) error { return fmt.Errorf("rate limit reached: %w", e) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := tt.wrapFn(tt.sentinel)
			if !errors.Is(wrapped, tt.sentinel) {
				t.Fatalf("errors.Is(wrapped, sentinel) = false for %s", tt.name)
			}
		})
	}
}

func TestErrorTaxonomyDiagnosticCodeExtraction(t *testing.T) {
	orig := diag.Error{
		Code:    "AX505",
		Message: "transient network connection reset",
		Line:    42,
	}
	wrapped := fmt.Errorf("activity execution failed: %w", orig)

	var extracted diag.Error
	if !errors.As(wrapped, &extracted) {
		t.Fatalf("errors.As(wrapped, &diag.Error) failed to extract original diagnostic")
	}
	if extracted.Code != "AX505" || extracted.Line != 42 {
		t.Fatalf("extracted diagnostic corrupted: %+v", extracted)
	}
}

func TestErrorTaxonomyADGOFailureClass(t *testing.T) {
	baseErr := errors.New("underlying socket closed")
	failErr := adgo.Fail(adgo.FailureTransient, baseErr)

	var typed *adgo.FailureError
	if !errors.As(failErr, &typed) {
		t.Fatalf("errors.As(failErr, &adgo.FailureError) failed")
	}
	if typed.Class != adgo.FailureTransient {
		t.Fatalf("typed.Class = %v, want FailureTransient", typed.Class)
	}
	if !errors.Is(failErr, baseErr) {
		t.Fatalf("errors.Is(failErr, baseErr) failed to unwrap underlying error")
	}
}

func TestErrorTaxonomyDurableStateErrorContract(t *testing.T) {
	// Durable control flow errors must commit transaction state
	var retryErr error = &runtime.RetryScheduledError{ExecutionID: "exec-1", TaskID: "task-1"}
	if dse, ok := retryErr.(runtime.DurableStateError); !ok || !dse.ShouldCommitState() {
		t.Fatalf("RetryScheduledError must satisfy DurableStateError with ShouldCommitState() == true")
	}

	// Unexpected standard error must NOT commit state (triggers rollback)
	standardErr := errors.New("database disk full")
	if _, ok := standardErr.(runtime.DurableStateError); ok {
		t.Fatalf("standard error must not implement DurableStateError")
	}
}
