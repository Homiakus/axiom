// Package model provides a file-free declarative Go builder that
// compiles into the same canonical Plan as AXM and TOML.
package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Homiakus/axiom"
)

type Expr struct{ text string }

func (e Expr) String() string { return e.text }
func Raw(value string) Expr   { return Expr{text: strings.TrimSpace(value)} }
func Ref(name string) Expr    { return Raw(name) }
func Lit(value any) Expr {
	if duration, ok := value.(time.Duration); ok {
		return Raw(duration.String())
	}
	data, _ := json.Marshal(value)
	return Raw(string(data))
}
func Eq(a, b Expr) Expr                     { return binary(a, "==", b) }
func Ne(a, b Expr) Expr                     { return binary(a, "!=", b) }
func GT(a, b Expr) Expr                     { return binary(a, ">", b) }
func GTE(a, b Expr) Expr                    { return binary(a, ">=", b) }
func LT(a, b Expr) Expr                     { return binary(a, "<", b) }
func LTE(a, b Expr) Expr                    { return binary(a, "<=", b) }
func And(values ...Expr) Expr               { return join("and", values) }
func Or(values ...Expr) Expr                { return join("or", values) }
func Not(value Expr) Expr                   { return Raw("not (" + value.text + ")") }
func Exists(value Expr) Expr                { return Raw(value.text + " exists") }
func Implies(a, b Expr) Expr                { return binary(a, "implies", b) }
func binary(a Expr, op string, b Expr) Expr { return Raw("(" + a.text + " " + op + " " + b.text + ")") }
func join(op string, values []Expr) Expr {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value.text != "" {
			parts = append(parts, "("+value.text+")")
		}
	}
	return Raw(strings.Join(parts, " "+op+" "))
}

type Trigger struct{ text string }

func OnSignal(name string) Trigger      { return Trigger{text: name} }
func OnChanged(field Expr) Trigger      { return Trigger{text: "changed(" + field.text + ")"} }
func OnTimer(expression string) Trigger { return Trigger{text: "timer(" + expression + ")"} }

type fieldDecl struct {
	goName, name, typ string
	defaultValue      *Expr
}
type schemaDecl struct {
	name   string
	fields []fieldDecl
}
type computedDecl struct {
	name, typ string
	expr      Expr
}
type factDecl struct {
	name   string
	when   []Expr
	expose map[string]Expr
}
type policyDecl struct {
	name    string
	entries map[string]Expr
}
type activityDecl struct {
	name           string
	require        []Expr
	input          map[string]Expr
	output         map[string]string
	effect, policy string
	idempotency    *Expr
}
type ruleDecl struct {
	name          string
	triggers      []Trigger
	when, require []Expr
	run           string
	writes        map[string]Expr
}
type claimDecl struct {
	name        string
	expressions []Expr
}
type queryDecl struct {
	name   string
	values map[string]Expr
}

type Definition struct {
	name, version  string
	events, states []schemaDecl
	computeds      []computedDecl
	facts          []factDecl
	policies       []policyDecl
	activities     []activityDecl
	rules          []ruleDecl
	claims         []claimDecl
	queries        []queryDecl
}

func New(name string) *Definition                        { return &Definition{name: name, version: "1"} }
func (d *Definition) Version(version string) *Definition { d.version = version; return d }

type StateRef[T any] struct {
	definition *Definition
	index      int
}
type EventRef[T any] struct {
	definition *Definition
	index      int
}

func State[T any](d *Definition, name string) *StateRef[T] {
	declaration := schemaFromType(reflect.TypeFor[T](), name)
	d.states = append(d.states, declaration)
	return &StateRef[T]{definition: d, index: len(d.states) - 1}
}

func Event[T any](d *Definition, name string) *EventRef[T] {
	declaration := schemaFromType(reflect.TypeFor[T](), name)
	d.events = append(d.events, declaration)
	return &EventRef[T]{definition: d, index: len(d.events) - 1}
}

