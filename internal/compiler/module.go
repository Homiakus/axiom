package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Homiakus/axiom/internal/diag"
	"github.com/Homiakus/axiom/internal/lang"
)

const (
	DSLVersion      = "axm/v1"
	CompilerVersion = "axiom-compiler/v2"
	PlanVersion     = "fast-plan/v2"
)

type FieldID uint32
type SignalID uint32
type RuleID uint32
type ActivityID uint32

type IDTables struct {
	Fields      []string
	FieldIDs    map[string]FieldID
	Signals     []string
	SignalIDs   map[string]SignalID
	Rules       []string
	RuleIDs     map[string]RuleID
	Activities  []string
	ActivityIDs map[string]ActivityID
}

type Module struct {
	AST             *lang.Module
	Domain          string
	SourceHash      string
	CompiledHash    string
	DSLVersion      string
	CompilerVersion string
	PlanVersion     string
	Signals         map[string]lang.SignalDecl
	Contexts        map[string]lang.ContextDecl
	Computeds       map[string]lang.ComputedDecl
	Facts           map[string]lang.FactDecl
	Policies        map[string]lang.PolicyDecl
	Activities      map[string]lang.ActivityDecl
	Rules           map[string]lang.RuleDecl
	Claims          map[string]lang.ClaimDecl
	Queries         map[string]lang.QueryDecl
	Indexes         DependencyIndexes
	Symbols         map[string]Symbol
	IDs             IDTables
}

type SymbolKind string

const (
	SymbolSignal       SymbolKind = "signal"
	SymbolContext      SymbolKind = "context"
	SymbolField        SymbolKind = "field"
	SymbolComputed     SymbolKind = "computed"
	SymbolFact         SymbolKind = "fact"
	SymbolExposedField SymbolKind = "exposedField"
	SymbolPolicy       SymbolKind = "policy"
	SymbolActivity     SymbolKind = "activity"
	SymbolRule         SymbolKind = "rule"
	SymbolClaim        SymbolKind = "claim"
	SymbolQuery        SymbolKind = "query"
)

type Symbol struct {
	Name  string
	Kind  SymbolKind
	Type  string
	Owner string
}

type NodeID struct {
	Kind string
	Name string
}

type DependencyIndexes struct {
	ContextFieldIndex       map[string][]NodeID
	ComputedDependencyIndex map[string][]NodeID
	FactDependencyIndex     map[string][]NodeID
	SignalIndex             map[string][]string
	ChangedIndex            map[string][]string
	TimerIndex              map[string][]string
	WriteTargetIndex        map[string][]string
	PolicyIndex             map[string][]string
	ClaimIndex              map[string][]string
}

type Diagnostic = diag.Error
type Diagnostics = diag.Errors

func Compile(source []byte) (*Module, error) {
	ast, err := lang.Parse(source)
	if err != nil {
		if parseErr, ok := err.(*lang.ParseError); ok {
			return nil, Diagnostics{{
				Code:    "AX000",
				Message: parseErr.Msg,
				Line:    parseErr.Line,
				Kind:    "parse",
				Cause:   err,
			}}
		}
		return nil, Diagnostics{{Code: "AX000", Message: err.Error(), Kind: "parse", Cause: err}}
	}
	module, err := CompileAST(ast)
	if err != nil {
		return nil, err
	}
	module.SourceHash = hashBytes(source)
	module.CompiledHash = compiledHash(module)
	return module, nil
}

func CompileAST(ast *lang.Module) (*Module, error) {
	c := compiler{module: newModule(ast)}
	c.collectSymbols()
	c.validate()
	c.buildIndexes()
	c.buildIDs()
	if len(c.diags) > 0 {
		return nil, c.diags
	}
	c.module.CompiledHash = compiledHash(c.module)
	return c.module, nil
}

