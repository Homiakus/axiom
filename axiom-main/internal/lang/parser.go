package lang

import (
	"fmt"
	"strings"
)

type sourceLine struct {
	no     int
	indent int
	text   string
}

func Parse(source []byte) (*Module, error) {
	lines, err := cleanLines(string(source))
	if err != nil {
		return nil, err
	}
	p := fileParser{lines: lines}
	return p.parse()
}

func cleanLines(source string) ([]sourceLine, error) {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	var lines []sourceLine
	for i, raw := range strings.Split(source, "\n") {
		if strings.Contains(raw, "\t") {
			return nil, &ParseError{Line: i + 1, Msg: "tabs are not allowed"}
		}
		withoutComment := stripComment(raw)
		if strings.TrimSpace(withoutComment) == "" {
			continue
		}
		indent := len(withoutComment) - len(strings.TrimLeft(withoutComment, " "))
		lines = append(lines, sourceLine{
			no:     i + 1,
			indent: indent,
			text:   strings.TrimSpace(withoutComment),
		})
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

type fileParser struct {
	lines []sourceLine
	pos   int
}

func (p *fileParser) parse() (*Module, error) {
	module := &Module{}
	for !p.done() {
		line := p.peek()
		if line.indent != 0 {
			return nil, p.err(line, "expected top-level declaration")
		}
		switch {
		case strings.HasPrefix(line.text, "domain "):
			if module.Domain != "" {
				return nil, p.err(line, "duplicate domain declaration")
			}
			module.Domain = strings.TrimSpace(strings.TrimPrefix(line.text, "domain "))
			p.pos++
		case strings.HasPrefix(line.text, "import "):
			decl, err := p.parseImport(line)
			if err != nil {
				return nil, err
			}
			module.Imports = append(module.Imports, decl)
			p.pos++
		case strings.HasPrefix(line.text, "signal "):
			decl, err := p.parseSignal(line)
			if err != nil {
				return nil, err
			}
			module.Signals = append(module.Signals, decl)
		case strings.HasPrefix(line.text, "context "):
			decl, err := p.parseContext(line)
			if err != nil {
				return nil, err
			}
			module.Contexts = append(module.Contexts, decl)
		case strings.HasPrefix(line.text, "computed "):
			decl, err := p.parseComputed(line)
			if err != nil {
				return nil, err
			}
			module.Computeds = append(module.Computeds, decl)
		case strings.HasPrefix(line.text, "fact "):
			decl, err := p.parseFact(line)
			if err != nil {
				return nil, err
			}
			module.Facts = append(module.Facts, decl)
		case strings.HasPrefix(line.text, "policy "):
			decl, err := p.parsePolicy(line)
			if err != nil {
				return nil, err
			}
			module.Policies = append(module.Policies, decl)
		case strings.HasPrefix(line.text, "activity "):
			decl, err := p.parseActivity(line)
			if err != nil {
				return nil, err
			}
			module.Activities = append(module.Activities, decl)
		case strings.HasPrefix(line.text, "rule "):
			decl, err := p.parseRule(line)
			if err != nil {
				return nil, err
			}
			module.Rules = append(module.Rules, decl)
		case strings.HasPrefix(line.text, "claim "):
			decl, err := p.parseClaim(line)
			if err != nil {
				return nil, err
			}
			module.Claims = append(module.Claims, decl)
		case strings.HasPrefix(line.text, "query "):
			decl, err := p.parseQuery(line)
			if err != nil {
				return nil, err
			}
			module.Queries = append(module.Queries, decl)
		default:
			return nil, p.err(line, "unknown declaration")
		}
	}
	if module.Domain == "" {
		return nil, &ParseError{Msg: "domain declaration is required"}
	}
	return module, nil
}

func (p *fileParser) parseImport(line sourceLine) (ImportDecl, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line.text, "import "))
	parts := strings.Fields(rest)
	if len(parts) == 1 {
		return ImportDecl{Name: parts[0]}, nil
	}
	if len(parts) == 3 && parts[1] == "as" {
		return ImportDecl{Name: parts[0], Alias: parts[2]}, nil
	}
	return ImportDecl{}, p.err(line, "invalid import declaration")
}

