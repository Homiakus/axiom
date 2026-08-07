package lang

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type ExprKind string

const (
	ExprLiteral ExprKind = "literal"
	ExprRef     ExprKind = "ref"
	ExprUnary   ExprKind = "unary"
	ExprBinary  ExprKind = "binary"
	ExprCall    ExprKind = "call"
)

type Expr struct {
	Kind  ExprKind
	Op    string
	Name  string
	Value any
	Args  []*Expr
	Left  *Expr
	Right *Expr
}

type DurationLiteral string

func ParseExpr(src string) (*Expr, error) {
	tokens, err := lexExpr(src)
	if err != nil {
		return nil, err
	}
	p := exprParser{tokens: tokens}
	expr, err := p.parse()
	if err != nil {
		return nil, err
	}
	if !p.atEnd() {
		return nil, fmt.Errorf("unexpected token %q", p.peek().value)
	}
	return expr, nil
}

func ExprRefs(expr *Expr) []string {
	seen := map[string]struct{}{}
	var refs []string
	var walk func(*Expr)
	walk = func(e *Expr) {
		if e == nil {
			return
		}
		if e.Kind == ExprRef {
			if _, ok := seen[e.Name]; !ok {
				seen[e.Name] = struct{}{}
				refs = append(refs, e.Name)
			}
		}
		for _, arg := range e.Args {
			walk(arg)
		}
		walk(e.Left)
		walk(e.Right)
	}
	walk(expr)
	return refs
}

func ExprString(expr *Expr) string {
	if expr == nil {
		return ""
	}
	switch expr.Kind {
	case ExprLiteral:
		return fmt.Sprint(expr.Value)
	case ExprRef:
		return expr.Name
	case ExprUnary:
		return expr.Op + "(" + ExprString(expr.Left) + ")"
	case ExprBinary:
		return "(" + ExprString(expr.Left) + " " + expr.Op + " " + ExprString(expr.Right) + ")"
	case ExprCall:
		var args []string
		for _, arg := range expr.Args {
			args = append(args, ExprString(arg))
		}
		return expr.Name + "(" + strings.Join(args, ", ") + ")"
	default:
		return ""
	}
}

type exprToken struct {
	kind  string
	value string
}

func lexExpr(src string) ([]exprToken, error) {
	var tokens []exprToken
	for i := 0; i < len(src); {
		r := rune(src[i])
		if unicode.IsSpace(r) {
			i++
			continue
		}
		if src[i] == '"' {
			start := i
			i++
			escaped := false
			for i < len(src) {
				if escaped {
					escaped = false
					i++
					continue
				}
				if src[i] == '\\' {
					escaped = true
					i++
					continue
				}
				if src[i] == '"' {
					i++
					break
				}
				i++
			}
			if i > len(src) || src[i-1] != '"' {
				return nil, fmt.Errorf("unterminated string literal")
			}
			tokens = append(tokens, exprToken{kind: "string", value: src[start:i]})
			continue
		}
		if isDigit(src[i]) {
			start := i
			for i < len(src) && isDigit(src[i]) {
				i++
			}
			if i < len(src) && src[i] == '.' {
				i++
				for i < len(src) && isDigit(src[i]) {
					i++
				}
			}
			if i < len(src) && isAlpha(src[i]) {
				for i < len(src) && isAlpha(src[i]) {
					i++
				}
				tokens = append(tokens, exprToken{kind: "duration", value: src[start:i]})
				continue
			}
			tokens = append(tokens, exprToken{kind: "number", value: src[start:i]})
			continue
		}
		if isIdentStart(src[i]) {
			start := i
			for i < len(src) && isIdentPart(src[i]) {
				i++
			}
			tokens = append(tokens, exprToken{kind: "ident", value: src[start:i]})
			continue
		}
		if i+1 < len(src) {
			two := src[i : i+2]
			if two == "==" || two == "!=" || two == ">=" || two == "<=" || two == "->" {
				tokens = append(tokens, exprToken{kind: "op", value: two})
				i += 2
				continue
			}
		}
		switch src[i] {
		case '(', ')', '[', ']', '{', '}', ',', ':', '+', '-', '*', '/', '%', '>', '<':
			tokens = append(tokens, exprToken{kind: string(src[i]), value: string(src[i])})
			i++
		default:
			return nil, fmt.Errorf("unexpected character %q", src[i])
		}
	}
	return tokens, nil
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentStart(b byte) bool {
	return isAlpha(b) || b == '_'
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || isDigit(b) || b == '.'
}

type exprParser struct {
	tokens []exprToken
	pos    int
}

func (p *exprParser) parse() (*Expr, error) {
	if len(p.tokens) == 0 {
		return nil, fmt.Errorf("empty expression")
	}
	return p.parseImplies()
}

func (p *exprParser) parseImplies() (*Expr, error) {
	left, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.matchIdent("implies") {
		right, err := p.parseImplies()
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: ExprBinary, Op: "implies", Left: left, Right: right}, nil
	}
	return left, nil
}