func (s *StateRef[T]) Field(name string) Expr {
	return Ref(s.definition.states[s.index].name + "." + fieldName(s.definition.states[s.index], name))
}
func (s *StateRef[T]) Changed(name string) Trigger { return OnChanged(s.Field(name)) }
func (s *StateRef[T]) Default(name string, value any) *StateRef[T] {
	declaration := &s.definition.states[s.index]
	field := fieldIndex(*declaration, name)
	expression := Lit(value)
	declaration.fields[field].defaultValue = &expression
	return s
}
func (e *EventRef[T]) Field(name string) Expr {
	return Ref("signal." + fieldName(e.definition.events[e.index], name))
}
func (e *EventRef[T]) Trigger() Trigger { return OnSignal(e.definition.events[e.index].name) }

func (d *Definition) Computed(name, typ string, expression Expr) *Definition {
	d.computeds = append(d.computeds, computedDecl{name: name, typ: typ, expr: expression})
	return d
}
func (d *Definition) Fact(name string, when []Expr, expose map[string]Expr) *Definition {
	d.facts = append(d.facts, factDecl{name: name, when: when, expose: expose})
	return d
}

type PolicyBuilder struct {
	definition *Definition
	index      int
}

func (d *Definition) Policy(name string) *PolicyBuilder {
	d.policies = append(d.policies, policyDecl{name: name, entries: map[string]Expr{}})
	return &PolicyBuilder{definition: d, index: len(d.policies) - 1}
}
func (p *PolicyBuilder) Retry(value int) *PolicyBuilder { p.entry("retry", Lit(value)); return p }
func (p *PolicyBuilder) Timeout(value time.Duration) *PolicyBuilder {
	p.entry("timeout", Lit(value))
	return p
}
func (p *PolicyBuilder) Concurrency(value string) *PolicyBuilder {
	p.entry("concurrency", Raw(value))
	return p
}
func (p *PolicyBuilder) Idempotency(value string) *PolicyBuilder {
	p.entry("idempotency", Raw(value))
	return p
}
func (p *PolicyBuilder) entry(name string, value Expr) {
	p.definition.policies[p.index].entries[name] = value
}

type ActivityBuilder struct {
	definition *Definition
	index      int
}

func (d *Definition) Activity(name string) *ActivityBuilder {
	d.activities = append(d.activities, activityDecl{name: name, input: map[string]Expr{}, output: map[string]string{}, effect: "none"})
	return &ActivityBuilder{definition: d, index: len(d.activities) - 1}
}
func (a *ActivityBuilder) Require(values ...Expr) *ActivityBuilder {
	a.definition.activities[a.index].require = append(a.definition.activities[a.index].require, values...)
	return a
}
func (a *ActivityBuilder) Input(name string, expression Expr) *ActivityBuilder {
	a.definition.activities[a.index].input[name] = expression
	return a
}
func (a *ActivityBuilder) Output(name, typ string) *ActivityBuilder {
	a.definition.activities[a.index].output[name] = typ
	return a
}
func (a *ActivityBuilder) Effect(value string) *ActivityBuilder {
	a.definition.activities[a.index].effect = value
	return a
}
func (a *ActivityBuilder) Policy(name string) *ActivityBuilder {
	a.definition.activities[a.index].policy = name
	return a
}
func (a *ActivityBuilder) IdempotencyKey(value Expr) *ActivityBuilder {
	a.definition.activities[a.index].idempotency = &value
	return a
}

type RuleBuilder struct {
	definition *Definition
	index      int
}