func newModule(ast *lang.Module) *Module {
	return &Module{
		AST:             ast,
		Domain:          ast.Domain,
		DSLVersion:      DSLVersion,
		CompilerVersion: CompilerVersion,
		PlanVersion:     PlanVersion,
		Signals:         map[string]lang.SignalDecl{},
		Contexts:        map[string]lang.ContextDecl{},
		Computeds:       map[string]lang.ComputedDecl{},
		Facts:           map[string]lang.FactDecl{},
		Policies:        map[string]lang.PolicyDecl{},
		Activities:      map[string]lang.ActivityDecl{},
		Rules:           map[string]lang.RuleDecl{},
		Claims:          map[string]lang.ClaimDecl{},
		Queries:         map[string]lang.QueryDecl{},
		Indexes: DependencyIndexes{
			ContextFieldIndex:       map[string][]NodeID{},
			ComputedDependencyIndex: map[string][]NodeID{},
			FactDependencyIndex:     map[string][]NodeID{},
			SignalIndex:             map[string][]string{},
			ChangedIndex:            map[string][]string{},
			TimerIndex:              map[string][]string{},
			WriteTargetIndex:        map[string][]string{},
			PolicyIndex:             map[string][]string{},
			ClaimIndex:              map[string][]string{},
		},
		Symbols: map[string]Symbol{},
		IDs: IDTables{
			FieldIDs:    map[string]FieldID{},
			SignalIDs:   map[string]SignalID{},
			RuleIDs:     map[string]RuleID{},
			ActivityIDs: map[string]ActivityID{},
		},
	}
}

type compiler struct {
	module *Module
	diags  Diagnostics
}

func (c *compiler) collectSymbols() {
	for _, decl := range c.module.AST.Signals {
		c.addDecl(decl.Name, SymbolSignal, decl.Name, "", decl.Line, func() { c.module.Signals[decl.Name] = decl })
		for _, field := range decl.Fields {
			c.addSymbol(decl.Name+"."+field.Name, SymbolField, field.Type, decl.Name)
		}
	}
	for _, decl := range c.module.AST.Contexts {
		c.addDecl(decl.Name, SymbolContext, decl.Name, "", decl.Line, func() { c.module.Contexts[decl.Name] = decl })
		seen := map[string]struct{}{}
		for _, field := range decl.Fields {
			if _, ok := seen[field.Name]; ok {
				c.addAt("AX003", fmt.Sprintf("duplicate field: %s.%s", decl.Name, field.Name), "compile", decl.Name+"."+field.Name, field.Line, "Rename one of the duplicate fields.")
			}
			seen[field.Name] = struct{}{}
			c.addSymbol(decl.Name+"."+field.Name, SymbolField, field.Type, decl.Name)
		}
	}
	for _, decl := range c.module.AST.Computeds {
		c.addDecl(decl.Name, SymbolComputed, decl.Type, "", decl.Line, func() { c.module.Computeds[decl.Name] = decl })
	}
	for _, decl := range c.module.AST.Facts {
		c.addDecl(decl.Name, SymbolFact, "Bool", "", decl.Line, func() { c.module.Facts[decl.Name] = decl })
		seen := map[string]struct{}{}
		for _, expose := range decl.Expose {
			if _, ok := seen[expose.Name]; ok {
				c.addAt("AX003", fmt.Sprintf("duplicate exposed field: %s.%s", decl.Name, expose.Name), "compile", decl.Name+"."+expose.Name, expose.Line, "Rename one of the duplicate exposed fields.")
			}
			seen[expose.Name] = struct{}{}
			c.addSymbol(decl.Name+"."+expose.Name, SymbolExposedField, "Any", decl.Name)
		}
	}
	for _, decl := range c.module.AST.Policies {
		c.addDecl(decl.Name, SymbolPolicy, decl.Name, "", decl.Line, func() { c.module.Policies[decl.Name] = decl })
	}
	for _, decl := range c.module.AST.Activities {
		c.addDecl(decl.Name, SymbolActivity, decl.Name, "", decl.Line, func() { c.module.Activities[decl.Name] = decl })
	}
	for _, decl := range c.module.AST.Rules {
		c.addDecl(decl.Name, SymbolRule, decl.Name, "", decl.Line, func() { c.module.Rules[decl.Name] = decl })
	}
	for _, decl := range c.module.AST.Claims {
		c.addDecl(decl.Name, SymbolClaim, decl.Name, "", decl.Line, func() { c.module.Claims[decl.Name] = decl })
	}
	for _, decl := range c.module.AST.Queries {
		c.addDecl(decl.Name, SymbolQuery, decl.Name, "", decl.Line, func() { c.module.Queries[decl.Name] = decl })
	}
}

func (c *compiler) addDecl(name string, kind SymbolKind, typ string, owner string, line int, put func()) {
	if _, exists := c.module.Symbols[name]; exists {
		c.addAt("AX002", fmt.Sprintf("duplicate declaration: %s", name), "compile", name, line, "Use a unique declaration name.")
		return
	}
	c.addSymbol(name, kind, typ, owner)
	put()
}

