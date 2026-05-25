package main

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"axiom/pkg/axiom"
)

var (
	topDeclRe       = regexp.MustCompile(`^(system|state|event|schedule|condition|profile|function|action|rule|always|view)\b\s*(.*)$`)
	commentTitleRe  = regexp.MustCompile(`^#\s*(.+?)\s*$`)
	eventHeaderRe   = regexp.MustCompile(`^event\s+([A-Za-z_][\w]*)(?:\(([^)]*)\))?`)
	stateHeaderRe   = regexp.MustCompile(`^state\s+([A-Za-z_][\w]*(?:\[[^]]+\])?)\s*:`)
	condHeaderRe    = regexp.MustCompile(`^condition\s+([A-Za-z_][\w]*)(?:\(([^)]*)\))?\s*:`)
	alwaysHeaderRe  = regexp.MustCompile(`^always\s+([A-Za-z_][\w]*)\s*:`)
	viewHeaderRe    = regexp.MustCompile(`^view\s+([A-Za-z_][\w]*)\s*:`)
	funcCallRe      = regexp.MustCompile(`\b([A-Z][A-Za-z0-9_]*)\s*\(`)
	actionCallRe    = regexp.MustCompile(`\b([A-Z][A-Za-z0-9_]*)\s*\(([^)]*)\)`)
	resultFieldRe   = regexp.MustCompile(`\bresult\.([A-Za-z_][\w]*)`)
	stateRefRe      = regexp.MustCompile(`\b([A-Z][A-Za-z0-9_]*(?:\[[^]]+\])?\.[A-Za-z_][\w]*)`)
	setRe           = regexp.MustCompile(`^\s*set\s+(.+?)\s*=`)
	simpleCompareRe = regexp.MustCompile(`^([A-Za-z_][\w]*(?:\[[^]]+\])?(?:\.[A-Za-z_][\w]*)+)\s*(==|!=|>=|<=|>|<)\s*(.+)$`)
	boolFieldRe     = regexp.MustCompile(`^([A-Za-z_][\w]*(?:\[[^]]+\])?(?:\.[A-Za-z_][\w]*)+)$`)
	actuatorRe      = regexp.MustCompile(`(?i)(Pump|Dose|Doser|Light|Siren|Valve|Actuator|Command|Relay)`)
	safetyRe        = regexp.MustCompile(`(?i)(Safety|estop|CanUseHardware|unsafe|locked|emergency)`)
)

func parseProject(source, path string) ProjectModel {
	m := ProjectModel{
		Path:       path,
		Source:     source,
		SystemName: "Unknown",
		Rules:      map[string]RuleInfo{},
		States:     map[string]Block{},
		Events:     map[string]Block{},
		Conditions: map[string]Block{},
		Always:     map[string]Block{},
		Views:      map[string]Block{},
		Functions:  map[string]Block{},
		Actions:    map[string]ActionInfo{},
	}
	lines := strings.Split(source, "\n")
	section := "General"
	pendingTitle := ""
	for i := 0; i < len(lines); {
		line := strings.TrimRight(lines[i], "\r")
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "#") {
			if mm := commentTitleRe.FindStringSubmatch(stripped); len(mm) > 1 {
				title := strings.Trim(mm[1], " -")
				if title != "" && !strings.HasPrefix(title, "TRIZ") && len(title) < 80 {
					pendingTitle = title
				}
			}
			i++
			continue
		}
		mm := topDeclRe.FindStringSubmatch(line)
		if len(mm) == 0 {
			i++
			continue
		}
		kind, rest := mm[1], strings.TrimSpace(mm[2])
		start := i
		header := line
		body := []string{}
		i++
		for i < len(lines) && !topDeclRe.MatchString(lines[i]) {
			body = append(body, strings.TrimRight(lines[i], "\r"))
			i++
		}
		if kind == "system" {
			fs := strings.Fields(rest)
			if len(fs) > 0 {
				m.SystemName = fs[0]
			}
			continue
		}
		name := extractName(kind, header)
		if pendingTitle != "" && (kind == "rule" || kind == "always" || kind == "view") {
			section = pendingTitle
			pendingTitle = ""
		}
		b := Block{ID: kind + ":" + name, Kind: kind, Name: name, Header: header, Body: body, StartLine: start + 1, EndLine: i, Section: section}
		m.Blocks = append(m.Blocks, b)
		switch kind {
		case "rule":
			m.Rules[name] = parseRule(b)
		case "state":
			m.States[name] = b
		case "event":
			m.Events[name] = b
		case "condition":
			m.Conditions[name] = b
		case "always":
			m.Always[name] = b
		case "view":
			m.Views[name] = b
		case "function", "action":
			m.Functions[name] = b
		}
	}
	seen := map[string]bool{}
	for _, r := range m.Rules {
		for _, f := range r.Functions {
			if _, ok := m.Functions[f]; !ok {
				seen[f] = true
			}
		}
	}
	for f := range seen {
		m.InferredFunctions = append(m.InferredFunctions, f)
	}
	sort.Strings(m.InferredFunctions)
	m.Actions = buildActions(m)
	m.Diagnostics = diagnose(m)
	applyCompilerModel(&m)
	m.Sections = groupSections(m.Blocks)
	m.Graph = buildProjectGraph(m, SimulationReport{})
	return m
}