func (d *Definition) Rule(name string) *RuleBuilder {
	d.rules = append(d.rules, ruleDecl{name: name, writes: map[string]Expr{}})
	return &RuleBuilder{definition: d, index: len(d.rules) - 1}
}
func (r *RuleBuilder) On(values ...Trigger) *RuleBuilder {
	r.definition.rules[r.index].triggers = append(r.definition.rules[r.index].triggers, values...)
	return r
}
func (r *RuleBuilder) When(values ...Expr) *RuleBuilder {
	r.definition.rules[r.index].when = append(r.definition.rules[r.index].when, values...)
	return r
}
func (r *RuleBuilder) Require(values ...Expr) *RuleBuilder {
	r.definition.rules[r.index].require = append(r.definition.rules[r.index].require, values...)
	return r
}
func (r *RuleBuilder) Run(activity string) *RuleBuilder {
	r.definition.rules[r.index].run = activity
	return r
}
func (r *RuleBuilder) Set(target, value Expr) *RuleBuilder {
	r.definition.rules[r.index].writes[target.text] = value
	return r
}

func (d *Definition) Claim(name string, expressions ...Expr) *Definition {
	d.claims = append(d.claims, claimDecl{name: name, expressions: expressions})
	return d
}
func (d *Definition) Query(name string, values map[string]Expr) *Definition {
	d.queries = append(d.queries, queryDecl{name: name, values: values})
	return d
}

func (d *Definition) CompilePlan() (*axiom.Plan, error) { return d.Compile() }
func (d *Definition) Compile() (*axiom.Plan, error) {
	source := []byte(d.Source())
	plan, err := axiom.CompilePlan(source, axiom.WithSourceName("go:model:"+d.name))
	if err != nil {
		return nil, err
	}
	plan.Format = "go:model"
	plan.Version = d.version
	return plan, nil
}

func (d *Definition) Source() string {
	var out strings.Builder
	fmt.Fprintf(&out, "domain %s\n\n", d.name)
	for _, value := range d.events {
		renderSchema(&out, "signal", value)
	}
	for _, value := range d.states {
		renderSchema(&out, "context", value)
	}
	for _, value := range d.computeds {
		fmt.Fprintf(&out, "computed %s: %s =\n  %s\n\n", value.name, value.typ, value.expr.text)
	}
	for _, value := range d.facts {
		fmt.Fprintf(&out, "fact %s when:\n", value.name)
		renderExprList(&out, value.when, 2)
		if len(value.expose) > 0 {
			out.WriteString("expose:\n")
			renderExprMap(&out, value.expose, 2)
		}
		out.WriteByte('\n')
	}
	for _, value := range d.policies {
		fmt.Fprintf(&out, "policy %s:\n", value.name)
		renderPolicyMap(&out, value.entries, 2)
		out.WriteByte('\n')
	}
	for _, value := range d.activities {
		fmt.Fprintf(&out, "activity %s:\n", value.name)
		if len(value.require) > 0 {
			out.WriteString("  require:\n")
			renderExprList(&out, value.require, 4)
		}
		if len(value.input) > 0 {
			out.WriteString("  input:\n")
			renderExprMap(&out, value.input, 4)
		}
		if len(value.output) > 0 {
			out.WriteString("  output:\n")
			renderTypeMap(&out, value.output, 4)
		}
		fmt.Fprintf(&out, "  effect: %s\n", value.effect)
		if value.idempotency != nil {
			fmt.Fprintf(&out, "  idempotencyKey: %s\n", value.idempotency.text)
		}
		if value.policy != "" {
			fmt.Fprintf(&out, "  policy: %s\n", value.policy)
		}
		out.WriteByte('\n')
	}
	for _, value := range d.rules {
		fmt.Fprintf(&out, "rule %s:\n", value.name)
		for _, trigger := range value.triggers {
			fmt.Fprintf(&out, "  on %s\n", trigger.text)
		}
		if len(value.when) > 0 {
			out.WriteString("  when:\n")
			renderExprList(&out, value.when, 4)
		}
		if len(value.require) > 0 {
			out.WriteString("  require:\n")
			renderExprList(&out, value.require, 4)
		}
		if value.run != "" {
			fmt.Fprintf(&out, "  run: %s\n", value.run)
		}
		if len(value.writes) > 0 {
			out.WriteString("  write:\n")
			renderExprMap(&out, value.writes, 4)
		}
		out.WriteByte('\n')
	}
	for _, value := range d.claims {
		fmt.Fprintf(&out, "claim %s:\n  always:\n", value.name)
		renderExprList(&out, value.expressions, 4)
		out.WriteByte('\n')
	}
	for _, value := range d.queries {
		fmt.Fprintf(&out, "query %s:\n  return:\n", value.name)
		renderExprMap(&out, value.values, 4)
		out.WriteByte('\n')
	}
	return out.String()
}