func (p *fileParser) parseSignal(line sourceLine) (SignalDecl, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line.text, "signal "))
	p.pos++
	if strings.HasSuffix(rest, ":") {
		name := strings.TrimSuffix(rest, ":")
		fields, err := p.parseFields(line.indent + 2)
		return SignalDecl{Name: strings.TrimSpace(name), Fields: fields, Line: line.no}, err
	}
	return SignalDecl{Name: rest, Line: line.no}, nil
}

func (p *fileParser) parseContext(line sourceLine) (ContextDecl, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line.text, "context "))
	if !strings.HasSuffix(rest, ":") {
		return ContextDecl{}, p.err(line, "context requires a field block")
	}
	p.pos++
	fields, err := p.parseFields(line.indent + 2)
	return ContextDecl{Name: strings.TrimSpace(strings.TrimSuffix(rest, ":")), Fields: fields, Line: line.no}, err
}

func (p *fileParser) parseComputed(line sourceLine) (ComputedDecl, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line.text, "computed "))
	name, typ, exprTail, err := splitTypedAssignment(rest)
	if err != nil {
		return ComputedDecl{}, p.err(line, err.Error())
	}
	p.pos++
	if strings.TrimSpace(exprTail) == "" {
		exprTail = p.collectExpressionBlock(line.indent+2, " ")
	}
	expr, err := ParseExpr(exprTail)
	if err != nil {
		return ComputedDecl{}, p.err(line, err.Error())
	}
	return ComputedDecl{Name: name, Type: typ, Expr: expr, Line: line.no}, nil
}

func (p *fileParser) parseFact(line sourceLine) (FactDecl, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line.text, "fact "))
	name, ok := strings.CutSuffix(rest, " when:")
	if !ok {
		return FactDecl{}, p.err(line, "fact requires when block")
	}
	p.pos++
	when, err := p.parseExprBlock(line.indent + 2)
	if err != nil {
		return FactDecl{}, err
	}
	decl := FactDecl{Name: strings.TrimSpace(name), When: when, Line: line.no}
	if !p.done() && p.peek().indent == line.indent && p.peek().text == "expose:" {
		p.pos++
		expose, err := p.parseBindings(line.indent + 2)
		if err != nil {
			return FactDecl{}, err
		}
		decl.Expose = expose
	}
	return decl, nil
}

func (p *fileParser) parsePolicy(line sourceLine) (PolicyDecl, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line.text, "policy "))
	if !strings.HasSuffix(rest, ":") {
		return PolicyDecl{}, p.err(line, "policy requires a block")
	}
	decl := PolicyDecl{
		Name:    strings.TrimSpace(strings.TrimSuffix(rest, ":")),
		Entries: map[string]*Expr{},
		Catches: map[string]string{},
		Line:    line.no,
	}
	p.pos++
	for !p.done() && p.peek().indent > line.indent {
		current := p.peek()
		if current.indent != line.indent+2 {
			return PolicyDecl{}, p.err(current, "invalid policy indentation")
		}
		if current.text == "catch:" {
			p.pos++
			for !p.done() && p.peek().indent == current.indent+2 {
				catchLine := p.peek()
				from, to, ok := strings.Cut(catchLine.text, "->")
				if !ok {
					return PolicyDecl{}, p.err(catchLine, "invalid catch mapping")
				}
				decl.Catches[strings.TrimSpace(from)] = strings.TrimSpace(to)
				p.pos++
			}
			continue
		}
		key, value, ok := strings.Cut(current.text, ":")
		if !ok {
			return PolicyDecl{}, p.err(current, "invalid policy entry")
		}
		expr, err := ParseExpr(strings.TrimSpace(value))
		if err != nil {
			return PolicyDecl{}, p.err(current, err.Error())
		}
		decl.Entries[strings.TrimSpace(key)] = expr
		p.pos++
	}
	return decl, nil
}

