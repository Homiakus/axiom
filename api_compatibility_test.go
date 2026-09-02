package axiom

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// PublicPackages lists the root and public subpackages whose exported API
// is protected by backward-compatibility promises.
var PublicPackages = []string{
	".",
	"adgo",
	"diagram",
	"model",
	"store/pebble",
	"table",
}

func extractPackageAPI(pkgPath string) (string, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, pkgPath, func(fi os.FileInfo) bool {
		name := fi.Name()
		return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
	}, 0)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	var pkgNames []string
	for name := range pkgs {
		pkgNames = append(pkgNames, name)
	}
	sort.Strings(pkgNames)

	for _, name := range pkgNames {
		pkgAst := pkgs[name]
		pDoc := doc.New(pkgAst, pkgPath, doc.AllDecls)

		fmt.Fprintf(&buf, "package %s\n", pDoc.Name)

		// Constants
		for _, c := range pDoc.Consts {
			for _, spec := range c.Decl.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for _, name := range vs.Names {
						if name.IsExported() {
							fmt.Fprintf(&buf, "const %s\n", name.Name)
						}
					}
				}
			}
		}

		// Variables
		for _, v := range pDoc.Vars {
			for _, spec := range v.Decl.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for _, name := range vs.Names {
						if name.IsExported() {
							fmt.Fprintf(&buf, "var %s\n", name.Name)
						}
					}
				}
			}
		}

		// Types
		sort.Slice(pDoc.Types, func(i, j int) bool {
			return pDoc.Types[i].Name < pDoc.Types[j].Name
		})

		for _, t := range pDoc.Types {
			if !ast.IsExported(t.Name) {
				continue
			}
			fmt.Fprintf(&buf, "type %s\n", t.Name)

			// Factory / package-level functions associated with type
			for _, f := range t.Funcs {
				if ast.IsExported(f.Name) {
					fmt.Fprintf(&buf, "func %s\n", f.Name)
				}
			}

			// Methods
			for _, m := range t.Methods {
				if ast.IsExported(m.Name) {
					fmt.Fprintf(&buf, "method (%s) %s\n", t.Name, m.Name)
				}
			}
		}

		// Top-level functions
		sort.Slice(pDoc.Funcs, func(i, j int) bool {
			return pDoc.Funcs[i].Name < pDoc.Funcs[j].Name
		})

		for _, f := range pDoc.Funcs {
			if ast.IsExported(f.Name) {
				fmt.Fprintf(&buf, "func %s\n", f.Name)
			}
		}
	}

	return buf.String(), nil
}

func normalizeAPILineEndings(api string) string {
	api = strings.ReplaceAll(api, "\r\n", "\n")
	return strings.ReplaceAll(api, "\r", "\n")
}

// computeAPIDiff returns removed and added symbols between expected and current API.
func computeAPIDiff(expectedAPI, currentAPI string) (removed, added []string) {
	expectedLines := strings.Split(normalizeAPILineEndings(expectedAPI), "\n")
	currentLines := strings.Split(normalizeAPILineEndings(currentAPI), "\n")

	expectedSet := make(map[string]bool)
	for _, l := range expectedLines {
		if l != "" {
			expectedSet[l] = true
		}
	}

	currentSet := make(map[string]bool)
	for _, l := range currentLines {
		if l != "" {
			currentSet[l] = true
		}
	}

	for l := range expectedSet {
		if !currentSet[l] {
			removed = append(removed, l)
		}
	}
	sort.Strings(removed)

	for l := range currentSet {
		if !expectedSet[l] {
			added = append(added, l)
		}
	}
	sort.Strings(added)

	return removed, added
}

// TestPublicAPICompatibilityGate enforces that exported public symbols across
// Axiom packages cannot be removed or broken without explicit deprecation.
func TestPublicAPICompatibilityGate(t *testing.T) {
	manifestPath := filepath.Join("testdata", "compat", "public_api_manifest.txt")

	var fullAPI bytes.Buffer
	for _, relPkg := range PublicPackages {
		api, err := extractPackageAPI(relPkg)
		if err != nil {
			t.Fatalf("failed to extract API for %s: %v", relPkg, err)
		}
		fmt.Fprintf(&fullAPI, "=== Package: %s ===\n", relPkg)
		fullAPI.WriteString(api)
		fullAPI.WriteString("\n")
	}

	currentAPI := normalizeAPILineEndings(fullAPI.String())

	if os.Getenv("UPDATE_API_MANIFEST") == "1" {
		if err := os.WriteFile(manifestPath, []byte(currentAPI), 0644); err != nil {
			t.Fatalf("failed to write updated API manifest: %v", err)
		}
		t.Logf("Updated public API manifest at %s", manifestPath)
		return
	}

	manifestBytes, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		// First generation of manifest
		if err := os.WriteFile(manifestPath, []byte(currentAPI), 0644); err != nil {
			t.Fatalf("failed to initialize API manifest: %v", err)
		}
		t.Logf("Initialized public API manifest at %s", manifestPath)
		return
	}
	if err != nil {
		t.Fatalf("failed to read API manifest: %v", err)
	}

	expectedAPI := normalizeAPILineEndings(string(manifestBytes))

	if currentAPI != expectedAPI {
		removed, added := computeAPIDiff(expectedAPI, currentAPI)

		if len(removed) > 0 {
			t.Errorf("BREAKING API CHANGE: %d public symbols were removed or modified:\n  %s\n\n"+
				"Policy: Public exported APIs in v0.1.0+ cannot be removed without deprecation.",
				len(removed), strings.Join(removed, "\n  "))
		}

		if len(added) > 0 {
			t.Logf("NOTICE: %d public symbols were added. If intentional, run with UPDATE_API_MANIFEST=1 to accept.", len(added))
		}
	}
}

func TestAPICompatibilityDiffDetector(t *testing.T) {
	baseline := "package axiom\ntype Engine\nfunc NewEngine\nconst TraceFull\n"
	modified := "package axiom\ntype Engine\nfunc NewEngineV2\nconst TraceFull\n"

	removed, added := computeAPIDiff(baseline, modified)
	if len(removed) != 1 || removed[0] != "func NewEngine" {
		t.Fatalf("removed = %v, want ['func NewEngine']", removed)
	}
	if len(added) != 1 || added[0] != "func NewEngineV2" {
		t.Fatalf("added = %v, want ['func NewEngineV2']", added)
	}
}

func TestAPICompatibilityDiffDetectorNormalizesLineEndings(t *testing.T) {
	baseline := "package axiom\r\ntype Engine\r\nfunc NewEngine\r\n"
	current := "package axiom\ntype Engine\nfunc NewEngine\n"

	removed, added := computeAPIDiff(baseline, current)
	if len(removed) != 0 || len(added) != 0 {
		t.Fatalf("line-ending-only diff: removed=%v added=%v", removed, added)
	}
}
