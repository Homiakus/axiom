package triz

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/Homiakus/axiom/internal/diag"
	"github.com/Homiakus/axiom/internal/lang"
)

type SourceMapEntry struct {
	TRIZKind string
	TRIZName string
	TRIZLine int
	V0Kind   string
	V0Name   string
	V0Line   int
}

type Result struct {
	Source      []byte
	Diagnostics diag.Errors
	SourceMap   []SourceMapEntry
}

type decl struct {
	Kind int
	Name string
	Line int
}

type fieldDecl struct {
	Name       string
	Type       string
	Default    string
	HasDefault bool
}

type stateDecl struct {
	Name    string
	Indexed bool
	Fields  []fieldDecl
	Line    int
}

type eventDecl struct {
	Name   string
	Params []fieldDecl
	Line   int
}

type conditionDecl struct {
	Name string
	Body []string
	Line int
}

type profileDecl struct {
	Name    string
	Entries map[string]string
	Flags   map[string]bool
	Line    int
}

type functionDecl struct {
	Name    string
	Params  []fieldDecl
	Outputs []fieldDecl
	Line    int
}

type ruleDecl struct {
	Name      string
	HeaderOn  string
	WhenLines []string
	DoProfile string
	DoAssign  string
	DoCall    callExpr
	ThenLines []string
	Line      int
}

type simpleDecl struct {
	Name string
	Body []string
	Line int
}

type model struct {
	System      string
	States      []stateDecl
	Events      []eventDecl
	Conditions  []conditionDecl
	Profiles    []profileDecl
	Functions   []functionDecl
	Rules       []ruleDecl
	Always      []simpleDecl
	Views       []simpleDecl
	Diagnostics diag.Errors
}

type callExpr struct {
	Name string
	Args []argExpr
	Raw  string
}

type argExpr struct {
	Name string
	Expr string
}

