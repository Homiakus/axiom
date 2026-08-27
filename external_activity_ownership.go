package axiom

import (
	"context"
	"fmt"
)

// NewWithExternalActivities builds an Engine using the normal New safeguards,
// while declaring selected AXM `effect: external` activities as owned by the
// fenced external-worker API instead of the inline Go activity runner.
//
// External ownership is installed before the Engine is returned to the caller,
// so RunUntilIdle can never lease one of these tasks. Local activities may still
// be registered through ordinary Act/Acts options in the same engine.
func NewWithExternalActivities(module *Module, externalNames []string, opts ...Option) (*Engine, error) {
	if module == nil {
		return nil, Errors{{Code: "AX500", Kind: "config", Message: "module is required", Hint: "Pass the module returned by Load or Compile."}}
	}

	external := make(map[string]struct{}, len(externalNames))
	placeholderOptions := make([]Option, 0, len(externalNames))
	for _, name := range externalNames {
		if name == "" {
			return nil, Errors{{Code: "AX509", Kind: "config", Message: "external activity name must not be empty"}}
		}
		if _, duplicate := external[name]; duplicate {
			return nil, Errors{{Code: "AX509", Kind: "config", Entity: name, Message: fmt.Sprintf("external activity %s is declared more than once", name)}}
		}
		activity, ok := module.Activities[name]
		if !ok {
			return nil, Errors{{Code: "AX502", Kind: "config", Entity: name, Message: fmt.Sprintf("external activity %s is not declared in .axm", name), Hint: "Declare the activity block before assigning external worker ownership."}}
		}
		if activity.Effect != "external" {
			return nil, Errors{{Code: "AX509", Kind: "config", Entity: name, Line: activity.Line, Message: fmt.Sprintf("activity %s has effect %q, not external", name, activity.Effect), Hint: "Only `effect: external` activities may be assigned to the fenced external worker."}}
		}
		external[name] = struct{}{}
		placeholderOptions = append(placeholderOptions, Act(name, externalActivityPlaceholder))
	}

	// External ownership is authoritative. Append placeholders after caller
	// options so an accidental local Act registration for the same name cannot
	// make an externally-owned effect executable by RunUntilIdle.
	combined := make([]Option, 0, len(opts)+len(placeholderOptions))
	combined = append(combined, opts...)
	combined = append(combined, placeholderOptions...)
	engine, err := New(module, combined...)
	if err != nil {
		return nil, err
	}
	engine.SetExternalActivities(external)
	return engine, nil
}

func externalActivityPlaceholder(context.Context, Input) (Output, error) {
	// Store-level ownership fencing rejects inline polling before this function
	// can be reached. Keep a fail-closed sentinel for defense in depth.
	return nil, ErrExternalActivityWorkerRequired
}