func (c *compiler) addSymbol(name string, kind SymbolKind, typ string, owner string) {
	c.module.Symbols[name] = Symbol{Name: name, Kind: kind, Type: typ, Owner: owner}
}

func (c *compiler) validate() {
	c.validateExpressions()
	c.validateActivities()
	c.validateRules()
	c.validatePolicies()
	c.validateCycles()
}

type exprScope struct {
	allowSignal  bool
	outputFields map[string]struct{}
	allowRuntime bool
}

func (c *compiler) validateExpressions() {
	for _, decl := range c.module.Computeds {
		c.validateExpr(decl.Expr, exprScope{})
	}
	for _, decl := range c.module.Facts {
		c.validateExprs(decl.When, exprScope{})
		for _, binding := range decl.Expose {
			c.validateExpr(binding.Expr, exprScope{})
		}
	}
	for _, decl := range c.module.Activities {
		c.validateExprs(decl.Require, exprScope{})
		for _, binding := range decl.Input {
			c.validateExpr(binding.Expr, exprScope{allowSignal: true})
		}
		if decl.IdempotencyKey != nil {
			c.validateExpr(decl.IdempotencyKey, exprScope{allowSignal: true})
		}
	}
	for _, decl := range c.module.Claims {
		c.validateExprs(decl.Always, exprScope{})
	}
	for _, decl := range c.module.Queries {
		for _, binding := range decl.Return {
			c.validateExpr(binding.Expr, exprScope{allowRuntime: true})
		}
	}
}

func (c *compiler) validateActivities() {
	for _, activity := range c.module.Activities {
		if activity.Policy == "" {
			if activity.Effect == "external" || activity.Effect == "local" {
				c.addAt("AX304", fmt.Sprintf("activity %s requires policy", activity.Name), "compile", activity.Name, activity.Line, "Add policy: <policyName> to the activity.")
			}
			continue
		}
		policy, ok := c.module.Policies[activity.Policy]
		if !ok {
			c.addAt("AX001", fmt.Sprintf("unresolved reference: %s", activity.Policy), "compile", activity.Name, activity.Line, "Define the policy or update the activity policy name.")
			continue
		}
		if activity.Effect == "external" {
			idem, ok := policyString(policy, "idempotency")
			if !ok || idem != "required" || activity.IdempotencyKey == nil {
				c.addAt("AX305", fmt.Sprintf("idempotency key required but missing: %s", activity.Name), "compile", activity.Name, activity.Line, "Add idempotencyKey or use a policy that does not require it.")
			}
		}
	}
}

func (c *compiler) validateRules() {
	for _, rule := range c.module.Rules {
		if len(rule.Triggers) == 0 {
			c.addAt("AX001", fmt.Sprintf("rule has no triggers: %s", rule.Name), "compile", rule.Name, rule.Line, "Add an on trigger to the rule.")
		}
		allowSignal := false
		for _, trigger := range rule.Triggers {
			switch trigger.Kind {
			case lang.TriggerSignal:
				allowSignal = true
				if _, ok := c.module.Signals[trigger.Name]; !ok {
					c.addAt("AX001", fmt.Sprintf("unresolved reference: %s", trigger.Name), "compile", rule.Name, rule.Line, "Define the signal or update the trigger name.")
				}
			case lang.TriggerChanged:
				if !c.isContextField(trigger.Target) {
					c.addAt("AX001", fmt.Sprintf("unresolved reference: %s", trigger.Target), "compile", rule.Name, rule.Line, "Use changed(<Context.field>) with an existing context field.")
				}
			}
		}
		outputFields := map[string]struct{}{}
		if rule.Run != "" {
			activity, ok := c.module.Activities[rule.Run]
			if !ok {
				c.addAt("AX303", fmt.Sprintf("rule.run references non-activity: %s", rule.Run), "compile", rule.Name, rule.Line, "Define the activity or update run:.")
			} else {
				for _, field := range activity.Output {
					outputFields[field.Name] = struct{}{}
				}
			}
		}
		scope := exprScope{allowSignal: allowSignal, outputFields: outputFields}
		c.validateExprs(rule.When, scope)
		c.validateExprs(rule.Require, scope)
		for _, write := range rule.Writes {
			if !c.isContextField(write.Name) {
				c.addAt("AX301", fmt.Sprintf("invalid write target: %s", write.Name), "compile", rule.Name, write.Line, "Write only to existing context fields.")
			}
			c.validateExpr(write.Expr, scope)
		}
	}
}

