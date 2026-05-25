package lang

import "fmt"

type Module struct {
	Domain     string
	Imports    []ImportDecl
	Signals    []SignalDecl
	Contexts   []ContextDecl
	Computeds  []ComputedDecl
	Facts      []FactDecl
	Policies   []PolicyDecl
	Activities []ActivityDecl
	Rules      []RuleDecl
	Claims     []ClaimDecl
	Queries    []QueryDecl
}

type ImportDecl struct {
	Name  string
	Alias string
}

type FieldDecl struct {
	Name       string
	Type       string
	Default    *Expr
	HasDefault bool
	Line       int
}

type Binding struct {
	Name string
	Expr *Expr
	Line int
}

type SignalDecl struct {
	Name   string
	Fields []FieldDecl
	Line   int
}

type ContextDecl struct {
	Name   string
	Fields []FieldDecl
	Line   int
}

type ComputedDecl struct {
	Name string
	Type string
	Expr *Expr
	Line int
}

type FactDecl struct {
	Name   string
	When   []*Expr
	Expose []Binding
	Line   int
}

type PolicyDecl struct {
	Name    string
	Entries map[string]*Expr
	Catches map[string]string
	Line    int
}

type ActivityDecl struct {
	Name           string
	Require        []*Expr
	Input          []Binding
	Output         []FieldDecl
	Effect         string
	IdempotencyKey *Expr
	Policy         string
	Line           int
}

type TriggerKind string

const (
	TriggerSignal  TriggerKind = "signal"
	TriggerChanged TriggerKind = "changed"
	TriggerTimer   TriggerKind = "timer"
)

type Trigger struct {
	Kind   TriggerKind
	Name   string
	Target string
	Raw    string
}

type RuleDecl struct {
	Name     string
	Triggers []Trigger
	When     []*Expr
	Require  []*Expr
	Run      string
	Writes   []Binding
	Line     int
}

type ClaimDecl struct {
	Name   string
	Always []*Expr
	Line   int
}

type QueryDecl struct {
	Name   string
	Return []Binding
	Line   int
}

type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string {
	if e.Line <= 0 {
		return e.Msg
	}
	return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
}
