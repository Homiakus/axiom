package axiom

import (
	"os"
	"strings"
	"testing"
)

// TestDocsIntegrityAndLinkages enforces that all canonical architectural and
// operational documentation referenced in docs/README.md exists and is non-empty.
func TestDocsIntegrityAndLinkages(t *testing.T) {
	requiredDocs := []string{
		"docs/README.md",
		"docs/api-guide.md",
		"docs/deprecation-inventory.md",
		"docs/error-taxonomy.md",
		"docs/observability-and-health.md",
		"docs/operational-runbooks.md",
		"docs/runtime-semantics.md",
		"docs/flow-durability.md",
		"docs/durable-primitives-inventory.md",
		"docs/serialized-surfaces.md",
		"docs/clock-inventory.md",
		"docs/architecture-fmea.md",
		"docs/architecture-risk-register.json",
		"docs/versioning.md",
		"docs/QUALITY_LOOP.md",
		"docs/axiom-file-specification.md",
		"docs/axiomgen.md",
		"docs/go-first-architecture.md",
		"MASTER_PLAN.md",
		"ARCHITECTURE.md",
		"CONTRIBUTING.md",
		"DEVELOPMENT.md",
		"SECURITY.md",
	}

	for _, relPath := range requiredDocs {
		t.Run(relPath, func(t *testing.T) {
			info, err := os.Stat(relPath)
			if err != nil {
				t.Fatalf("required documentation file %s is missing: %v", relPath, err)
			}
			if info.Size() == 0 {
				t.Fatalf("required documentation file %s is empty", relPath)
			}

			content, err := os.ReadFile(relPath)
			if err != nil {
				t.Fatalf("failed to read %s: %v", relPath, err)
			}

			// Ensure UTF-8 clean text
			if strings.Contains(string(content), "\x00") {
				t.Fatalf("documentation file %s contains invalid null bytes", relPath)
			}
		})
	}
}