func (p *fileParser) parseActivity(line sourceLine) (ActivityDecl, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line.text, "activity "))
	if !strings.HasSuffix(rest, ":") {
		return ActivityDecl{}, p.err(line, "activity requires a block")
	}
	decl := ActivityDecl{Name: strings.TrimSpace(strings.TrimSuffix(rest, ":")), Line: line.no}
	p.pos++
	for !p.done() && p.peek().indent > line.indent {
		current := p.peek()
		if current.indent != line.indent+2 {
			return ActivityDecl{}, p.err(current, "invalid activity indentation")
		}
		switch current.text {
		case "require:":
			p.pos++
			values, err := p.parseExprBlock(current.indent + 2)
			if err != nil {
				return ActivityDecl{}, err
			}
			decl.Require = values
		case "input:":
			p.pos++
			values, err := p.parseBindings(current.indent + 2)
			if err != nil {
				return ActivityDecl{}, err
			}
			decl.Input = values
		case "output:":
			p.pos++
			values, err := p.parseFields(current.indent + 2)
			if err != nil {
				return ActivityDecl{}, err
			}
			decl.Output = values
		default:
			key, value, ok := strings.Cut(current.text, ":")
			if !ok {
				return ActivityDecl{}, p.err(current, "invalid activity field")
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			switch key {
			case "effect":
				decl.Effect = value
			case "idempotencyKey":
				expr, err := ParseExpr(value)
				if err != nil {
					return ActivityDecl{}, p.err(current, err.Error())
				}
				decl.IdempotencyKey = expr
			case "policy":
				decl.Policy = value
			default:
				return ActivityDecl{}, p.err(current, "unknown activity field")
			}
			p.pos++
		}
	}
	return decl, nil
}

func (p *fileParser) parseRule(line sourceLine) (RuleDecl, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line.text, "rule "))
	if !strings.HasSuffix(rest, ":") {
		return RuleDecl{}, p.err(line, "rule requires a block")
	}
	decl := RuleDecl{Name: strings.TrimSpace(strings.TrimSuffix(rest, ":")), Line: line.no}
	p.pos++
	for !p.done() && p.peek().indent > line.indent {
		current := p.peek()
		if current.indent != line.indent+2 {
			return RuleDecl{}, p.err(current, "invalid rule indentation")
		}
		switch {
		case current.text == "on:":
			p.pos++
			for !p.done() && p.peek().indent == current.indent+2 {
				trigger, err := parseTrigger(p.peek().text)
				if err != nil {
					return RuleDecl{}, p.err(p.peek(), err.Error())
				}
				decl.Triggers = append(decl.Triggers, trigger)
				p.pos++
			}
		case strings.HasPrefix(current.text, "on "):
			trigger, err := parseTrigger(strings.TrimSpace(strings.TrimPrefix(current.text, "on ")))
			if err != nil {
				return RuleDecl{}, p.err(current, err.Error())
			}
			decl.Triggers = append(decl.Triggers, trigger)
			p.pos++
		case current.text == "when:":
			p.pos++
			values, err := p.parseExprBlock(current.indent + 2)
			if err != nil {
				return RuleDecl{}, err
			}
			decl.When = values
		case current.text == "require:":
			p.pos++
			values, err := p.parseExprBlock(current.indent + 2)
			if err != nil {
				return RuleDecl{}, err
			}
			decl.Require = values
		case strings.HasPrefix(current.text, "run:"):
			decl.Run = strings.TrimSpace(strings.TrimPrefix(current.text, "run:"))
			p.pos++
		case current.text == "write:":
			p.pos++
			values, err := p.parseBindings(current.indent + 2)
			if err != nil {
				return RuleDecl{}, err
			}
			decl.Writes = values
		default:
			return RuleDecl{}, p.err(current, "unknown rule field")
		}
	}
	return decl, nil
}

func (p *fileParser) parseClaim(line sourceLine) (ClaimDecl, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line.text, "claim "))
	if !strings.HasSuffix(rest, ":") {
		return ClaimDecl{}, p.err(line, "claim requires a block")
	}
	decl := ClaimDecl{Name: strings.TrimSpace(strings.TrimSuffix(rest, ":")), Line: line.no}
	p.pos++
	if p.done() || p.peek().text != "always:" {
		return ClaimDecl{}, p.err(line, "claim requires always block")
	}
	current := p.peek()
	p.pos++
	values, err := p.parseExprBlock(current.indent + 2)
	if err != nil {
		return ClaimDecl{}, err
	}
	decl.Always = values
	return decl, nil
}