func schemaFromType(typ reflect.Type, name string) schemaDecl {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		panic("axiom/model: state and event types must be structs")
	}
	declaration := schemaDecl{name: name}
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		if !field.IsExported() {
			continue
		}
		serialized := field.Tag.Get("axiom")
		if serialized == "" {
			serialized = strings.Split(field.Tag.Get("json"), ",")[0]
		}
		if serialized == "" {
			serialized = lowerFirst(field.Name)
		}
		if serialized == "-" {
			continue
		}
		declaration.fields = append(declaration.fields, fieldDecl{goName: field.Name, name: serialized, typ: axiomType(field.Type)})
	}
	return declaration
}
func axiomType(typ reflect.Type) string {
	optional := false
	if typ.Kind() == reflect.Pointer {
		optional = true
		typ = typ.Elem()
	}
	var value string
	switch {
	case typ == reflect.TypeFor[time.Time]():
		value = "Time"
	case typ == reflect.TypeFor[time.Duration]():
		value = "Duration"
	default:
		switch typ.Kind() {
		case reflect.String:
			value = "String"
		case reflect.Bool:
			value = "Bool"
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			value = "Int"
		case reflect.Float32, reflect.Float64:
			value = "Float"
		case reflect.Slice, reflect.Array:
			value = "List<" + axiomType(typ.Elem()) + ">"
		case reflect.Map, reflect.Struct:
			value = "Object"
		default:
			value = "Any"
		}
	}
	if optional {
		value += "?"
	}
	return value
}
func fieldName(declaration schemaDecl, name string) string {
	return declaration.fields[fieldIndex(declaration, name)].name
}
func fieldIndex(declaration schemaDecl, name string) int {
	for index, field := range declaration.fields {
		if field.goName == name || field.name == name {
			return index
		}
	}
	panic("axiom/model: unknown field " + declaration.name + "." + name)
}
func lowerFirst(value string) string {
	if value == "" {
		return value
	}
	return strings.ToLower(value[:1]) + value[1:]
}
func renderSchema(out *strings.Builder, kind string, declaration schemaDecl) {
	fmt.Fprintf(out, "%s %s:\n", kind, declaration.name)
	for _, field := range declaration.fields {
		fmt.Fprintf(out, "  %s: %s", field.name, field.typ)
		if field.defaultValue != nil {
			fmt.Fprintf(out, " = %s", field.defaultValue.text)
		}
		out.WriteByte('\n')
	}
	out.WriteByte('\n')
}
func renderExprList(out *strings.Builder, values []Expr, indent int) {
	for _, value := range values {
		fmt.Fprintf(out, "%s%s\n", strings.Repeat(" ", indent), value.text)
	}
}
func renderExprMap(out *strings.Builder, values map[string]Expr, indent int) {
	keys := sortedKeys(values)
	for _, key := range keys {
		fmt.Fprintf(out, "%s%s = %s\n", strings.Repeat(" ", indent), key, values[key].text)
	}
}

// renderPolicyMap uses the canonical AXM policy syntax: key: value.
// Other expression maps use key = expression.
func renderPolicyMap(out *strings.Builder, values map[string]Expr, indent int) {
	keys := sortedKeys(values)
	for _, key := range keys {
		fmt.Fprintf(out, "%s%s: %s\n", strings.Repeat(" ", indent), key, values[key].text)
	}
}

func renderTypeMap(out *strings.Builder, values map[string]string, indent int) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(out, "%s%s: %s\n", strings.Repeat(" ", indent), key, values[key])
	}
}
func sortedKeys(values map[string]Expr) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
