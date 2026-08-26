package durabletime

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod in parent directories")
		}
		dir = parent
	}
}

// TestArchitectureSemanticClockGuard enforces that any direct time.* invocation
// (time.Now, time.NewTimer, time.NewTicker, time.After, time.Since, time.Sleep)
// in restricted orchestration packages (e.g. adgo, internal/runtime) is explicitly
// registered in internal/durabletime.Registry with an architectural category and rationale.
func TestArchitectureSemanticClockGuard(t *testing.T) {
	root := findRepoRoot(t)

	// Build lookup map from Registry: key = "pkg/file:callType" or "pkg/file"
	type registryKey struct {
		pkg      string
		file     string
		callType string
	}
	registered := map[registryKey][]ClockUsageEntry{}
	for _, entry := range Registry {
		key := registryKey{
			pkg:      entry.Package,
			file:     entry.File,
			callType: entry.CallType,
		}
		registered[key] = append(registered[key], entry)
	}

	restrictedDirs := []struct {
		pkgName string
		relPath string
	}{
		{pkgName: "adgo", relPath: "adgo"},
		{pkgName: "runtime", relPath: filepath.Join("internal", "runtime")},
	}

	timeFunctions := map[string]bool{
		"Now":       true,
		"NewTimer":  true,
		"NewTicker": true,
		"After":     true,
		"Since":     true,
		"Sleep":     true,
	}

	fset := token.NewFileSet()
	var unlistedCalls []string

	for _, target := range restrictedDirs {
		dirPath := filepath.Join(root, target.relPath)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			t.Fatalf("failed to read dir %s: %v", dirPath, err)
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}

			filePath := filepath.Join(dirPath, entry.Name())
			node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("failed to parse %s: %v", filePath, err)
			}

			ast.Inspect(node, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}

				ident, ok := sel.X.(*ast.Ident)
				if !ok || ident.Name != "time" || !timeFunctions[sel.Sel.Name] {
					return true
				}

				callType := "time." + sel.Sel.Name
				pos := fset.Position(call.Pos())

				key := registryKey{
					pkg:      target.pkgName,
					file:     entry.Name(),
					callType: callType,
				}

				if entries, exists := registered[key]; exists && len(entries) > 0 {
					return true
				}

				unlistedCalls = append(unlistedCalls, fmt.Sprintf(
					"%s:%d: unallowlisted %s (package %s, file %s)",
					pos.Filename, pos.Line, callType, target.pkgName, entry.Name(),
				))
				return true
			})
		}
	}

	if len(unlistedCalls) > 0 {
		t.Errorf("Architecture guard detected %d unallowlisted time call(s) in restricted packages:\n%s\n\n"+
			"Policy: Direct time calls in orchestration paths must be evaluated. If this call is informational/telemetry/filesystem,"+
			" add an entry to internal/durabletime.Registry with rationale. If it influences durable decisions, migrate to Clock.",
			len(unlistedCalls), strings.Join(unlistedCalls, "\n"))
	}
}
