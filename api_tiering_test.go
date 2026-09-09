package axiom

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type apiTieringDocument struct {
	Version     string                  `json:"version"`
	Updated     string                  `json:"updated"`
	Description string                  `json:"description"`
	Tiers       map[string]string       `json:"tiers"`
	TierCounts  map[string]int          `json:"tier_counts"`
	Symbols     []apiTieringSymbolEntry `json:"symbols"`
}

type apiTieringSymbolEntry struct {
	Package           string `json:"package"`
	Symbol            string `json:"symbol"`
	Kind              string `json:"kind"`
	Tier              string `json:"tier"`
	Rationale         string `json:"rationale"`
	RequiredByFeature string `json:"required_by_feature,omitempty"`
	Replacement       string `json:"replacement,omitempty"`
	TargetVersion     string `json:"target_version,omitempty"`
}

// TestAPITieringIntegrity enforces that every public exported symbol in the
// compatibility manifest is classified into one of the five canonical tiers,
// and that advanced/internalization symbols map to high-level features while
// deprecated symbols declare replacements.
func TestAPITieringIntegrity(t *testing.T) {
	manifestPath := filepath.Join("testdata", "compat", "public_api_manifest.txt")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read public API manifest: %v", err)
	}

	tieringPath := filepath.Join("docs", "api-tiering.json")
	tieringBytes, err := os.ReadFile(tieringPath)
	if err != nil {
		t.Fatalf("failed to read api-tiering.json: %v", err)
	}

	var doc apiTieringDocument
	if err := json.Unmarshal(tieringBytes, &doc); err != nil {
		t.Fatalf("failed to parse api-tiering.json: %v", err)
	}

	validTiers := map[string]bool{
		"stable_facade":             true,
		"advanced":                  true,
		"extension_spi":             true,
		"deprecated":                true,
		"internalization_candidate": true,
	}

	for tier := range validTiers {
		if _, ok := doc.Tiers[tier]; !ok {
			t.Errorf("missing definition for canonical tier %q", tier)
		}
	}

	classifiedMap := make(map[string]apiTieringSymbolEntry)
	for _, entry := range doc.Symbols {
		key := entry.Package + "::" + entry.Symbol
		if _, duplicate := classifiedMap[key]; duplicate {
			t.Errorf("duplicate classification for symbol %s in package %s", entry.Symbol, entry.Package)
		}
		classifiedMap[key] = entry

		if !validTiers[entry.Tier] {
			t.Errorf("invalid tier %q for symbol %s", entry.Tier, entry.Symbol)
		}
		if entry.Rationale == "" {
			t.Errorf("missing rationale for symbol %s", entry.Symbol)
		}

		if entry.Tier == "advanced" || entry.Tier == "internalization_candidate" {
			if entry.RequiredByFeature == "" {
				t.Errorf("symbol %s tiered as %s requires 'required_by_feature' mapping", entry.Symbol, entry.Tier)
			}
		}

		if entry.Tier == "deprecated" {
			if entry.Replacement == "" || entry.TargetVersion == "" {
				t.Errorf("deprecated symbol %s requires 'replacement' and 'target_version'", entry.Symbol)
			}
		}
	}

	// Read symbols from manifest
	manifestLines := strings.Split(normalizeAPILineEndings(string(manifestBytes)), "\n")
	currentPkg := ""
	manifestCount := 0

	for _, line := range manifestLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "=== Package:") {
			pkg := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "=== Package:"), "==="))
			currentPkg = pkg
			continue
		}
		if strings.HasPrefix(line, "package ") {
			continue
		}

		manifestCount++
		key := currentPkg + "::" + line
		if _, ok := classifiedMap[key]; !ok {
			t.Errorf("unclassified public symbol in package %s: %s (add to docs/api-tiering.json)", currentPkg, line)
		}
	}

	if manifestCount != len(doc.Symbols) {
		t.Errorf("symbol count mismatch: manifest has %d symbols, api-tiering.json has %d", manifestCount, len(doc.Symbols))
	}

	t.Logf("Verified %d public symbols across 5 tiers: stable_facade=%d, extension_spi=%d, advanced=%d, internalization_candidate=%d, deprecated=%d",
		manifestCount,
		doc.TierCounts["stable_facade"],
		doc.TierCounts["extension_spi"],
		doc.TierCounts["advanced"],
		doc.TierCounts["internalization_candidate"],
		doc.TierCounts["deprecated"])
}
