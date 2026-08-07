package model

import (
	"sort"
	"strings"
)

// Catch routes a terminal coded activity failure to a signal after the retry
// budget is exhausted. errorCode should match the code passed to
// axiom.FailActivity or returned by a custom ActivityErrorCoder.
func (p *PolicyBuilder) Catch(errorCode, signal string) *PolicyBuilder {
	if p == nil || p.definition == nil || p.index < 0 || p.index >= len(p.definition.policies) {
		return p
	}
	errorCode = strings.TrimSpace(errorCode)
	signal = strings.TrimSpace(signal)
	if errorCode == "" {
		p.definition.addBuilderDiagnostic("policy.catch", "catch error code must not be empty", "Use a stable domain error code or CatchAll for the wildcard fallback.")
		return p
	}
	if signal == "" {
		p.definition.addBuilderDiagnostic("policy.catch."+errorCode, "catch target signal must not be empty", "Pass the name of a declared event/signal.")
		return p
	}

	declaration := &p.definition.policies[p.index]
	mappings := parseRenderedCatch(declaration.entries["catch"].text)
	mappings[errorCode] = signal
	declaration.entries["catch"] = Expr{text: renderCatchExpression(mappings)}
	return p
}

// CatchAll configures the wildcard fallback for terminal activity failures that
// do not match an exact coded catch mapping.
func (p *PolicyBuilder) CatchAll(signal string) *PolicyBuilder {
	return p.Catch("*", signal)
}

func parseRenderedCatch(text string) map[string]string {
	mappings := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		from, to, ok := strings.Cut(line, "->")
		if !ok {
			continue
		}
		mappings[strings.TrimSpace(from)] = strings.TrimSpace(to)
	}
	return mappings
}

func renderCatchExpression(mappings map[string]string) string {
	keys := make([]string, 0, len(mappings))
	for key := range mappings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, "    "+key+" -> "+mappings[key])
	}
	return "\n" + strings.Join(lines, "\n")
}