func currentProject(clearMsg bool) (ProjectModel, string) {
	state.mu.Lock()
	source, path, msg := state.source, state.path, state.msg
	if clearMsg {
		state.msg = ""
	}
	if state.modelValid && state.modelSource == source && state.modelPath == path {
		m := state.model
		state.mu.Unlock()
		return m, msg
	}
	state.mu.Unlock()

	m := parseProject(source, path)

	state.mu.Lock()
	if state.source == source && state.path == path {
		state.model = m
		state.modelSource = source
		state.modelPath = path
		state.modelValid = true
	}
	state.mu.Unlock()
	return m, msg
}

func invalidateProjectLocked() {
	state.modelValid = false
}

func applyCompilerModel(m *ProjectModel) {
	if m == nil {
		return
	}
	source := []byte(m.Source)
	if looksLikeTRIZ(source) {
		m.Format = "TRIZ DSL"
		result, err := axiom.NormalizeTRIZ(source, axiom.WithSourceName(m.Path))
		if result != nil {
			m.NormalizedSource = string(result.NormalizedSource)
			m.CompilerDiagnostics = appendDiagnostics(m.CompilerDiagnostics, result.Diagnostics)
			if result.Module != nil {
				m.CompileOK = true
				applyModuleSummary(m, result.Module)
			}
		}
		if err != nil {
			m.Diagnostics = append(m.Diagnostics, "Compiler: "+err.Error())
			m.CompilerDiagnostics = appendDiagnostics(m.CompilerDiagnostics, err)
		}
		if result != nil {
			for _, d := range result.Diagnostics {
				m.Diagnostics = append(m.Diagnostics, diagnosticLine(d))
			}
		}
		return
	}
	m.Format = "Axiom v0"
	module, err := axiom.CompileAny(source, axiom.WithSourceName(m.Path))
	if err != nil {
		m.Diagnostics = append(m.Diagnostics, "Compiler: "+err.Error())
		m.CompilerDiagnostics = appendDiagnostics(m.CompilerDiagnostics, err)
		return
	}
	m.CompileOK = true
	m.NormalizedSource = m.Source
	applyModuleSummary(m, module)
}

func applyModuleSummary(m *ProjectModel, module *axiom.Module) {
	if module == nil {
		return
	}
	m.SystemName = module.Domain
	for name := range module.Signals {
		if _, ok := m.Events[name]; !ok {
			m.Events[name] = Block{ID: "signal:" + name, Kind: "signal", Name: name}
		}
	}
	for name := range module.Contexts {
		if _, ok := m.States[name]; !ok {
			m.States[name] = Block{ID: "context:" + name, Kind: "context", Name: name}
		}
	}
	for name := range module.Activities {
		if _, ok := m.Functions[name]; !ok {
			m.Functions[name] = Block{ID: "activity:" + name, Kind: "activity", Name: name}
		}
	}
}

func appendDiagnostics(out []CompilerDiagnostic, err error) []CompilerDiagnostic {
	if err == nil {
		return out
	}
	if ds, ok := err.(axiom.Diagnostics); ok {
		for _, d := range ds {
			out = append(out, compilerDiagnostic(d))
		}
		return out
	}
	var ds axiom.Diagnostics
	if errors.As(err, &ds) {
		for _, d := range ds {
			out = append(out, compilerDiagnostic(d))
		}
		return out
	}
	if d, ok := err.(axiom.Diagnostic); ok {
		out = append(out, compilerDiagnostic(d))
		return out
	}
	var d axiom.Diagnostic
	if errors.As(err, &d) {
		out = append(out, compilerDiagnostic(d))
		return out
	}
	out = append(out, CompilerDiagnostic{Message: err.Error()})
	return out
}

func compilerDiagnostic(d axiom.Diagnostic) CompilerDiagnostic {
	return CompilerDiagnostic{Code: d.Code, Kind: d.Kind, Entity: d.Entity, Line: d.Line, Message: d.Message, Hint: d.Hint}
}

func diagnosticLine(d axiom.Diagnostic) string {
	var parts []string
	if d.Code != "" {
		parts = append(parts, d.Code)
	}
	if d.Line > 0 {
		parts = append(parts, fmt.Sprintf("line %d", d.Line))
	}
	if d.Entity != "" {
		parts = append(parts, d.Entity)
	}
	if d.Message != "" {
		parts = append(parts, d.Message)
	}
	if d.Hint != "" {
		parts = append(parts, "hint: "+d.Hint)
	}
	return strings.Join(parts, ": ")
}

func looksLikeTRIZ(source []byte) bool {
	for _, raw := range strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return strings.HasPrefix(line, "system ")
	}
	return false
}

