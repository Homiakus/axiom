package axiom

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Homiakus/axiom/adgo"
)

// ProbeStatus represents the result of a health or readiness check.
type ProbeStatus struct {
	Healthy bool
	Reason  string
}

// EvaluateLiveness returns true if the engine or worker process is operational.
func EvaluateLiveness(isDeadlocked bool) ProbeStatus {
	if isDeadlocked {
		return ProbeStatus{Healthy: false, Reason: "deadlock detected"}
	}
	return ProbeStatus{Healthy: true, Reason: "ok"}
}

// EvaluateReadiness verifies all critical dependencies before accepting work.
func EvaluateReadiness(storeErr error, schemaCompatible bool, outboxBacklog int, maxBacklog int) ProbeStatus {
	if storeErr != nil {
		return ProbeStatus{Healthy: false, Reason: "store unavailable: " + storeErr.Error()}
	}
	if !schemaCompatible {
		return ProbeStatus{Healthy: false, Reason: "unsupported store schema"}
	}
	if outboxBacklog > maxBacklog {
		return ProbeStatus{Healthy: false, Reason: "outbox backlog threshold exceeded"}
	}
	return ProbeStatus{Healthy: true, Reason: "ok"}
}

func TestObservabilityMetricCardinalityRestrictions(t *testing.T) {
	// Permitted bounded labels
	validFailureClasses := []adgo.FailureClass{
		adgo.FailureTransient,
		adgo.FailureRateLimit,
		adgo.FailureInvalidInput,
		adgo.FailureQuality,
		adgo.FailurePermanent,
		adgo.FailureAmbiguousSideEffect,
	}

	for _, fc := range validFailureClasses {
		if len(fc) == 0 {
			t.Errorf("empty failure class encountered")
		}
	}
}

func TestObservabilityLivenessVsReadinessProbes(t *testing.T) {
	// Case 1: Healthy system -> Both Live and Ready
	live := EvaluateLiveness(false)
	ready := EvaluateReadiness(nil, true, 5, 100)
	if !live.Healthy || !ready.Healthy {
		t.Fatalf("expected healthy system to be live and ready")
	}

	// Case 2: Store failure -> Live is STILL TRUE, but Ready is FALSE
	storeErr := errors.New("pebble disk IO timeout")
	live = EvaluateLiveness(false)
	ready = EvaluateReadiness(storeErr, true, 5, 100)
	if !live.Healthy {
		t.Fatalf("liveness probe must NOT fail on external store error (prevents restart storm)")
	}
	if ready.Healthy {
		t.Fatalf("readiness probe MUST fail when store is unavailable")
	}

	// Case 3: Incompatible schema -> Ready is FALSE
	ready = EvaluateReadiness(nil, false, 5, 100)
	if ready.Healthy {
		t.Fatalf("readiness probe MUST fail on schema mismatch")
	}

	// Case 4: Severe outbox backlog -> Ready is FALSE
	ready = EvaluateReadiness(nil, true, 250, 100)
	if ready.Healthy {
		t.Fatalf("readiness probe MUST fail when outbox backlog exceeds ceiling")
	}

	// Case 5: Process deadlock -> Live is FALSE
	live = EvaluateLiveness(true)
	if live.Healthy {
		t.Fatalf("liveness probe MUST fail when deadlock is detected")
	}
}

func TestADGOMetricsAggregation(t *testing.T) {
	m := adgo.Metrics{
		WallTime:          500 * time.Millisecond,
		ActiveComputeTime: 200 * time.Millisecond,
		Activities:        5,
		Retries:           2,
		Repairs:           1,
		QualityGain:       10.0,
		Cost:              2.5,
	}

	if qpc := m.QualityGainPerCost(); qpc != 4.0 {
		t.Fatalf("QualityGainPerCost = %f, want 4.0", qpc)
	}

	if qpr := m.QualityGainPerRepair(); qpr != 10.0 {
		t.Fatalf("QualityGainPerRepair = %f, want 10.0", qpr)
	}
}

func TestFleetMetricsCollector(t *testing.T) {
	ctx := context.Background()
	store := adgo.NewMemoryStore()

	snap, err := adgo.CollectFleetMetrics(ctx, store)
	if err != nil {
		t.Fatalf("CollectFleetMetrics error: %v", err)
	}
	if snap.Executions != 0 {
		t.Fatalf("snap.Executions = %d, want 0", snap.Executions)
	}
}
