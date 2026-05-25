package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var namedConditionRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*(?:\([^)]*\))?$`)

type EvalResult struct{ Status, Why string }

type MockOutputs map[string]map[string]string

func parseMockOutputs(text string) (MockOutputs, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return MockOutputs{}, nil
	}
	var raw map[string]map[string]any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, err
	}
	out := MockOutputs{}
	for action, fields := range raw {
		out[action] = map[string]string{}
		for name, value := range fields {
			out[action][name] = fmt.Sprint(value)
		}
	}
	return out, nil
}

func parseAssumptions(text string) map[string]string {
	vals := map[string]string{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			vals[strings.TrimSpace(line[:idx])] = strings.Trim(strings.TrimSpace(line[idx+1:]), `"`)
		}
	}
	return vals
}

func explainCondition(cond string, vals map[string]string) EvalResult {
	c := strings.TrimSpace(cond)
	if c == "" {
		return EvalResult{"UNKNOWN", "пустое условие"}
	}
	if v, ok := vals[c]; ok {
		vl := strings.ToLower(v)
		if vl == "true" || vl == "yes" || vl == "1" || vl == "on" {
			return EvalResult{"PASS", c + " = true"}
		}
		if vl == "false" || vl == "no" || vl == "0" || vl == "off" {
			return EvalResult{"FAIL", c + " = false"}
		}
	}
	if mm := simpleCompareRe.FindStringSubmatch(c); len(mm) > 0 {
		field, op, expected := mm[1], mm[2], strings.Trim(mm[3], `"`)
		actual, ok := vals[field]
		if !ok {
			return EvalResult{"UNKNOWN", "нет значения для " + field}
		}
		okRes := false
		switch op {
		case "==":
			okRes = actual == expected
		case "!=":
			okRes = actual != expected
		case ">", "<", ">=", "<=":
			var a, e float64
			if _, err := fmt.Sscanf(actual, "%f", &a); err != nil {
				return EvalResult{"UNKNOWN", "не число: " + actual}
			}
			if _, err := fmt.Sscanf(expected, "%f", &e); err != nil {
				return EvalResult{"UNKNOWN", "не число: " + expected}
			}
			switch op {
			case ">":
				okRes = a > e
			case "<":
				okRes = a < e
			case ">=":
				okRes = a >= e
			case "<=":
				okRes = a <= e
			}
		}
		if okRes {
			return EvalResult{"PASS", fmt.Sprintf("%s = %s; ожидалось %s %s", field, actual, op, expected)}
		}
		return EvalResult{"FAIL", fmt.Sprintf("%s = %s; ожидалось %s %s", field, actual, op, expected)}
	}
	if boolFieldRe.MatchString(c) {
		actual, ok := vals[c]
		if !ok {
			return EvalResult{"UNKNOWN", "нет значения для " + c}
		}
		vl := strings.ToLower(actual)
		if vl == "true" || vl == "yes" || vl == "1" || vl == "on" {
			return EvalResult{"PASS", c + " = " + actual}
		}
		return EvalResult{"FAIL", c + " = " + actual}
	}
	if namedConditionRe.MatchString(c) {
		return EvalResult{"UNKNOWN", "именованное condition; раскройте его или задайте assumption"}
	}
	return EvalResult{"UNKNOWN", "сложное выражение; MVP не вычисляет его полностью"}
}

func selectedBlock(m ProjectModel, id string) (Block, bool) {
	if id == "" && len(m.Blocks) > 0 {
		return m.Blocks[0], true
	}
	for _, b := range m.Blocks {
		if b.ID == id {
			return b, true
		}
	}
	return Block{}, false
}

func selectedRule(m ProjectModel, id string) (RuleInfo, bool) {
	b, ok := selectedBlock(m, id)
	if !ok || b.Kind != "rule" {
		return RuleInfo{}, false
	}
	r, ok := m.Rules[b.Name]
	return r, ok
}

func ruleVerdict(r RuleInfo, assumptions string) string {
	vals := parseAssumptions(assumptions)
	hasFail, hasUnknown := false, false
	for _, c := range r.WhenLines {
		er := explainCondition(c, vals)
		if er.Status == "FAIL" {
			hasFail = true
		}
		if er.Status == "UNKNOWN" {
			hasUnknown = true
		}
	}
	if hasFail {
		return "BLOCKED"
	}
	if hasUnknown {
		return "UNKNOWN"
	}
	return "RUNNABLE"
}