var (
	topDeclRe       = regexp.MustCompile(`^(system|state|event|condition|profile|function|rule|always|view)\b\s*(.*)$`)
	indexedStateRe  = regexp.MustCompile(`^([A-Za-z_][\w]*)\[[^]]+\]$`)
	stateIndexRefRe = regexp.MustCompile(`\b([A-Z][A-Za-z0-9_]*)\[[^]]+\]\.`)
	refLikeRe       = regexp.MustCompile(`\b[A-Z][A-Za-z0-9_]*(?:\[[^]]+\])?\.[A-Za-z_][A-Za-z0-9_]*`)
	objectFieldRe   = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*:\s*([A-Za-z_][A-Za-z0-9_]*(?:<[^>]+>)?\??)`)
	dangerousFnRe   = regexp.MustCompile(`(?i)(pump|dose|doser|light|siren|valve|actuator|relay|uart|command)`)
)

func LooksLike(source []byte) bool {
	for _, line := range strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n") {
		s := strings.TrimSpace(stripComment(line))
		if s == "" {
			continue
		}
		return strings.HasPrefix(s, "system ")
	}
	return false
}

func Normalize(source []byte) (*Result, error) {
	m, err := parse(source)
	if err != nil {
		return nil, err
	}
	out := emitter{model: m}
	src := out.emit()
	return &Result{Source: src, Diagnostics: append(m.Diagnostics, out.diags...), SourceMap: out.sourceMap}, nil
}

type sourceLine struct {
	no     int
	indent int
	text   string
}

func parse(source []byte) (*model, error) {
	lines, err := cleanLines(string(source))
	if err != nil {
		return nil, err
	}
	m := &model{}
	for i := 0; i < len(lines); {
		line := lines[i]
		if line.indent != 0 {
			m.add("AXT001", "syntax", "", line.no, "expected top-level declaration", "")
			i++
			continue
		}
		mm := topDeclRe.FindStringSubmatch(line.text)
		if len(mm) == 0 {
			m.add("AXT001", "syntax", "", line.no, "unknown TRIZ declaration", "Use system/state/event/condition/profile/function/rule/always/view.")
			i++
			continue
		}
		kind, rest := mm[1], strings.TrimSpace(mm[2])
		block, next := collectBlock(lines, i, kind)
		switch kind {
		case "system":
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				m.add("AXT001", "syntax", "system", line.no, "system name is required", "")
			} else {
				m.System = fields[0]
			}
		case "state":
			m.States = append(m.States, parseState(m, line, rest, block))
		case "event":
			m.Events = append(m.Events, parseEvent(m, line, rest))
		case "condition":
			m.Conditions = append(m.Conditions, conditionDecl{Name: trimHeaderName(rest), Body: block, Line: line.no})
		case "profile":
			m.Profiles = append(m.Profiles, parseProfile(line, rest, block))
		case "function":
			m.Functions = append(m.Functions, parseFunction(m, line, rest, block))
		case "rule":
			m.Rules = append(m.Rules, parseRule(m, line, rest, block))
		case "always":
			m.Always = append(m.Always, simpleDecl{Name: trimHeaderName(rest), Body: block, Line: line.no})
		case "view":
			m.Views = append(m.Views, simpleDecl{Name: trimHeaderName(rest), Body: block, Line: line.no})
		}
		i = next
	}
	if m.System == "" {
		m.add("AXT001", "syntax", "system", 0, "system declaration is required", "Start TRIZ files with system <Name>.")
	}
	return m, nil
}

func cleanLines(source string) ([]sourceLine, error) {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	var lines []sourceLine
	for i, raw := range strings.Split(source, "\n") {
		if strings.Contains(raw, "\t") {
			return nil, diag.Errors{{Code: "AXT001", Kind: "syntax", Line: i + 1, Message: "tabs are not allowed"}}
		}
		clean := stripComment(strings.TrimRight(raw, "\r"))
		if strings.TrimSpace(clean) == "" {
			continue
		}
		indent := len(clean) - len(strings.TrimLeft(clean, " "))
		lines = append(lines, sourceLine{no: i + 1, indent: indent, text: strings.TrimSpace(clean)})
	}
	return lines, nil
}

func stripComment(line string) string {
	inString := false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if r == '#' && !inString {
			return line[:i]
		}
	}
	return line
}

func collectBlock(lines []sourceLine, start int, kind string) ([]string, int) {
	base := lines[start].indent
	var block []string
	i := start + 1
	for i < len(lines) {
		if lines[i].indent <= base {
			if kind != "rule" {
				break
			}
			text := strings.TrimSpace(lines[i].text)
			if !(text == "then:" || text == "do:" || (strings.HasPrefix(text, "do ") && strings.HasSuffix(text, ":"))) {
				break
			}
		}
		block = append(block, lines[i].text)
		i++
	}
	return block, i
}

func parseState(m *model, line sourceLine, rest string, block []string) stateDecl {
	name := trimHeaderName(rest)
	indexed := false
	if mm := indexedStateRe.FindStringSubmatch(name); len(mm) > 1 {
		name = mm[1]
		indexed = true
		m.add("AXT205", "symbols", name, line.no, "indexed state is normalized as a single context in this version", "Dynamic Zone[event.zone] paths are collapsed to Zone.<field>.")
	}
	return stateDecl{Name: name, Indexed: indexed, Fields: parseFields(m, block, line.no), Line: line.no}
}

func parseEvent(m *model, line sourceLine, rest string) eventDecl {
	name, params := parseSignature(m, rest, line.no)
	return eventDecl{Name: name, Params: params, Line: line.no}
}

func parseProfile(line sourceLine, rest string, block []string) profileDecl {
	p := profileDecl{Name: trimHeaderName(rest), Entries: map[string]string{}, Flags: map[string]bool{}, Line: line.no}
	for _, raw := range block {
		s := strings.TrimSpace(raw)
		if key, val, ok := strings.Cut(s, ":"); ok {
			p.Entries[strings.TrimSpace(key)] = strings.TrimSpace(val)
		} else if s != "" {
			p.Flags[s] = true
		}
	}
	return p
}

func parseFunction(m *model, line sourceLine, rest string, block []string) functionDecl {
	header := strings.TrimSpace(rest)
	if len(block) > 0 {
		header += " " + strings.Join(block, " ")
	}
	left, right, ok := strings.Cut(header, "->")
	if !ok {
		m.add("AXT001", "syntax", trimHeaderName(rest), line.no, "function return type is required", "Use function Name(args) -> { field: Type }.")
		name, params := parseSignature(m, left, line.no)
		return functionDecl{Name: name, Params: params, Line: line.no}
	}
	name, params := parseSignature(m, strings.TrimSpace(left), line.no)
	return functionDecl{Name: name, Params: params, Outputs: parseObjectFields(m, right, line.no), Line: line.no}
}

func parseRule(m *model, line sourceLine, rest string, block []string) ruleDecl {
	r := ruleDecl{Line: line.no}
	header := strings.TrimSpace(strings.TrimSuffix(rest, ":"))
	fields := strings.Fields(header)
	if len(fields) > 0 {
		r.Name = fields[0]
	}
	for i := 1; i+1 < len(fields); i++ {
		if fields[i] == "on" {
			r.HeaderOn = strings.TrimSuffix(fields[i+1], ":")
		}
	}
	section := "when"
	if strings.HasSuffix(strings.TrimSpace(rest), "when:") {
		section = "when"
	}
	var doLines []string
	for _, raw := range block {
		s := strings.TrimSpace(raw)
		switch {
		case s == "when:":
			section = "when"
			continue
		case s == "then:":
			section = "then"
			continue
		case s == "do:":
			section = "do"
			r.DoProfile = ""
			continue
		case strings.HasPrefix(s, "do ") && strings.HasSuffix(s, ":"):
			section = "do"
			r.DoProfile = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "do "), ":"))
			continue
		}
		switch section {
		case "when":
			r.WhenLines = append(r.WhenLines, s)
		case "do":
			doLines = append(doLines, s)
		case "then":
			r.ThenLines = append(r.ThenLines, strings.TrimPrefix(s, "set "))
		}
	}
	if len(doLines) > 0 {
		r.DoAssign, r.DoCall = parseDoCall(m, strings.Join(doLines, " "), line.no)
	}
	return r
}

func parseFields(m *model, lines []string, lineNo int) []fieldDecl {
	var out []fieldDecl
	for _, raw := range lines {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		name, typDefault, ok := strings.Cut(s, ":")
		if !ok {
			m.add("AXT001", "syntax", "", lineNo, "expected typed field", s)
			continue
		}
		typ := strings.TrimSpace(typDefault)
		fd := fieldDecl{Name: strings.TrimSpace(name)}
		if left, right, ok := strings.Cut(typ, "="); ok {
			fd.Type = normalizeType(strings.TrimSpace(left))
			fd.Default = normalizeExpr(strings.TrimSpace(right))
			fd.HasDefault = true
		} else {
			fd.Type = normalizeType(typ)
		}
		out = append(out, fd)
	}
	return out
}

func parseObjectFields(m *model, src string, lineNo int) []fieldDecl {
	src = strings.TrimSpace(src)
	src = strings.TrimPrefix(src, "{")
	src = strings.TrimSuffix(src, "}")
	src = strings.ReplaceAll(src, "\n", " ")
	matches := objectFieldRe.FindAllStringSubmatch(src, -1)
	if len(matches) > 0 {
		out := make([]fieldDecl, 0, len(matches))
		for _, mm := range matches {
			out = append(out, fieldDecl{Name: mm[1], Type: normalizeType(mm[2])})
		}
		return out
	}
	parts := splitTopLevel(src, ',')
	if len(parts) == 1 && strings.Contains(parts[0], " ") {
		parts = strings.FieldsFunc(src, func(r rune) bool { return r == ',' || r == '\n' })
	}
	var lines []string
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p != "" {
			lines = append(lines, p)
		}
	}
	return parseFields(m, lines, lineNo)
}

func parseSignature(m *model, src string, lineNo int) (string, []fieldDecl) {
	src = strings.TrimSpace(strings.TrimSuffix(src, ":"))
	open := strings.Index(src, "(")
	if open < 0 {
		return strings.TrimSpace(src), nil
	}
	close := strings.LastIndex(src, ")")
	if close < open {
		m.add("AXT001", "syntax", src, lineNo, "unclosed parameter list", "")
		return strings.TrimSpace(src[:open]), nil
	}
	name := strings.TrimSpace(src[:open])
	var params []fieldDecl
	for _, part := range splitTopLevel(src[open+1:close], ',') {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		n, t, ok := strings.Cut(p, ":")
		if !ok {
			m.add("AXT001", "syntax", name, lineNo, "expected parameter name: Type", p)
			continue
		}
		params = append(params, fieldDecl{Name: strings.TrimSpace(n), Type: normalizeType(strings.TrimSpace(t))})
	}
	return name, params
}

func parseDoCall(m *model, src string, lineNo int) (string, callExpr) {
	left, right, ok := strings.Cut(src, "=")
	if !ok {
		m.add("AXT301", "effects", "", lineNo, "do block must assign a function result", "Use result = Function(args).")
		return "", callExpr{}
	}
	call := parseCall(strings.TrimSpace(right))
	if call.Name == "" {
		m.add("AXT301", "effects", "", lineNo, "function call is required in do block", "")
	}
	return strings.TrimSpace(left), call
}

func parseCall(src string) callExpr {
	src = strings.TrimSpace(src)
	open := strings.Index(src, "(")
	close := strings.LastIndex(src, ")")
	if open < 0 || close < open {
		return callExpr{Name: strings.TrimSpace(src), Raw: src}
	}
	call := callExpr{Name: strings.TrimSpace(src[:open]), Raw: src}
	for _, part := range splitTopLevel(src[open+1:close], ',') {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if name, expr, ok := strings.Cut(p, ":"); ok {
			call.Args = append(call.Args, argExpr{Name: strings.TrimSpace(name), Expr: strings.TrimSpace(expr)})
		} else {
			call.Args = append(call.Args, argExpr{Expr: p})
		}
	}
	return call
}

func splitTopLevel(src string, sep rune) []string {
	var out []string
	depth := 0
	start := 0
	for i, r := range src {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		default:
			if r == sep && depth == 0 {
				out = append(out, src[start:i])
				start = i + len(string(r))
			}
		}
	}
	out = append(out, src[start:])
	return out
}

func trimHeaderName(rest string) string {
	rest = strings.TrimSpace(strings.TrimSuffix(rest, ":"))
	if i := strings.IndexAny(rest, " ("); i >= 0 {
		rest = rest[:i]
	}
	return strings.TrimSpace(rest)
}

type emitter struct {
	model               *model
	diags               diag.Errors
	sourceMap           []SourceMapEntry
	line                int
	eventNames          map[string]bool
	conditionNames      map[string]bool
	functionNames       map[string]bool
	profileNames        map[string]bool
	firstCallByFunction map[string]callExpr
	profileByFunction   map[string]string
	activityNames       map[string]string
}

func (e *emitter) emit() []byte {
	e.prepareIndexes()
	var b bytes.Buffer
	e.write(&b, "domain %s\n\n", e.model.System)
	e.mapLine("system", e.model.System, 1, "domain", e.model.System)

	for _, ev := range e.model.Events {
		e.emitSignal(&b, ev)
	}
	for _, st := range e.model.States {
		e.emitContext(&b, st)
	}
	for _, c := range e.model.Conditions {
		e.emitFact(&b, c)
	}
	for _, p := range e.model.Profiles {
		e.emitPolicy(&b, p)
	}
	if !e.hasProfile("defaultExternal") {
		e.write(&b, "policy defaultExternal:\n")
		e.write(&b, "  retry: 0\n")
		e.write(&b, "  timeout: 10s\n")
		e.write(&b, "  concurrency: once\n")
		e.write(&b, "  idempotency: required\n")
		e.write(&b, "  audit: optional\n\n")
	}
	for _, fn := range e.model.Functions {
		e.emitActivity(&b, fn)
	}
	for _, r := range e.model.Rules {
		e.emitRule(&b, r)
	}
	for _, a := range e.model.Always {
		e.emitClaim(&b, a)
	}
	for _, v := range e.model.Views {
		e.emitQuery(&b, v)
	}
	return b.Bytes()
}

func (e *emitter) write(b *bytes.Buffer, format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	b.WriteString(text)
	e.line += strings.Count(text, "\n")
}

func (e *emitter) prepareIndexes() {
	e.eventNames = make(map[string]bool, len(e.model.Events))
	for _, ev := range e.model.Events {
		e.eventNames[ev.Name] = true
	}
	e.conditionNames = make(map[string]bool, len(e.model.Conditions))
	for _, c := range e.model.Conditions {
		e.conditionNames[c.Name] = true
	}
	e.functionNames = make(map[string]bool, len(e.model.Functions))
	for _, fn := range e.model.Functions {
		e.functionNames[fn.Name] = true
	}
	e.profileNames = make(map[string]bool, len(e.model.Profiles))
	for _, p := range e.model.Profiles {
		e.profileNames[p.Name] = true
	}
	ruleNames := make(map[string]bool, len(e.model.Rules))
	for _, r := range e.model.Rules {
		ruleNames[r.Name] = true
	}
	e.activityNames = make(map[string]string, len(e.model.Functions))
	for _, fn := range e.model.Functions {
		if ruleNames[fn.Name] {
			e.activityNames[fn.Name] = fn.Name + "Activity"
		} else {
			e.activityNames[fn.Name] = fn.Name
		}
	}
	e.firstCallByFunction = map[string]callExpr{}
	e.profileByFunction = map[string]string{}
	for _, r := range e.model.Rules {
		if r.DoCall.Name == "" {
			continue
		}
		if _, ok := e.firstCallByFunction[r.DoCall.Name]; !ok {
			e.firstCallByFunction[r.DoCall.Name] = r.DoCall
		}
		if _, ok := e.profileByFunction[r.DoCall.Name]; !ok {
			if r.DoProfile != "" {
				e.profileByFunction[r.DoCall.Name] = r.DoProfile
			} else {
				e.profileByFunction[r.DoCall.Name] = "defaultExternal"
			}
		}
	}
}

func (e *emitter) mapLine(tkind, tname string, tline int, vkind, vname string) {
	e.sourceMap = append(e.sourceMap, SourceMapEntry{TRIZKind: tkind, TRIZName: tname, TRIZLine: tline, V0Kind: vkind, V0Name: vname, V0Line: e.line + 1})
}

func (e *emitter) emitSignal(b *bytes.Buffer, ev eventDecl) {
	e.mapLine("event", ev.Name, ev.Line, "signal", ev.Name)
	if len(ev.Params) == 0 {
		e.write(b, "signal %s\n\n", ev.Name)
		return
	}
	e.write(b, "signal %s:\n", ev.Name)
	for _, p := range ev.Params {
		e.write(b, "  %s: %s\n", p.Name, p.Type)
	}
	e.write(b, "\n")
}

func (e *emitter) emitContext(b *bytes.Buffer, st stateDecl) {
	e.mapLine("state", st.Name, st.Line, "context", st.Name)
	e.write(b, "context %s:\n", st.Name)
	for _, f := range st.Fields {
		if f.HasDefault {
			e.write(b, "  %s: %s = %s\n", f.Name, f.Type, f.Default)
		} else {
			e.write(b, "  %s: %s\n", f.Name, f.Type)
		}
	}
	e.write(b, "\n")
}

func (e *emitter) emitFact(b *bytes.Buffer, c conditionDecl) {
	e.mapLine("condition", c.Name, c.Line, "fact", c.Name)
	e.write(b, "fact %s when:\n", c.Name)
	for _, line := range c.Body {
		expr := normalizeExpr(line)
		if unsupportedAggregate(expr) {
			e.add("AXT201", "types", c.Name, c.Line, "unsupported aggregate expression in condition", line)
			expr = "false"
		}
		e.write(b, "  %s\n", expr)
	}
	e.write(b, "\n")
}

func (e *emitter) emitPolicy(b *bytes.Buffer, p profileDecl) {
	e.mapLine("profile", p.Name, p.Line, "policy", p.Name)
	e.write(b, "policy %s:\n", p.Name)
	e.write(b, "  retry: %s\n", valueOr(p.Entries["retry"], "0"))
	e.write(b, "  timeout: %s\n", valueOr(p.Entries["timeout"], "10s"))
	e.write(b, "  concurrency: %s\n", profileConcurrency(p))
	e.write(b, "  idempotency: %s\n", profileIdempotency(p))
	e.write(b, "  audit: %s\n\n", profileAudit(p))
}

func (e *emitter) emitActivity(b *bytes.Buffer, fn functionDecl) {
	profile := e.profileForFunction(fn.Name)
	activityName := e.activityName(fn.Name)
	e.mapLine("function", fn.Name, fn.Line, "activity", activityName)
	e.write(b, "activity %s:\n", activityName)
	e.write(b, "  input:\n")
	for _, in := range e.inputsForFunction(fn) {
		e.write(b, "    %s = %s\n", in.Name, normalizeExpr(in.Default))
	}
	e.write(b, "  output:\n")
	for _, out := range fn.Outputs {
		e.write(b, "    %s: %s\n", out.Name, out.Type)
	}
	effect := "external"
	if profile == "local" {
		effect = "local"
	}
	e.write(b, "  effect: %s\n", effect)
	if effect == "external" {
		e.write(b, "  idempotencyKey: %s\n", e.idempotencyKey(fn))
	}
	e.write(b, "  policy: %s\n\n", profile)
}

func (e *emitter) emitRule(b *bytes.Buffer, r ruleDecl) {
	e.mapLine("rule", r.Name, r.Line, "rule", r.Name)
	e.write(b, "rule %s:\n", r.Name)
	triggers, when, require := e.ruleParts(r)
	if r.DoCall.Name != "" && !e.functionNames[r.DoCall.Name] {
		e.add("AXT101", "symbols", r.DoCall.Name, r.Line, "unresolved function reference", "Declare the function before using it in a do block.")
	}
	if r.DoCall.Name != "" && looksDangerousFunction(r.DoCall.Name) && !hasSafetyGuard(append(append([]string{}, when...), require...)) {
		e.add("AXT501", "safety", r.Name, r.Line, "actuator-like function without visible safety guard", "Add a condition such as CanUseHardware/CanDose or an always law that protects this path.")
	}
	if len(triggers) == 0 {
		e.add("AXT401", "rules", r.Name, r.Line, "rule has no trigger", "Add an event, changed(...) trigger, or a state condition.")
		return
	}
	if len(triggers) == 1 {
		e.write(b, "  on %s\n", triggers[0])
	} else {
		e.write(b, "  on:\n")
		for _, tr := range triggers {
			e.write(b, "    %s\n", tr)
		}
	}
	if len(when) > 0 {
		e.write(b, "  when:\n")
		for _, expr := range when {
			e.write(b, "    %s\n", expr)
		}
	}
	if len(require) > 0 {
		e.write(b, "  require:\n")
		for _, req := range require {
			e.write(b, "    %s\n", req)
		}
	}
	if r.DoCall.Name != "" {
		e.write(b, "  run: %s\n", e.activityName(r.DoCall.Name))
	}
	e.write(b, "  write:\n")
	for _, raw := range r.ThenLines {
		name, expr, ok := strings.Cut(raw, "=")
		if !ok {
			e.add("AXT401", "rules", r.Name, r.Line, "invalid then write", raw)
			continue
		}
		e.write(b, "    %s = %s\n", normalizeTarget(strings.TrimSpace(name)), normalizeExpr(strings.TrimSpace(expr)))
	}
	e.write(b, "\n")
}

func (e *emitter) emitClaim(b *bytes.Buffer, a simpleDecl) {
	var exprs []string
	for _, raw := range a.Body {
		expr := normalizeExpr(raw)
		if unsupportedAggregate(expr) {
			e.add("AXT201", "types", a.Name, a.Line, "unsupported aggregate expression in always law", raw)
			continue
		}
		exprs = append(exprs, expr)
	}
	if len(exprs) == 0 {
		return
	}
	e.mapLine("always", a.Name, a.Line, "claim", a.Name)
	e.write(b, "claim %s:\n", a.Name)
	e.write(b, "  always:\n")
	for _, expr := range exprs {
		e.write(b, "    %s\n", expr)
	}
	e.write(b, "\n")
}

func (e *emitter) emitQuery(b *bytes.Buffer, v simpleDecl) {
	e.mapLine("view", v.Name, v.Line, "query", v.Name)
	e.write(b, "query %s:\n", v.Name)
	e.write(b, "  return:\n")
	for _, raw := range v.Body {
		name, expr, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		e.write(b, "    %s = %s\n", strings.TrimSpace(name), normalizeExpr(strings.TrimSpace(expr)))
	}
	e.write(b, "\n")
}

func (e *emitter) ruleParts(r ruleDecl) ([]string, []string, []string) {
	var triggers, when, require []string
	if r.HeaderOn != "" {
		triggers = append(triggers, normalizeTrigger(r.HeaderOn))
	}
	for _, raw := range r.WhenLines {
		s := strings.TrimSpace(raw)
		switch {
		case e.eventNames[s]:
			triggers = append(triggers, s)
		case strings.HasPrefix(s, "changed(") || strings.HasPrefix(s, "timer("):
			triggers = append(triggers, normalizeTrigger(s))
		case e.conditionNames[s]:
			require = append(require, s)
		default:
			if IsExportedIdent(s) && !strings.ContainsAny(s, ".=<>!()[] ") {
				e.add("AXT101", "symbols", s, r.Line, "unresolved condition or event reference", "Declare the condition/event or replace it with a pure expression.")
				continue
			}
			expr := normalizeExpr(s)
			if unsupportedAggregate(expr) {
				e.add("AXT201", "types", r.Name, r.Line, "unsupported expression in rule condition", s)
				continue
			}
			when = append(when, expr)
		}
	}
	if len(triggers) == 0 {
		for _, expr := range when {
			for _, ref := range refsIn(expr) {
				triggers = append(triggers, "changed("+ref+")")
			}
		}
		sort.Strings(triggers)
		triggers = unique(triggers)
	}
	return triggers, when, require
}

func (e *emitter) inputsForFunction(fn functionDecl) []fieldDecl {
	var out []fieldDecl
	if call, ok := e.firstCallByFunction[fn.Name]; ok {
		for i, param := range fn.Params {
			expr := "null"
			name := param.Name
			if i < len(call.Args) {
				arg := call.Args[i]
				expr = arg.Expr
				if arg.Name != "" {
					name = arg.Name
				}
			}
			out = append(out, fieldDecl{Name: name, Type: param.Type, Default: expr})
		}
		return out
	}
	for _, param := range fn.Params {
		out = append(out, fieldDecl{Name: param.Name, Type: param.Type, Default: "null"})
	}
	return out
}

func (e *emitter) profileForFunction(name string) string {
	if profile, ok := e.profileByFunction[name]; ok {
		return profile
	}
	return "defaultExternal"
}

func (e *emitter) idempotencyKey(fn functionDecl) string {
	var args []string
	args = append(args, fmt.Sprintf("%q", fn.Name))
	for _, in := range e.inputsForFunction(fn) {
		args = append(args, normalizeExpr(in.Default))
	}
	return "hash(" + strings.Join(args, ", ") + ")"
}

func (e *emitter) activityName(name string) string {
	if activityName, ok := e.activityNames[name]; ok {
		return activityName
	}
	return name
}

func (e *emitter) eventSet() map[string]bool {
	return e.eventNames
}

func (e *emitter) conditionSet() map[string]bool {
	return e.conditionNames
}

func (e *emitter) functionSet() map[string]bool {
	return e.functionNames
}

func (e *emitter) hasProfile(name string) bool {
	return e.profileNames[name]
}

func (e *emitter) add(code, kind, entity string, line int, message, hint string) {
	e.diags = append(e.diags, diag.Error{Code: code, Kind: kind, Entity: entity, Line: line, Message: message, Hint: hint})
}

func normalizeType(typ string) string {
	typ = strings.TrimSpace(typ)
	nullable := strings.HasSuffix(typ, "?")
	typ = strings.TrimSuffix(typ, "?")
	switch typ {
	case "Text":
		typ = "String"
	}
	if nullable {
		return typ + "?"
	}
	return typ
}

func normalizeExpr(expr string) string {
	expr = strings.TrimSpace(expr)
	expr = strings.ReplaceAll(expr, "event.", "signal.")
	expr = strings.ReplaceAll(expr, "result.", "output.")
	expr = stateIndexRefRe.ReplaceAllString(expr, "$1.")
	return expr
}

func normalizeTarget(target string) string {
	return normalizeExpr(target)
}

func normalizeTrigger(trigger string) string {
	return normalizeExpr(trigger)
}

func unsupportedAggregate(expr string) bool {
	return strings.HasPrefix(strings.TrimSpace(expr), "all ") || strings.Contains(expr, " all ")
}

func refsIn(expr string) []string {
	expr = normalizeExpr(expr)
	var out []string
	for _, ref := range refLikeRe.FindAllString(expr, -1) {
		ref = normalizeTarget(ref)
		if strings.HasPrefix(ref, "signal.") || strings.HasPrefix(ref, "output.") {
			continue
		}
		out = append(out, ref)
	}
	return unique(out)
}

func profileConcurrency(p profileDecl) string {
	if p.Flags["once"] {
		return "once"
	}
	return valueOr(p.Entries["concurrency"], "once")
}

func profileIdempotency(p profileDecl) string {
	if p.Flags["idempotent"] {
		return "required"
	}
	return valueOr(p.Entries["idempotency"], "none")
}

func profileAudit(p profileDecl) string {
	if p.Flags["audited"] {
		return "required"
	}
	return valueOr(p.Entries["audit"], "optional")
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func (m *model) add(code, kind, entity string, line int, message, hint string) {
	m.Diagnostics = append(m.Diagnostics, diag.Error{Code: code, Kind: kind, Entity: entity, Line: line, Message: message, Hint: hint})
}

func unique(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func IsExportedIdent(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

func looksDangerousFunction(name string) bool {
	return dangerousFnRe.MatchString(name)
}

func hasSafetyGuard(lines []string) bool {
	text := strings.ToLower(strings.Join(lines, "\n"))
	return strings.Contains(text, "safety") ||
		strings.Contains(text, "estop") ||
		strings.Contains(text, "canusehardware") ||
		strings.Contains(text, "candose") ||
		strings.Contains(text, "allowed")
}

var _ = lang.Expr{}
