package axiom

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Homiakus/axiom/internal/lang"
)

var stableRuntimeQueryFields = map[string]struct{}{
	"id":              {},
	"domain":          {},
	"status":          {},
	"version":         {},
	"createdAt":       {},
	"updatedAt":       {},
	"moduleHash":      {},
	"compilerVersion": {},
	"planVersion":     {},
}

// ValidateRuntimeQueryProjections rejects misspelled or unsupported runtime.*
// fields before a Plan is exposed to the runtime. The compiler already limits
// runtime.* references to query scope; this validation narrows that namespace
// to the stable execution metadata contract.
func ValidateRuntimeQueryProjections(module *Module) error {
	if module == nil {
		return nil
	}

	var diagnostics Errors
	seen := map[string]struct{}{}
	for _, query := range module.Queries {
		for _, binding := range query.Return {
			for _, ref := range lang.ExprRefs(binding.Expr) {
				if !strings.HasPrefix(ref, "runtime.") {
					continue
				}
				field := strings.TrimPrefix(ref, "runtime.")
				if _, ok := stableRuntimeQueryFields[field]; ok {
					continue
				}
				key := query.Name + "\x00" + ref
				if _, duplicate := seen[key]; duplicate {
					continue
				}
				seen[key] = struct{}{}
				diagnostics = append(diagnostics, Error{
					Code:    "AX001",
					Kind:    "compile",
					Entity:  query.Name,
					Line:    binding.Line,
					Message: fmt.Sprintf("unresolved runtime query projection: %s", ref),
					Hint:    "Use one of the documented runtime metadata fields: " + strings.Join(RuntimeQueryProjectionNames(), ", ") + ".",
				})
			}
		}
	}
	if len(diagnostics) == 0 {
		return nil
	}
	sort.SliceStable(diagnostics, func(i, j int) bool {
		if diagnostics[i].Entity == diagnostics[j].Entity {
			if diagnostics[i].Line == diagnostics[j].Line {
				return diagnostics[i].Message < diagnostics[j].Message
			}
			return diagnostics[i].Line < diagnostics[j].Line
		}
		return diagnostics[i].Entity < diagnostics[j].Entity
	})
	return diagnostics
}

// RuntimeQueryProjectionNames returns the stable runtime.* query namespace.
func RuntimeQueryProjectionNames() []string {
	names := make([]string, 0, len(stableRuntimeQueryFields))
	for field := range stableRuntimeQueryFields {
		names = append(names, "runtime."+field)
	}
	sort.Strings(names)
	return names
}
