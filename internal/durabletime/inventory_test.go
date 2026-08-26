package durabletime

import (
	"testing"
)

func TestClockInventoryRegistryValid(t *testing.T) {
	if len(Registry) == 0 {
		t.Fatal("expected non-empty ClockUsage Registry")
	}

	validCategories := map[TimeUsageCategory]bool{
		CategoryDurableDecision:         true,
		CategoryLeaseFencing:            true,
		CategoryRetryScheduleDeadline:   true,
		CategoryPersistedEventTimestamp: true,
		CategoryObservabilityElapsed:    true,
		CategoryOSFreshnessBoundary:     true,
		CategoryTestOnlyWait:            true,
	}

	for i, entry := range Registry {
		if entry.Package == "" {
			t.Errorf("entry #%d missing Package", i)
		}
		if entry.File == "" {
			t.Errorf("entry #%d missing File", i)
		}
		if entry.Function == "" {
			t.Errorf("entry #%d missing Function", i)
		}
		if entry.CallType == "" {
			t.Errorf("entry #%d missing CallType", i)
		}
		if !validCategories[entry.Category] {
			t.Errorf("entry #%d (%s:%s) has invalid category %q", i, entry.File, entry.Function, entry.Category)
		}
		if entry.Rationale == "" {
			t.Errorf("entry #%d (%s:%s) missing Rationale", i, entry.File, entry.Function)
		}
	}
}

func TestClockInventoryDurableDecisionInjectionPolicy(t *testing.T) {
	// Rule: Any call categorized as CategoryDurableDecision MUST require Clock injection.
	for _, entry := range Registry {
		if entry.Category == CategoryDurableDecision && !entry.RequiresClockInjection {
			t.Errorf("entry %s:%s has category %q but RequiresClockInjection is false",
				entry.File, entry.Function, entry.Category)
		}
	}
}