func (c *compiler) validatePolicies() {
	for _, policy := range c.module.Policies {
		for _, target := range policy.Catches {
			if _, ok := c.module.Signals[target]; !ok {
				c.addAt("AX306", fmt.Sprintf("catch target signal does not exist: %s", target), "compile", policy.Name, policy.Line, "Define the target signal or update the catch mapping.")
			}
		}
	}
}

func (c *compiler) validateCycles() {
	if len(c.module.Computeds) == 0 && len(c.module.Facts) == 0 {
		return
	}
	if len(c.module.Computeds) > 0 && c.detectCycle("computed", c.computedGraph(), "AX201") {
		return
	}
	if len(c.module.Facts) > 0 && c.detectCycle("fact", c.factGraph(), "AX202") {
		return
	}
	if len(c.module.Computeds) > 0 && len(c.module.Facts) > 0 {
		c.detectCycle("cross-category", c.unifiedGraph(), "AX203")
	}
}

func (c *compiler) detectCycle(kind string, graph map[string][]string, code string) bool {
	const (
		unseen = 0
		active = 1
		done   = 2
	)
	state := map[string]int{}
	var visit func(string) bool
	visit = func(name string) bool {
		switch state[name] {
		case active:
			return true
		case done:
			return false
		}
		state[name] = active
		for _, next := range graph[name] {
			if visit(next) {
				return true
			}
		}
		state[name] = done
		return false
	}
	var names []string
	for name := range graph {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if visit(name) {
			c.add(code, fmt.Sprintf("cyclic %s dependency: %s", kind, name))
			return true
		}
	}
	return false
}

func (c *compiler) computedGraph() map[string][]string {
	graph := map[string][]string{}
	for _, decl := range c.module.Computeds {
		for _, ref := range lang.ExprRefs(decl.Expr) {
			if _, ok := c.module.Computeds[ref]; ok {
				graph[decl.Name] = append(graph[decl.Name], ref)
			}
		}
		if _, ok := graph[decl.Name]; !ok {
			graph[decl.Name] = nil
		}
	}
	return graph
}

func (c *compiler) factGraph() map[string][]string {
	graph := map[string][]string{}
	for _, decl := range c.module.Facts {
		addRef := func(ref string) {
			if _, ok := c.module.Facts[ref]; ok {
				graph[decl.Name] = append(graph[decl.Name], ref)
				return
			}
			parts := strings.Split(ref, ".")
			if len(parts) >= 2 {
				if _, ok := c.module.Facts[parts[0]]; ok && parts[0] != decl.Name {
					graph[decl.Name] = append(graph[decl.Name], parts[0])
				}
			}
		}
		for _, expr := range decl.When {
			for _, ref := range lang.ExprRefs(expr) {
				addRef(ref)
			}
		}
		for _, expose := range decl.Expose {
			for _, ref := range lang.ExprRefs(expose.Expr) {
				addRef(ref)
			}
		}
		if _, ok := graph[decl.Name]; !ok {
			graph[decl.Name] = nil
		}
	}
	return graph
}

func (c *compiler) unifiedGraph() map[string][]string {
	graph := map[string][]string{}
	for _, decl := range c.module.Computeds {
		for _, ref := range lang.ExprRefs(decl.Expr) {
			if _, ok := c.module.Computeds[ref]; ok {
				graph[decl.Name] = append(graph[decl.Name], ref)
			} else if _, ok := c.module.Facts[ref]; ok {
				graph[decl.Name] = append(graph[decl.Name], ref)
			} else {
				parts := strings.Split(ref, ".")
				if len(parts) >= 2 {
					if _, ok := c.module.Facts[parts[0]]; ok {
						graph[decl.Name] = append(graph[decl.Name], parts[0])
					}
				}
			}
		}
		if _, ok := graph[decl.Name]; !ok {
			graph[decl.Name] = nil
		}
	}
	for _, decl := range c.module.Facts {
		addRef := func(ref string) {
			if _, ok := c.module.Facts[ref]; ok {
				graph[decl.Name] = append(graph[decl.Name], ref)
			} else if _, ok := c.module.Computeds[ref]; ok {
				graph[decl.Name] = append(graph[decl.Name], ref)
			} else {
				parts := strings.Split(ref, ".")
				if len(parts) >= 2 {
					if _, ok := c.module.Facts[parts[0]]; ok && parts[0] != decl.Name {
						graph[decl.Name] = append(graph[decl.Name], parts[0])
					}
				}
			}
		}
		for _, expr := range decl.When {
			for _, ref := range lang.ExprRefs(expr) {
				addRef(ref)
			}
		}
		for _, expose := range decl.Expose {
			for _, ref := range lang.ExprRefs(expose.Expr) {
				addRef(ref)
			}
		}
		if _, ok := graph[decl.Name]; !ok {
			graph[decl.Name] = nil
		}
	}
	return graph
}