func (p *exprParser) parseOr() (*Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.matchIdent("or") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Expr{Kind: ExprBinary, Op: "or", Left: left, Right: right}
	}
	return left, nil
}

func (p *exprParser) parseAnd() (*Expr, error) {
	left, err := p.parseCompare()
	if err != nil {
		return nil, err
	}
	for p.matchIdent("and") {
		right, err := p.parseCompare()
		if err != nil {
			return nil, err
		}
		left = &Expr{Kind: ExprBinary, Op: "and", Left: left, Right: right}
	}
	return left, nil
}

func (p *exprParser) parseCompare() (*Expr, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	if p.matchValue("==") || p.matchValue("!=") || p.matchValue(">") || p.matchValue(">=") || p.matchValue("<") || p.matchValue("<=") || p.matchIdent("in") {
		op := p.previous().value
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: ExprBinary, Op: op, Left: left, Right: right}, nil
	}
	return left, nil
}

func (p *exprParser) parseAdd() (*Expr, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.matchValue("+") || p.matchValue("-") {
		op := p.previous().value
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = &Expr{Kind: ExprBinary, Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *exprParser) parseMul() (*Expr, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.matchValue("*") || p.matchValue("/") || p.matchValue("%") {
		op := p.previous().value
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		left = &Expr{Kind: ExprBinary, Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *exprParser) parsePrimary() (*Expr, error) {
	if p.atEnd() {
		return nil, fmt.Errorf("expected expression")
	}
	tok := p.advance()
	switch tok.kind {
	case "-":
		expr, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		if expr.Kind == ExprLiteral {
			switch v := expr.Value.(type) {
			case int:
				return &Expr{Kind: ExprLiteral, Value: -v}, nil
			case float64:
				return &Expr{Kind: ExprLiteral, Value: -v}, nil
			}
		}
		return &Expr{Kind: ExprUnary, Op: "-", Left: expr}, nil
	case "string":
		value, err := strconv.Unquote(tok.value)
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: ExprLiteral, Value: value}, nil
	case "duration":
		return &Expr{Kind: ExprLiteral, Value: DurationLiteral(tok.value)}, nil
	case "number":
		if strings.Contains(tok.value, ".") {
			v, err := strconv.ParseFloat(tok.value, 64)
			if err != nil {
				return nil, err
			}
			return &Expr{Kind: ExprLiteral, Value: v}, nil
		}
		v, err := strconv.Atoi(tok.value)
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: ExprLiteral, Value: v}, nil
	case "ident":
		switch tok.value {
		case "not":
			expr, err := p.parsePrimary()
			if err != nil {
				return nil, err
			}
			return &Expr{Kind: ExprUnary, Op: "not", Left: expr}, nil
		case "true":
			return &Expr{Kind: ExprLiteral, Value: true}, nil
		case "false":
			return &Expr{Kind: ExprLiteral, Value: false}, nil
		case "null":
			return &Expr{Kind: ExprLiteral, Value: nil}, nil
		case "latest", "first", "once", "parallel", "required", "optional", "none":
			return &Expr{Kind: ExprLiteral, Value: tok.value}, nil
		}
		if p.matchValue("(") {
			if tok.value == "timer" {
				raw, err := p.collectUntilMatchingParen()
				if err != nil {
					return nil, err
				}
				return &Expr{Kind: ExprCall, Name: "timer", Value: raw}, nil
			}
			var args []*Expr
			if !p.checkValue(")") {
				for {
					arg, err := p.parseImplies()
					if err != nil {
						return nil, err
					}
					args = append(args, arg)
					if !p.matchValue(",") {
						break
					}
				}
			}
			if !p.matchValue(")") {
				return nil, fmt.Errorf("expected )")
			}
			return &Expr{Kind: ExprCall, Name: tok.value, Args: args}, nil
		}
		ref := &Expr{Kind: ExprRef, Name: tok.value}
		if p.matchIdent("exists") {
			return &Expr{Kind: ExprUnary, Op: "exists", Left: ref}, nil
		}
		return ref, nil
	case "(":
		expr, err := p.parseImplies()
		if err != nil {
			return nil, err
		}
		if !p.matchValue(")") {
			return nil, fmt.Errorf("expected )")
		}
		return expr, nil
	case "[":
		var values []*Expr
		if !p.checkValue("]") {
			for {
				value, err := p.parseImplies()
				if err != nil {
					return nil, err
				}
				values = append(values, value)
				if !p.matchValue(",") {
					break
				}
			}
		}
		if !p.matchValue("]") {
			return nil, fmt.Errorf("expected ]")
		}
		return &Expr{Kind: ExprCall, Name: "list", Args: values}, nil
	case "{":
		var values []*Expr
		for !p.checkValue("}") {
			if p.atEnd() {
				return nil, fmt.Errorf("expected }")
			}
			key := p.advance()
			if key.kind != "ident" {
				return nil, fmt.Errorf("expected map key")
			}
			if !p.matchValue(":") {
				return nil, fmt.Errorf("expected :")
			}
			value, err := p.parseImplies()
			if err != nil {
				return nil, err
			}
			values = append(values, &Expr{Kind: ExprLiteral, Value: key.value}, value)
			if !p.matchValue(",") {
				break
			}
		}
		if !p.matchValue("}") {
			return nil, fmt.Errorf("expected }")
		}
		return &Expr{Kind: ExprCall, Name: "map", Args: values}, nil
	default:
		return nil, fmt.Errorf("unexpected token %q", tok.value)
	}
}

func (p *exprParser) collectUntilMatchingParen() (string, error) {
	depth := 1
	start := p.pos
	for p.pos < len(p.tokens) {
		tok := p.advance()
		switch tok.value {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				var parts []string
				for _, token := range p.tokens[start : p.pos-1] {
					parts = append(parts, token.value)
				}
				return strings.Join(parts, " "), nil
			}
		}
	}
	return "", fmt.Errorf("expected )")
}

func (p *exprParser) atEnd() bool {
	return p.pos >= len(p.tokens)
}

func (p *exprParser) peek() exprToken {
	return p.tokens[p.pos]
}

func (p *exprParser) previous() exprToken {
	return p.tokens[p.pos-1]
}

func (p *exprParser) advance() exprToken {
	tok := p.tokens[p.pos]
	p.pos++
	return tok
}

func (p *exprParser) matchIdent(value string) bool {
	if p.atEnd() || p.peek().kind != "ident" || p.peek().value != value {
		return false
	}
	p.pos++
	return true
}

func (p *exprParser) matchValue(value string) bool {
	if p.atEnd() || p.peek().value != value {
		return false
	}
	p.pos++
	return true
}

func (p *exprParser) checkValue(value string) bool {
	return !p.atEnd() && p.peek().value == value
}
