package durabletime

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArchitectureAntiDriftLeafDependencies mechanically verifies that shared
// durable primitive packages (such as durabletime and durableserial) remain
// pure leaf packages with no outward dependencies on runtime orchestrators.
func TestArchitectureAntiDriftLeafDependencies(t *testing.T) {
	root := findRepoRoot(t)

	type leafRule struct {
		relDir       string
		allowModules []string
		disallowed   []string
	}

	rules := []leafRule{
		{
			relDir: filepath.Join("internal", "durabletime"),
			disallowed: []string{
				"github.com/Homiakus/axiom/adgo",
				"github.com/Homiakus/axiom/internal/runtime",
				"github.com/Homiakus/axiom/internal/store",
				"github.com/Homiakus/axiom/model",
				"github.com/Homiakus/axiom",
			},
		},
		{
			relDir: filepath.Join("internal", "durableserial"),
			disallowed: []string{
				"github.com/Homiakus/axiom/adgo",
				"github.com/Homiakus/axiom/internal/runtime",
				"github.com/Homiakus/axiom/internal/store",
				"github.com/Homiakus/axiom/model",
			},
		},
	}

	fset := token.NewFileSet()

	for _, rule := range rules {
		dirPath := filepath.Join(root, rule.relDir)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			t.Fatalf("failed to read dir %s: %v", dirPath, err)
		}

		for _, entry := range entries {
			// Check production (non-test) source files
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}

			filePath := filepath.Join(dirPath, entry.Name())
			node, err := parser.ParseFile(fset, filePath, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("failed to parse imports for %s: %v", filePath, err)
			}

			for _, imp := range node.Imports {
				pathVal := strings.Trim(imp.Path.Value, `"`)
				for _, dis := range rule.disallowed {
					if pathVal == dis || strings.HasPrefix(pathVal, dis+"/") {
						t.Errorf("Architecture violation: leaf file %s imports disallowed package %q", filePath, pathVal)
					}
				}
			}
		}
	}
}

// TestArchitectureAntiDriftEngineSeparation verifies that Core runtime and ADGO
// orchestrator maintain strict boundary separation and do not cross-import each other.
func TestArchitectureAntiDriftEngineSeparation(t *testing.T) {
	root := findRepoRoot(t)

	fset := token.NewFileSet()

	checkNoImport := func(t *testing.T, relDir, forbiddenPrefix, reason string) {
		t.Helper()
		dirPath := filepath.Join(root, relDir)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			t.Fatalf("failed to read dir %s: %v", dirPath, err)
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}

			filePath := filepath.Join(dirPath, entry.Name())
			node, err := parser.ParseFile(fset, filePath, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("failed to parse imports for %s: %v", filePath, err)
			}

			for _, imp := range node.Imports {
				pathVal := strings.Trim(imp.Path.Value, `"`)
				if pathVal == forbiddenPrefix || strings.HasPrefix(pathVal, forbiddenPrefix+"/") {
					t.Errorf("Anti-drift violation in %s: %s (%q is forbidden)", filePath, reason, pathVal)
				}
			}
		}
	}

	t.Run("Core runtime does not import ADGO", func(t *testing.T) {
		checkNoImport(t, filepath.Join("internal", "runtime"), "github.com/Homiakus/axiom/adgo",
			"Core runtime must remain independent of ADGO distributed coordinator")
	})

	t.Run("ADGO does not import Core runtime", func(t *testing.T) {
		checkNoImport(t, "adgo", "github.com/Homiakus/axiom/internal/runtime",
			"ADGO must remain an independent execution engine without importing internal/runtime")
	})
}
