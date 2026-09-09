package axiom

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

type algorithmIntegrationMatrix struct {
	Version     string             `json:"version"`
	Updated     string             `json:"updated"`
	Description string             `json:"description"`
	Features    []algorithmFeature `json:"features"`
}

type algorithmFeature struct {
	FeatureID          string   `json:"feature_id"`
	Title              string   `json:"title"`
	Category           string   `json:"category"`
	Status             string   `json:"status"`
	Mode               string   `json:"mode"`
	Persistent         bool     `json:"persistent"`
	Reachability       string   `json:"reachability"`
	ImplementationRefs []string `json:"implementation_refs"`
	PublicAPIRefs      []string `json:"public_api_refs"`
	TestRefs           []string `json:"test_refs"`
	DocRefs            []string `json:"doc_refs"`
	ExampleRefs        []string `json:"example_refs"`
}

var featureIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

var requiredDeliverableFeatures = []string{
	"compiler_graph_validation",
	"retry_backoff",
	"leases_fencing",
	"timers",
	"outbox",
	"compensation",
	"repair",
	"convergence_oscillation",
	"adaptive_routing",
	"provider_health",
	"admission_rate_limiting",
	"cache_single_flight",
	"hedging",
	"ensemble",
	"budgets",
	"human_approval",
	"awaitables",
	"signals",
	"migration_fork_time_travel",
	"continue_as_new",
	"child_workflows",
	"schedules",
	"retention",
	"observability",
}

func TestAlgorithmIntegrationMatrixIntegrity(t *testing.T) {
	const matrixPath = "docs/algorithm-integration-matrix.json"
	raw, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatalf("failed to read algorithm integration matrix: %v", err)
	}

	var matrix algorithmIntegrationMatrix
	if err := json.Unmarshal(raw, &matrix); err != nil {
		t.Fatalf("parse algorithm integration matrix: %v", err)
	}

	if strings.TrimSpace(matrix.Version) == "" || strings.TrimSpace(matrix.Updated) == "" {
		t.Fatal("matrix must declare non-empty version and updated date")
	}
	if len(matrix.Features) == 0 {
		t.Fatal("matrix must contain features")
	}

	seenIDs := make(map[string]bool)
	for idx, feat := range matrix.Features {
		if !featureIDPattern.MatchString(feat.FeatureID) {
			t.Errorf("feature [%d] has invalid feature_id %q", idx, feat.FeatureID)
		}
		if seenIDs[feat.FeatureID] {
			t.Errorf("duplicate feature_id %q", feat.FeatureID)
		}
		seenIDs[feat.FeatureID] = true

		if strings.TrimSpace(feat.Title) == "" {
			t.Errorf("feature %s has empty title", feat.FeatureID)
		}
		if strings.TrimSpace(feat.Category) == "" {
			t.Errorf("feature %s has empty category", feat.FeatureID)
		}

		switch feat.Status {
		case "implemented", "public", "wired", "persistent", "verified", "documented", "exampled":
			// valid
		default:
			t.Errorf("feature %s has unknown status %q", feat.FeatureID, feat.Status)
		}

		if feat.Mode != "opt-in" && feat.Mode != "default" {
			t.Errorf("feature %s has unknown mode %q (must be opt-in or default)", feat.FeatureID, feat.Mode)
		}

		assertExistingFiles(t, feat.FeatureID, "implementation_refs", feat.ImplementationRefs)
		assertExistingFiles(t, feat.FeatureID, "public_api_refs", feat.PublicAPIRefs)
		assertExistingFiles(t, feat.FeatureID, "test_refs", feat.TestRefs)
		assertExistingFiles(t, feat.FeatureID, "doc_refs", feat.DocRefs)
		assertExistingFiles(t, feat.FeatureID, "example_refs", feat.ExampleRefs)
	}

	for _, requiredID := range requiredDeliverableFeatures {
		if !seenIDs[requiredID] {
			t.Errorf("missing required deliverable feature %q in algorithm integration matrix", requiredID)
		}
	}
}

func assertExistingFiles(t *testing.T, featureID, refField string, paths []string) {
	t.Helper()
	if len(paths) == 0 {
		t.Errorf("feature %s has empty %s", featureID, refField)
		return
	}
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			t.Errorf("feature %s %s has empty path", featureID, refField)
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("feature %s %s reference %q not found: %v", featureID, refField, p, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("feature %s %s reference %q is a directory, want file", featureID, refField, p)
		}
	}
}
