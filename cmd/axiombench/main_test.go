package main

import (
	"testing"
)

func TestCompareReportsRegressionDetection(t *testing.T) {
	baseline := report{
		Results: []scenarioResult{
			{
				Scenario:      "test_scenario",
				ThroughputOPS: 1000.0,
				Latency: latencySummary{
					P99US: 100.0,
				},
			},
		},
	}

	// Case 1: Healthy run (higher throughput, lower latency)
	currentFast := report{
		Results: []scenarioResult{
			{
				Scenario:      "test_scenario",
				ThroughputOPS: 1200.0,
				Latency: latencySummary{
					P99US: 80.0,
				},
			},
		},
	}
	rows := compareReports(baseline, currentFast, 25.0)
	if len(rows) != 1 || rows[0].Regression {
		t.Fatalf("expected healthy run without regression: %+v", rows)
	}

	// Case 2: Regressed run (throughput dropped 50%)
	currentSlow := report{
		Results: []scenarioResult{
			{
				Scenario:      "test_scenario",
				ThroughputOPS: 500.0,
				Latency: latencySummary{
					P99US: 200.0,
				},
			},
		},
	}
	rows = compareReports(baseline, currentSlow, 25.0)
	if len(rows) != 1 || !rows[0].Regression {
		t.Fatalf("expected regression detected: %+v", rows)
	}
}