func (c *compiler) validateExprs(exprs []*lang.Expr, scope exprScope) {
	for _, expr := range exprs {
		c.validateExpr(expr, scope)
	}
}

func (c *compiler) validateExpr(expr *lang.Expr, scope exprScope) {
	for _, ref := range lang.ExprRefs(expr) {
		switch {
		case strings.HasPrefix(ref, "signal."):
			if !scope.allowSignal {
				c.add("AX204", fmt.Sprintf("signal.* outside signal-triggered rule: %s", ref))
			}
		case strings.HasPrefix(ref, "output."):
			field := strings.TrimPrefix(ref, "output.")
			if _, ok := scope.outputFields[field]; !ok {
				c.add("AX302", fmt.Sprintf("output field does not exist: %s", ref))
			}
		case strings.HasPrefix(ref, "runtime."):
			if !scope.allowRuntime {
				c.add("AX001", fmt.Sprintf("unresolved reference: %s", ref))
			}
		case c.isKnownRef(ref):
		default:
			c.add("AX001", fmt.Sprintf("unresolved reference: %s", ref))
		}
	}
}

func (c *compiler) isKnownRef(ref string) bool {
	if _, ok := c.module.Symbols[ref]; ok {
		return true
	}
	if c.isContextField(ref) {
		return true
	}
	parts := strings.Split(ref, ".")
	if len(parts) > 1 {
		if _, ok := c.module.Computeds[ref]; ok {
			return true
		}
		if _, ok := c.module.Symbols[parts[0]+"."+parts[1]]; ok && c.module.Symbols[parts[0]+"."+parts[1]].Kind == SymbolField {
			return true
		}
	}
	return false
}

func (c *compiler) isContextField(ref string) bool {
	parts := strings.Split(ref, ".")
	if len(parts) < 2 {
		return false
	}
	if _, ok := c.module.Contexts[parts[0]]; !ok {
		return false
	}
	sym, ok := c.module.Symbols[parts[0]+"."+parts[1]]
	return ok && sym.Kind == SymbolField && sym.Owner == parts[0]
}

func (c *compiler) buildIndexes() {
	for _, computed := range c.module.Computeds {
		c.indexExpr(computed.Expr, NodeID{Kind: "computed", Name: computed.Name})
	}
	for _, fact := range c.module.Facts {
		node := NodeID{Kind: "fact", Name: fact.Name}
		for _, expr := range fact.When {
			c.indexExpr(expr, node)
		}
		for _, expose := range fact.Expose {
			c.indexExpr(expose.Expr, node)
		}
	}
	for _, claim := range c.module.Claims {
		node := NodeID{Kind: "claim", Name: claim.Name}
		for _, expr := range claim.Always {
			c.indexExpr(expr, node)
			for _, ref := range lang.ExprRefs(expr) {
				if field, ok := c.contextFieldRoot(ref); ok {
					c.module.Indexes.ClaimIndex[field] = append(c.module.Indexes.ClaimIndex[field], claim.Name)
				}
			}
		}
	}
	for _, activity := range c.module.Activities {
		if activity.Policy != "" {
			c.module.Indexes.PolicyIndex[activity.Policy] = append(c.module.Indexes.PolicyIndex[activity.Policy], activity.Name)
		}
	}
	for _, rule := range c.module.Rules {
		for _, trigger := range rule.Triggers {
			switch trigger.Kind {
			case lang.TriggerSignal:
				c.module.Indexes.SignalIndex[trigger.Name] = append(c.module.Indexes.SignalIndex[trigger.Name], rule.Name)
			case lang.TriggerChanged:
				c.module.Indexes.ChangedIndex[trigger.Target] = append(c.module.Indexes.ChangedIndex[trigger.Target], rule.Name)
			case lang.TriggerTimer:
				c.module.Indexes.TimerIndex[trigger.Target] = append(c.module.Indexes.TimerIndex[trigger.Target], rule.Name)
			}
		}
		for _, write := range rule.Writes {
			c.module.Indexes.WriteTargetIndex[write.Name] = append(c.module.Indexes.WriteTargetIndex[write.Name], rule.Name)
		}
		node := NodeID{Kind: "rule", Name: rule.Name}
		for _, expr := range rule.When {
			c.indexExpr(expr, node)
		}
		for _, expr := range rule.Require {
			c.indexExpr(expr, node)
		}
	}
}