func (p *fileParser) parseQuery(line sourceLine) (QueryDecl, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line.text, "query "))
	if !strings.HasSuffix(rest, ":") {
		return QueryDecl{}, p.err(line, "query requires a block")
	}
	decl := QueryDecl{Name: strings.TrimSpace(strings.TrimSuffix(rest, ":")), Line: line.no}
	p.pos++
	if p.done() || p.peek().text != "return:" {
		return QueryDecl{}, p.err(line, "query requires return block")
	}
	current := p.peek()
	p.pos++
	values, err := p.parseBindings(current.indent + 2)
	if err != nil {
		return QueryDecl{}, err
	}
	decl.Return = values
	return decl, nil
}

func (p *fileParser) parseFields(indent int) ([]FieldDecl, error) {
	var fields []FieldDecl
	for !p.done() && p.peek().indent == indent {
		line := p.peek()
		name, typ, exprTail, err := splitTypedAssignment(line.text)
		if err != nil {
			name, typ, err = splitTypedField(line.text)
			if err != nil {
				return nil, p.err(line, err.Error())
			}
			fields = append(fields, FieldDecl{Name: name, Type: typ, Line: line.no})
			p.pos++
			continue
		}
		expr, err := ParseExpr(exprTail)
		if err != nil {
			return nil, p.err(line, err.Error())
		}
		fields = append(fields, FieldDecl{Name: name, Type: typ, Default: expr, HasDefault: true, Line: line.no})
		p.pos++
	}
	return fields, nil
}

func (p *fileParser) parseBindings(indent int) ([]Binding, error) {
	var bindings []Binding
	for !p.done() && p.peek().indent == indent {
		line := p.peek()
		name, value, ok := strings.Cut(line.text, "=")
		if !ok {
			return nil, p.err(line, "expected binding")
		}
		expr, err := ParseExpr(strings.TrimSpace(value))
		if err != nil {
			return nil, p.err(line, err.Error())
		}
		bindings = append(bindings, Binding{Name: strings.TrimSpace(name), Expr: expr, Line: line.no})
		p.pos++
	}
	return bindings, nil
}

func (p *fileParser) parseExprBlock(indent int) ([]*Expr, error) {
	var exprs []*Expr
	for !p.done() && p.peek().indent == indent {
		line := p.peek()
		expr, err := ParseExpr(line.text)
		if err != nil {
			return nil, p.err(line, err.Error())
		}
		exprs = append(exprs, expr)
		p.pos++
	}
	return exprs, nil
}

func (p *fileParser) collectExpressionBlock(indent int, sep string) string {
	var parts []string
	for !p.done() && p.peek().indent == indent {
		parts = append(parts, p.peek().text)
		p.pos++
	}
	return strings.Join(parts, sep)
}

func parseTrigger(raw string) (Trigger, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "changed(") && strings.HasSuffix(raw, ")") {
		return Trigger{Kind: TriggerChanged, Target: strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "changed("), ")")), Raw: raw}, nil
	}
	if strings.HasPrefix(raw, "timer(") && strings.HasSuffix(raw, ")") {
		return Trigger{Kind: TriggerTimer, Raw: raw, Target: strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "timer("), ")"))}, nil
	}
	if raw == "" {
		return Trigger{}, fmt.Errorf("empty trigger")
	}
	return Trigger{Kind: TriggerSignal, Name: raw, Raw: raw}, nil
}

func splitTypedAssignment(text string) (string, string, string, error) {
	left, right, ok := strings.Cut(text, "=")
	if !ok {
		return "", "", "", fmt.Errorf("expected assignment")
	}
	name, typ, err := splitTypedField(strings.TrimSpace(left))
	if err != nil {
		return "", "", "", err
	}
	return name, typ, strings.TrimSpace(right), nil
}

func splitTypedField(text string) (string, string, error) {
	name, typ, ok := strings.Cut(text, ":")
	if !ok {
		return "", "", fmt.Errorf("expected typed field")
	}
	name = strings.TrimSpace(name)
	typ = strings.TrimSpace(typ)
	if name == "" || typ == "" {
		return "", "", fmt.Errorf("invalid typed field")
	}
	return name, typ, nil
}

func (p *fileParser) done() bool {
	return p.pos >= len(p.lines)
}

func (p *fileParser) peek() sourceLine {
	return p.lines[p.pos]
}

func (p *fileParser) err(line sourceLine, msg string) error {
	return &ParseError{Line: line.no, Msg: msg}
}