func simulateEvent(m ProjectModel, eventName, assumptions string) []SimLine {
	out := []SimLine{}
	vals := parseAssumptions(assumptions)
	for _, r := range m.Rules {
		if r.OnEvent != "" && !strings.Contains(r.OnEvent, eventName) {
			continue
		}
		if r.OnEvent == "" && !strings.Contains(r.Block.Source(), eventName) {
			continue
		}
		verdict, checks := evalRuleDetailed(m, r, vals)
		why := []string{}
		for _, er := range checks {
			why = append(why, fmt.Sprintf("%s: %s (%s)", er.Status, er.Condition, er.Why))
		}
		if len(why) == 0 {
			why = append(why, "no explicit conditions")
		}
		out = append(out, SimLine{Rule: r.Block.Name, Verdict: verdict, Why: strings.Join(why, "\n")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rule < out[j].Rule })
	return out
}

func buildActions(m ProjectModel) map[string]ActionInfo {
	actions := map[string]ActionInfo{}
	for name, b := range m.Functions {
		ai := actions[name]
		ai.Name = name
		ai.Declared = true
		ai.Block = b
		parseFunctionBlock(&ai, b)
		actions[name] = ai
	}
	ruleNames := []string{}
	for n := range m.Rules {
		ruleNames = append(ruleNames, n)
	}
	sort.Strings(ruleNames)
	for _, rn := range ruleNames {
		r := m.Rules[rn]
		for _, call := range extractActionCalls(r.DoLines) {
			ai := actions[call.Name]
			ai.Name = call.Name
			ai.CalledBy = appendUnique(ai.CalledBy, rn)
			ai.CallForms = appendUnique(ai.CallForms, call.Form)
			for _, a := range call.Args {
				ai.Inputs = appendUnique(ai.Inputs, a)
			}
			for _, o := range extractResultFields(r.ThenLines) {
				ai.Outputs = appendUnique(ai.Outputs, o)
			}
			for _, w := range r.Writes {
				ai.Writes = appendUnique(ai.Writes, w)
			}
			if strings.Contains(strings.ToLower(strings.Join(r.WhenLines, "\n")), "estop") || strings.Contains(strings.ToLower(strings.Join(r.WhenLines, "\n")), "safety") {
				ai.SafetyHints = appendUnique(ai.SafetyHints, "blocked by safety/estop condition")
			}
			actions[call.Name] = ai
		}
	}
	return actions
}

type ActionCall struct {
	Name string
	Args []string
	Form string
}

func extractActionCalls(lines []string) []ActionCall {
	out := []ActionCall{}
	for _, line := range lines {
		for _, mm := range actionCallRe.FindAllStringSubmatch(line, -1) {
			if len(mm) < 3 {
				continue
			}
			name := mm[1]
			if name == "if" || name == "for" || name == "when" {
				continue
			}
			args := splitArgs(mm[2])
			out = append(out, ActionCall{Name: name, Args: args, Form: strings.TrimSpace(mm[0])})
		}
	}
	return out
}

func splitArgs(s string) []string {
	parts := []string{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func extractResultFields(lines []string) []string {
	seen := map[string]bool{}
	for _, line := range lines {
		for _, mm := range resultFieldRe.FindAllStringSubmatch(line, -1) {
			if len(mm) > 1 {
				seen[mm[1]] = true
			}
		}
	}
	out := []string{}
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func appendUnique(xs []string, v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return xs
	}
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}

func parseFunctionBlock(ai *ActionInfo, b Block) {
	section := ""
	for _, raw := range b.Body {
		s := strings.TrimSpace(raw)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if strings.HasPrefix(s, "input") {
			section = "input"
			continue
		}
		if strings.HasPrefix(s, "output") {
			section = "output"
			continue
		}
		if strings.HasPrefix(s, "effect") {
			ai.SafetyHints = appendUnique(ai.SafetyHints, s)
			continue
		}
		if section == "input" {
			ai.Inputs = appendUnique(ai.Inputs, s)
		}
		if section == "output" {
			ai.Outputs = appendUnique(ai.Outputs, strings.TrimSuffix(s, ":"))
		}
	}
}

func selectedAction(m ProjectModel, name string) (ActionInfo, bool) {
	if name == "" {
		return ActionInfo{}, false
	}
	if ai, ok := m.Actions[name]; ok {
		return ai, true
	}
	return ActionInfo{}, false
}

func conditionLinesOf(b Block) []string {
	out := []string{}
	for _, raw := range b.Body {
		s := strings.TrimSpace(raw)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		out = append(out, s)
	}
	return out
}

func evalRuleDetailed(m ProjectModel, r RuleInfo, vals map[string]string) (string, []CondEval) {
	checks := []CondEval{}
	hasFail, hasUnknown := false, false
	for _, c := range r.WhenLines {
		checks = append(checks, evalConditionExpanded(m, c, vals, map[string]bool{})...)
	}
	for _, ce := range checks {
		if ce.Status == "FAIL" {
			hasFail = true
		}
		if ce.Status == "UNKNOWN" {
			hasUnknown = true
		}
	}
	if hasFail {
		return "BLOCKED", checks
	}
	if hasUnknown {
		return "UNKNOWN", checks
	}
	return "RUNNABLE", checks
}

func evalConditionExpanded(m ProjectModel, cond string, vals map[string]string, stack map[string]bool) []CondEval {
	c := strings.TrimSpace(cond)
	if c == "" {
		return nil
	}
	if strings.HasPrefix(c, "not ") {
		inner := strings.TrimSpace(strings.TrimPrefix(c, "not "))
		res := evalConditionExpanded(m, inner, vals, stack)
		for i := range res {
			if res[i].Status == "PASS" {
				res[i].Status = "FAIL"
				res[i].Why = "not failed: " + res[i].Why
			} else if res[i].Status == "FAIL" {
				res[i].Status = "PASS"
				res[i].Why = "not passed: " + res[i].Why
			}
			res[i].Condition = c
		}
		return res
	}
	if b, ok := m.Conditions[c]; ok && !stack[c] {
		stack[c] = true
		out := []CondEval{{Condition: c, Status: "EXPAND", Why: "named condition"}}
		for _, sub := range conditionLinesOf(b) {
			out = append(out, evalConditionExpanded(m, sub, vals, stack)...)
		}
		return out
	}
	// Split very simple inline AND chains.
	if strings.Contains(c, " and ") {
		out := []CondEval{}
		for _, part := range strings.Split(c, " and ") {
			out = append(out, evalConditionExpanded(m, part, vals, stack)...)
		}
		return out
	}
	er := explainCondition(c, vals)
	return []CondEval{{Condition: c, Status: er.Status, Why: er.Why}}
}

func ruleMatchesInitialEvent(r RuleInfo, eventName string) bool {
	if eventName == "" {
		return false
	}
	if r.OnEvent != "" && strings.Contains(r.OnEvent, eventName) {
		return true
	}
	if strings.Contains(r.Block.Header, " on "+eventName) {
		return true
	}
	return false
}

func ruleDependsOnChanged(r RuleInfo, changed []string) bool {
	if len(changed) == 0 {
		return false
	}
	text := strings.Join(append(r.WhenLines, r.Reads...), "\n")
	for _, c := range changed {
		if c != "" && strings.Contains(text, c) {
			return true
		}
	}
	return false
}

func applyRuleWrites(r RuleInfo, vals map[string]string) []SimWrite {
	return applyRuleWritesWithMocks(r, vals, MockOutputs{})
}

func applyRuleWritesWithMocks(r RuleInfo, vals map[string]string, mocks MockOutputs) []SimWrite {
	writes := []SimWrite{}
	actionName := ""
	if calls := extractActionCalls(r.DoLines); len(calls) > 0 {
		actionName = calls[0].Name
	}
	for _, line := range r.ThenLines {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "increment ") {
			target := strings.TrimSpace(strings.TrimPrefix(s, "increment "))
			old := vals[target]
			var n int
			if _, err := fmt.Sscanf(old, "%d", &n); err == nil {
				n++
				vals[target] = fmt.Sprintf("%d", n)
			} else {
				vals[target] = "<incremented>"
			}
			writes = append(writes, SimWrite{Target: target, Value: vals[target]})
			continue
		}
		if mm := setRe.FindStringSubmatch(s); len(mm) > 1 {
			target := strings.TrimSpace(mm[1])
			parts := strings.SplitN(s, "=", 2)
			value := "<updated>"
			if len(parts) == 2 {
				value = normalizeSimValueWithMocks(strings.TrimSpace(parts[1]), actionName, mocks)
			}
			vals[target] = value
			writes = append(writes, SimWrite{Target: target, Value: value})
		}
	}
	return writes
}

func normalizeSimValue(expr string) string {
	return normalizeSimValueWithMocks(expr, "", MockOutputs{})
}

func normalizeSimValueWithMocks(expr, actionName string, mocks MockOutputs) string {
	expr = strings.TrimSpace(expr)
	expr = strings.Trim(expr, `"`)
	if expr == "true" || expr == "false" {
		return expr
	}
	if strings.HasPrefix(expr, "result.") {
		if actionName != "" {
			field := strings.TrimPrefix(expr, "result.")
			if values, ok := mocks[actionName]; ok {
				if value, ok := values[field]; ok {
					return value
				}
			}
		}
		return "<" + expr + ">"
	}
	if strings.HasPrefix(expr, "event.") {
		return "<" + expr + ">"
	}
	if expr == "now" {
		return "<now>"
	}
	return expr
}

func simulateSystem(m ProjectModel, eventName, assumptions string) SimulationReport {
	return simulateSystemWithMocks(m, eventName, assumptions, MockOutputs{})
}

func simulateSystemWithMocks(m ProjectModel, eventName, assumptions string, mocks MockOutputs) SimulationReport {
	vals := parseAssumptions(assumptions)
	report := SimulationReport{Event: eventName}
	executed := map[string]bool{}
	changed := []string{}
	ruleNames := []string{}
	for n := range m.Rules {
		ruleNames = append(ruleNames, n)
	}
	sort.Strings(ruleNames)
	idx := 1
	// Phase 1: event-triggered rules, including blocked/unknown for visibility.
	for _, rn := range ruleNames {
		r := m.Rules[rn]
		if !ruleMatchesInitialEvent(r, eventName) {
			continue
		}
		verdict, checks := evalRuleDetailed(m, r, vals)
		step := SimStep{Index: idx, Phase: "event", Rule: rn, Verdict: verdict, Conditions: checks}
		idx++
		if verdict == "RUNNABLE" {
			for _, c := range extractActionCalls(r.DoLines) {
				step.Actions = append(step.Actions, c.Form)
			}
			step.Writes = applyRuleWritesWithMocks(r, vals, mocks)
			for _, w := range step.Writes {
				changed = appendUnique(changed, w.Target)
			}
			executed[rn] = true
		}
		report.Steps = append(report.Steps, step)
	}
	// Phase 2: propagation by changed fields, only actually runnable rules.
	for pass := 1; pass <= 5; pass++ {
		if len(changed) == 0 {
			break
		}
		lastChanged := changed
		changed = []string{}
		for _, rn := range ruleNames {
			if executed[rn] {
				continue
			}
			r := m.Rules[rn]
			if r.OnEvent != "" {
				continue
			}
			if !ruleDependsOnChanged(r, lastChanged) {
				continue
			}
			verdict, checks := evalRuleDetailed(m, r, vals)
			if verdict != "RUNNABLE" {
				continue
			}
			step := SimStep{Index: idx, Phase: fmt.Sprintf("propagation %d", pass), Rule: rn, Verdict: verdict, Conditions: checks}
			idx++
			for _, c := range extractActionCalls(r.DoLines) {
				step.Actions = append(step.Actions, c.Form)
			}
			step.Writes = applyRuleWritesWithMocks(r, vals, mocks)
			for _, w := range step.Writes {
				changed = appendUnique(changed, w.Target)
			}
			executed[rn] = true
			report.Steps = append(report.Steps, step)
		}
	}
	keys := []string{}
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		report.FinalState = append(report.FinalState, SimKV{k, vals[k]})
	}
	return report
}