func (c *compiler) buildIDs() {
	ids := IDTables{
		FieldIDs:    map[string]FieldID{},
		SignalIDs:   map[string]SignalID{},
		RuleIDs:     map[string]RuleID{},
		ActivityIDs: map[string]ActivityID{},
	}
	for _, contextDecl := range c.module.AST.Contexts {
		for _, field := range contextDecl.Fields {
			name := contextDecl.Name + "." + field.Name
			ids.FieldIDs[name] = FieldID(len(ids.Fields))
			ids.Fields = append(ids.Fields, name)
		}
	}
	for _, signal := range c.module.AST.Signals {
		ids.SignalIDs[signal.Name] = SignalID(len(ids.Signals))
		ids.Signals = append(ids.Signals, signal.Name)
	}
	for _, rule := range c.module.AST.Rules {
		ids.RuleIDs[rule.Name] = RuleID(len(ids.Rules))
		ids.Rules = append(ids.Rules, rule.Name)
	}
	for _, activity := range c.module.AST.Activities {
		ids.ActivityIDs[activity.Name] = ActivityID(len(ids.Activities))
		ids.Activities = append(ids.Activities, activity.Name)
	}
	c.module.IDs = ids
}

func (c *compiler) indexExpr(expr *lang.Expr, node NodeID) {
	for _, ref := range lang.ExprRefs(expr) {
		if field, ok := c.contextFieldRoot(ref); ok {
			c.module.Indexes.ContextFieldIndex[field] = append(c.module.Indexes.ContextFieldIndex[field], node)
			continue
		}
		if _, ok := c.module.Computeds[ref]; ok {
			c.module.Indexes.ComputedDependencyIndex[ref] = append(c.module.Indexes.ComputedDependencyIndex[ref], node)
			continue
		}
		if _, ok := c.module.Facts[ref]; ok {
			c.module.Indexes.FactDependencyIndex[ref] = append(c.module.Indexes.FactDependencyIndex[ref], node)
		}
	}
}

func (c *compiler) contextFieldRoot(ref string) (string, bool) {
	parts := strings.Split(ref, ".")
	if len(parts) < 2 {
		return "", false
	}
	root := parts[0] + "." + parts[1]
	if c.isContextField(root) {
		return root, true
	}
	return "", false
}

func (c *compiler) add(code, message string) {
	c.addAt(code, message, "compile", "", 0, "")
}

func (c *compiler) addAt(code, message, kind, entity string, line int, hint string) {
	c.diags = append(c.diags, Diagnostic{Code: code, Message: message, Kind: kind, Entity: entity, Line: line, Hint: hint})
}