func extractName(kind, header string) string {
	var re *regexp.Regexp
	switch kind {
	case "event":
		re = eventHeaderRe
	case "state":
		re = stateHeaderRe
	case "condition":
		re = condHeaderRe
	case "always":
		re = alwaysHeaderRe
	case "view":
		re = viewHeaderRe
	}
	if re != nil {
		if mm := re.FindStringSubmatch(header); len(mm) > 1 {
			return mm[1]
		}
	}
	rest := strings.TrimSpace(strings.TrimPrefix(header, kind))
	rest = strings.TrimSuffix(rest, ":")
	for _, sep := range []string{" ", "(", ":"} {
		if idx := strings.Index(rest, sep); idx >= 0 {
			rest = rest[:idx]
		}
	}
	return strings.TrimSpace(rest)
}

func parseRule(b Block) RuleInfo {
	r := RuleInfo{Block: b}
	header := strings.TrimSuffix(b.Header, ":")
	fields := strings.Fields(header)
	if len(fields) >= 2 && fields[0] == "rule" {
		for i := 2; i < len(fields); i++ {
			if fields[i] == "on" && i+1 < len(fields) {
				r.OnEvent = strings.TrimSuffix(fields[i+1], ":")
			}
			if fields[i] == "every" && i+1 < len(fields) {
				r.Every = strings.Join(fields[i+1:], " ")
			}
		}
	}
	section := "when"
	for _, raw := range b.Body {
		s := strings.TrimSpace(raw)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if s == "when:" {
			section = "when"
			continue
		}
		if s == "do:" || (strings.HasPrefix(s, "do ") && strings.HasSuffix(s, ":")) {
			section = "do"
			continue
		}
		if s == "then:" {
			section = "then"
			continue
		}
		if strings.HasPrefix(s, "catch ") && strings.HasSuffix(s, ":") {
			section = "catch"
			r.CatchLines = append(r.CatchLines, s)
			continue
		}
		switch section {
		case "when":
			r.WhenLines = append(r.WhenLines, s)
		case "do":
			r.DoLines = append(r.DoLines, s)
		case "then":
			r.ThenLines = append(r.ThenLines, s)
		case "catch":
			r.CatchLines = append(r.CatchLines, s)
		}
	}
	r.Functions = uniqueMatches(funcCallRe, strings.Join(r.DoLines, "\n"))
	r.Reads = uniqueMatches(stateRefRe, strings.Join(append(r.WhenLines, r.DoLines...), "\n"))
	writeSeen := map[string]bool{}
	for _, line := range append(r.ThenLines, r.CatchLines...) {
		if mm := setRe.FindStringSubmatch(line); len(mm) > 1 {
			writeSeen[strings.TrimSpace(mm[1])] = true
		}
	}
	for w := range writeSeen {
		r.Writes = append(r.Writes, w)
	}
	sort.Strings(r.Writes)
	return r
}

func uniqueMatches(re *regexp.Regexp, text string) []string {
	seen := map[string]bool{}
	for _, mm := range re.FindAllStringSubmatch(text, -1) {
		if len(mm) > 1 {
			seen[mm[1]] = true
		}
	}
	out := []string{}
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func groupSections(blocks []Block) []SectionGroup {
	order := []string{}
	mp := map[string][]Block{}
	for _, b := range blocks {
		s := b.Section
		if _, ok := mp[s]; !ok {
			order = append(order, s)
		}
		mp[s] = append(mp[s], b)
	}
	out := []SectionGroup{}
	for _, s := range order {
		out = append(out, SectionGroup{Name: s, Blocks: mp[s]})
	}
	return out
}

func diagnose(m ProjectModel) []string {
	out := []string{}
	if len(m.Rules) == 0 {
		out = append(out, "Нет правил: система ничего не делает.")
	}
	if len(m.Always) == 0 {
		out = append(out, "Нет always-законов: safety не зафиксирована явно.")
	}
	if len(m.InferredFunctions) > 0 {
		out = append(out, "Функции вызываются в do-блоках, но не объявлены явно: "+strings.Join(m.InferredFunctions, ", "))
	}
	for name, r := range m.Rules {
		if actuatorRe.MatchString(name+"\n"+r.Block.Source()) && !safetyRe.MatchString(strings.Join(r.WhenLines, "\n")) {
			out = append(out, fmt.Sprintf("%s: actuator-сценарий без видимого safety-условия в when.", name))
		}
		if len(r.DoLines) > 0 && len(r.ThenLines) == 0 {
			out = append(out, fmt.Sprintf("%s: есть do-блок, но нет then-записи результата.", name))
		}
		if strings.Contains(strings.ToLower(name), "dose") && !strings.Contains(strings.ToLower(strings.Join(r.WhenLines, "\n")), "water") {
			out = append(out, fmt.Sprintf("%s: дозирование без явной проверки воды/уровня.", name))
		}
	}
	return out
}