func policyString(policy lang.PolicyDecl, name string) (string, bool) {
	expr, ok := policy.Entries[name]
	if !ok || expr.Kind != lang.ExprLiteral {
		return "", false
	}
	value, ok := expr.Value.(string)
	return value, ok
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

const SemanticDigestVersion = "v1"

func canonicalExpr(expr *lang.Expr) any {
	if expr == nil {
		return nil
	}
	m := map[string]any{
		"kind": string(expr.Kind),
	}
	if expr.Op != "" {
		m["op"] = expr.Op
	}
	if expr.Name != "" {
		m["name"] = expr.Name
	}
	if expr.Value != nil {
		switch v := expr.Value.(type) {
		case int:
			m["value"] = int64(v)
		case int64:
			m["value"] = v
		case float64:
			m["value"] = v
		case string:
			m["value"] = v
		case bool:
			m["value"] = v
		case lang.DurationLiteral:
			m["value"] = string(v)
		default:
			m["value"] = fmt.Sprint(v)
		}
	}
	if expr.Left != nil {
		m["left"] = canonicalExpr(expr.Left)
	}
	if expr.Right != nil {
		m["right"] = canonicalExpr(expr.Right)
	}
	if len(expr.Args) > 0 {
		args := make([]any, len(expr.Args))
		for i, a := range expr.Args {
			args[i] = canonicalExpr(a)
		}
		m["args"] = args
	}
	return m
}

func canonicalIR(module *Module) any {
	if module == nil {
		return nil
	}
	var contexts []any
	if len(module.Contexts) > 0 {
		contextNames := sortedKeys(module.Contexts)
		contexts = make([]any, 0, len(contextNames))
		for _, name := range contextNames {
			ctx := module.Contexts[name]
			fields := append([]lang.FieldDecl{}, ctx.Fields...)
			sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
			cFields := make([]any, 0, len(fields))
			for _, f := range fields {
				cFields = append(cFields, map[string]any{
					"name":        f.Name,
					"type":        f.Type,
					"has_default": f.HasDefault,
					"default":     canonicalExpr(f.Default),
				})
			}
			contexts = append(contexts, map[string]any{
				"name":   ctx.Name,
				"fields": cFields,
			})
		}
	}

	var signals []any
	if len(module.Signals) > 0 {
		signalNames := sortedKeys(module.Signals)
		signals = make([]any, 0, len(signalNames))
		for _, name := range signalNames {
			sig := module.Signals[name]
			fields := append([]lang.FieldDecl{}, sig.Fields...)
			sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
			sFields := make([]any, 0, len(fields))
			for _, f := range fields {
				sFields = append(sFields, map[string]any{
					"name": f.Name,
					"type": f.Type,
				})
			}
			signals = append(signals, map[string]any{
				"name":   sig.Name,
				"fields": sFields,
			})
		}
	}

	var computeds []any
	if len(module.Computeds) > 0 {
		computedNames := sortedKeys(module.Computeds)
		computeds = make([]any, 0, len(computedNames))
		for _, name := range computedNames {
			comp := module.Computeds[name]
			computeds = append(computeds, map[string]any{
				"name": comp.Name,
				"type": comp.Type,
				"expr": canonicalExpr(comp.Expr),
			})
		}
	}

	var facts []any
	if len(module.Facts) > 0 {
		factNames := sortedKeys(module.Facts)
		facts = make([]any, 0, len(factNames))
		for _, name := range factNames {
			fact := module.Facts[name]
			when := make([]any, 0, len(fact.When))
			for _, w := range fact.When {
				when = append(when, canonicalExpr(w))
			}
			var cExpose []any
			if len(fact.Expose) > 0 {
				expose := append([]lang.Binding{}, fact.Expose...)
				sort.Slice(expose, func(i, j int) bool { return expose[i].Name < expose[j].Name })
				cExpose = make([]any, 0, len(expose))
				for _, exp := range expose {
					cExpose = append(cExpose, map[string]any{
						"name": exp.Name,
						"expr": canonicalExpr(exp.Expr),
					})
				}
			}
			facts = append(facts, map[string]any{
				"name":   fact.Name,
				"when":   when,
				"expose": cExpose,
			})
		}
	}

	var rules []any
	if len(module.Rules) > 0 {
		ruleNames := sortedKeys(module.Rules)
		rules = make([]any, 0, len(ruleNames))
		for _, name := range ruleNames {
			rule := module.Rules[name]
			triggers := append([]lang.Trigger{}, rule.Triggers...)
			sort.Slice(triggers, func(i, j int) bool {
				if triggers[i].Kind != triggers[j].Kind {
					return triggers[i].Kind < triggers[j].Kind
				}
				if triggers[i].Name != triggers[j].Name {
					return triggers[i].Name < triggers[j].Name
				}
				return triggers[i].Target < triggers[j].Target
			})
			cTriggers := make([]any, 0, len(triggers))
			for _, t := range triggers {
				cTriggers = append(cTriggers, map[string]any{
					"kind":   string(t.Kind),
					"name":   t.Name,
					"target": t.Target,
				})
			}
			when := make([]any, 0, len(rule.When))
			for _, w := range rule.When {
				when = append(when, canonicalExpr(w))
			}
			require := make([]any, 0, len(rule.Require))
			for _, req := range rule.Require {
				require = append(require, canonicalExpr(req))
			}
			writes := make([]any, 0, len(rule.Writes))
			for _, wr := range rule.Writes {
				writes = append(writes, map[string]any{
					"name": wr.Name,
					"expr": canonicalExpr(wr.Expr),
				})
			}
			rules = append(rules, map[string]any{
				"name":     rule.Name,
				"triggers": cTriggers,
				"when":     when,
				"require":  require,
				"run":      rule.Run,
				"writes":   writes,
			})
		}
	}

	var claims []any
	if len(module.Claims) > 0 {
		claimNames := sortedKeys(module.Claims)
		claims = make([]any, 0, len(claimNames))
		for _, name := range claimNames {
			claim := module.Claims[name]
			always := make([]any, 0, len(claim.Always))
			for _, a := range claim.Always {
				always = append(always, canonicalExpr(a))
			}
			claims = append(claims, map[string]any{
				"name":   claim.Name,
				"always": always,
			})
		}
	}

	var queries []any
	if len(module.Queries) > 0 {
		queryNames := sortedKeys(module.Queries)
		queries = make([]any, 0, len(queryNames))
		for _, name := range queryNames {
			q := module.Queries[name]
			ret := append([]lang.Binding{}, q.Return...)
			sort.Slice(ret, func(i, j int) bool { return ret[i].Name < ret[j].Name })
			cRet := make([]any, 0, len(ret))
			for _, b := range ret {
				cRet = append(cRet, map[string]any{
					"name": b.Name,
					"expr": canonicalExpr(b.Expr),
				})
			}
			queries = append(queries, map[string]any{
				"name":   q.Name,
				"return": cRet,
			})
		}
	}

	var activities []any
	if len(module.Activities) > 0 {
		activityNames := sortedKeys(module.Activities)
		activities = make([]any, 0, len(activityNames))
		for _, name := range activityNames {
			act := module.Activities[name]
			require := make([]any, 0, len(act.Require))
			for _, r := range act.Require {
				require = append(require, canonicalExpr(r))
			}
			input := append([]lang.Binding{}, act.Input...)
			sort.Slice(input, func(i, j int) bool { return input[i].Name < input[j].Name })
			cInput := make([]any, 0, len(input))
			for _, b := range input {
				cInput = append(cInput, map[string]any{
					"name": b.Name,
					"expr": canonicalExpr(b.Expr),
				})
			}
			output := append([]lang.FieldDecl{}, act.Output...)
			sort.Slice(output, func(i, j int) bool { return output[i].Name < output[j].Name })
			cOutput := make([]any, 0, len(output))
			for _, f := range output {
				cOutput = append(cOutput, map[string]any{
					"name": f.Name,
					"type": f.Type,
				})
			}
			activities = append(activities, map[string]any{
				"name":            act.Name,
				"policy":          act.Policy,
				"effect":          act.Effect,
				"idempotency_key": canonicalExpr(act.IdempotencyKey),
				"require":         require,
				"input":           cInput,
				"output":          cOutput,
			})
		}
	}

	var policies []any
	if len(module.Policies) > 0 {
		policyNames := sortedKeys(module.Policies)
		policies = make([]any, 0, len(policyNames))
		for _, name := range policyNames {
			pol := module.Policies[name]
			eKeys := sortedKeys(pol.Entries)
			cEntries := make([]any, 0, len(eKeys))
			for _, k := range eKeys {
				cEntries = append(cEntries, map[string]any{
					"key":  k,
					"expr": canonicalExpr(pol.Entries[k]),
				})
			}
			cKeys := sortedKeys(pol.Catches)
			cCatches := make([]any, 0, len(cKeys))
			for _, k := range cKeys {
				cCatches = append(cCatches, map[string]any{
					"source": k,
					"target": pol.Catches[k],
				})
			}
			policies = append(policies, map[string]any{
				"name":    pol.Name,
				"entries": cEntries,
				"catches": cCatches,
			})
		}
	}

	return map[string]any{
		"semantic_digest_version": SemanticDigestVersion,
		"dsl":                     module.DSLVersion,
		"compiler":                module.CompilerVersion,
		"plan":                    module.PlanVersion,
		"domain":                  module.Domain,
		"contexts":                contexts,
		"signals":                 signals,
		"computeds":               computeds,
		"facts":                   facts,
		"rules":                   rules,
		"claims":                  claims,
		"queries":                 queries,
		"activities":              activities,
		"policies":                policies,
	}
}

func compiledHash(module *Module) string {
	if module == nil {
		return ""
	}
	ir := canonicalIR(module)
	data, err := json.Marshal(ir)
	if err != nil {
		return ""
	}
	return hashBytes(data)
}

func sortedKeys[T any](values map[string]T) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
