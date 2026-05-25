package runtime

import (
	"fmt"
	"math/bits"
	"sort"
	"strings"

	"axiom/internal/compiler"
	"axiom/internal/diag"
	"axiom/internal/lang"
)

type bitset []uint64

const MaxFastClauses = 1024

func newBitset(size int) bitset {
	if size <= 0 {
		return nil
	}
	return make([]uint64, (size+63)/64)
}

func (b bitset) clone() bitset {
	out := make(bitset, len(b))
	copy(out, b)
	return out
}

func (b bitset) set(id int) {
	b[id/64] |= 1 << uint(id%64)
}

func (b bitset) clear(id int) {
	b[id/64] &^= 1 << uint(id%64)
}

func (b bitset) clearAll() {
	for i := range b {
		b[i] = 0
	}
}

func (b bitset) has(id int) bool {
	return b[id/64]&(1<<uint(id%64)) != 0
}

func (b bitset) or(other bitset) {
	for i := range b {
		if i < len(other) {
			b[i] |= other[i]
		}
	}
}

func (b bitset) orMany(indexes []bitset, ids bitset) {
	ids.forEach(func(id int) {
		if id < len(indexes) {
			b.or(indexes[id])
		}
	})
}

func (b bitset) empty() bool {
	for _, word := range b {
		if word != 0 {
			return false
		}
	}
	return true
}

func (b bitset) count() int {
	count := 0
	for _, word := range b {
		count += bits.OnesCount64(word)
	}
	return count
}

func (b bitset) first() (int, bool) {
	for wordIdx, word := range b {
		if word == 0 {
			continue
		}
		return wordIdx*64 + bits.TrailingZeros64(word), true
	}
	return 0, false
}

func (b bitset) forEach(fn func(id int)) {
	for wordIdx, word := range b {
		for word != 0 {
			bit := bits.TrailingZeros64(word)
			fn(wordIdx*64 + bit)
			word &= word - 1
		}
	}
}

func (b bitset) containsAll(required bitset) bool {
	for i, word := range required {
		if b[i]&word != word {
			return false
		}
	}
	return true
}

func (b bitset) containsNone(forbidden bitset) bool {
	for i, word := range forbidden {
		if b[i]&word != 0 {
			return false
		}
	}
	return true
}

func (b bitset) ids() []int {
	out := make([]int, 0, b.count())
	b.forEach(func(id int) { out = append(out, id) })
	return out
}

type fastAtomKind string

const (
	fastAtomPredicate fastAtomKind = "predicate"
	fastAtomComputed  fastAtomKind = "computed"
	fastAtomFact      fastAtomKind = "fact"
)

type fastAtom struct {
	id     int
	name   string
	kind   fastAtomKind
	expr   *lang.Expr
	exprs  []*lang.Expr
	expose []lang.Binding
	vm     *exprProgram
	vms    []*exprProgram
}

type fastClause struct {
	required  bitset
	forbidden bitset
}

type fastCondition struct {
	clauses      []*fastClause
	slow         []*lang.Expr
	slowPrograms []*exprProgram
}

type fastRule struct {
	name    string
	when    fastCondition
	require fastCondition
}

type fastPlan struct {
	module       *compiler.Module
	strict       bool
	atoms        []fastAtom
	atomsByName  map[string]int
	predicateKey map[string]int
	fieldIDs     map[string]int
	fields       []string
	signalIDs    map[string]int
	signals      []string
	fieldAtoms   []bitset
	atomDeps     []bitset
	atomRules    []bitset
	atomClaims   []bitset
	signalRules  []bitset
	fieldRules   []bitset
	rules        []fastRule
	ruleIDs      map[string]int
	claims       []fastCondition
	claimNames   []string
	strictErrs   []diag.Error
	compiler     exprCompiler
}

func compileFastPlan(module *compiler.Module, strict bool) *fastPlan {
	p := &fastPlan{
		module:       module,
		strict:       strict,
		atomsByName:  map[string]int{},
		predicateKey: map[string]int{},
		fieldIDs:     map[string]int{},
		signalIDs:    map[string]int{},
		ruleIDs:      map[string]int{},
	}
	if module == nil || module.AST == nil {
		return p
	}
	p.fields = append([]string{}, module.IDs.Fields...)
	for id, name := range p.fields {
		p.fieldIDs[name] = id
	}
	p.signals = append([]string{}, module.IDs.Signals...)
	for id, name := range p.signals {
		p.signalIDs[name] = id
	}
	p.fieldAtoms = make([]bitset, len(p.fields))
	p.fieldRules = make([]bitset, len(p.fields))
	p.signalRules = make([]bitset, len(p.signals))
	p.compiler = exprCompiler{fieldIDs: p.fieldIDs, atomIDs: p.atomsByName}
	for _, computed := range module.AST.Computeds {
		if computed.Type == "Bool" {
			p.addNamedAtom(computed.Name, fastAtom{kind: fastAtomComputed, name: computed.Name, expr: computed.Expr})
		}
	}
	for _, fact := range module.AST.Facts {
		p.addNamedAtom(fact.Name, fastAtom{kind: fastAtomFact, name: fact.Name, exprs: fact.When, expose: fact.Expose})
	}
	for _, computed := range module.AST.Computeds {
		if computed.Type == "Bool" {
			p.indexAtomDeps(p.atomsByName[computed.Name], []*lang.Expr{computed.Expr})
		}
	}
	for _, fact := range module.AST.Facts {
		p.indexAtomDeps(p.atomsByName[fact.Name], fact.When)
	}
	for _, rule := range module.AST.Rules {
		ruleID := len(p.rules)
		p.ruleIDs[rule.Name] = ruleID
		when := p.compileExprs(rule.Name+".when", rule.When)
		require := p.compileExprs(rule.Name+".require", rule.Require)
		p.rules = append(p.rules, fastRule{name: rule.Name, when: when, require: require})
		for _, trigger := range rule.Triggers {
			switch trigger.Kind {
			case lang.TriggerSignal:
				if signalID, ok := p.signalID(trigger.Name); ok {
					p.signalRules[signalID] = p.withBit(p.signalRules[signalID], ruleID, len(p.rules))
				}
			case lang.TriggerChanged:
				if fieldID, ok := p.fieldID(trigger.Target); ok {
					p.fieldRules[fieldID] = p.withBit(p.fieldRules[fieldID], ruleID, len(p.rules))
				}
			}
		}
		p.indexRuleDeps(ruleID, when)
		p.indexRuleDeps(ruleID, require)
	}
	for _, claim := range module.AST.Claims {
		claimID := len(p.claims)
		cond := p.compileExprs(claim.Name+".always", claim.Always)
		p.claims = append(p.claims, cond)
		p.claimNames = append(p.claimNames, claim.Name)
		p.indexClaimDeps(claimID, cond)
	}
	p.resizeIndexes()
	return p
}

func (p *fastPlan) addNamedAtom(name string, atom fastAtom) int {
	if id, ok := p.atomsByName[name]; ok {
		return id
	}
	atom.id = len(p.atoms)
	if atom.expr != nil {
		atom.vm = p.compiler.compile(atom.expr)
	}
	for _, expr := range atom.exprs {
		atom.vms = append(atom.vms, p.compiler.compile(expr))
	}
	p.atoms = append(p.atoms, atom)
	p.atomsByName[name] = atom.id
	p.atomDeps = append(p.atomDeps, nil)
	p.atomRules = append(p.atomRules, nil)
	p.atomClaims = append(p.atomClaims, nil)
	return atom.id
}

func (p *fastPlan) addPredicateAtom(expr *lang.Expr) int {
	key := exprKey(expr)
	if id, ok := p.predicateKey[key]; ok {
		return id
	}
	atom := fastAtom{id: len(p.atoms), kind: fastAtomPredicate, name: key, expr: expr, vm: p.compiler.compile(expr)}
	p.atoms = append(p.atoms, atom)
	p.predicateKey[key] = atom.id
	p.atomDeps = append(p.atomDeps, nil)
	p.atomRules = append(p.atomRules, nil)
	p.atomClaims = append(p.atomClaims, nil)
	p.indexAtomDeps(atom.id, []*lang.Expr{expr})
	return atom.id
}

func (p *fastPlan) compileExprs(owner string, exprs []*lang.Expr) fastCondition {
	cond := fastCondition{clauses: []*fastClause{{required: newBitset(len(p.atoms)), forbidden: newBitset(len(p.atoms))}}}
	for _, expr := range exprs {
		next, ok := p.compileBool(expr)
		if !ok {
			cond.slow = append(cond.slow, expr)
			cond.slowPrograms = append(cond.slowPrograms, p.compiler.compile(expr))
			p.addStrictErr(owner, expr)
			continue
		}
		cond = p.andConditions(cond, next)
	}
	return cond
}

func (p *fastPlan) compileBool(expr *lang.Expr) (fastCondition, bool) {
	if expr == nil {
		return fastCondition{clauses: []*fastClause{{required: newBitset(len(p.atoms)), forbidden: newBitset(len(p.atoms))}}}, true
	}
	if expr.Kind == lang.ExprRef {
		if id, ok := p.atomsByName[expr.Name]; ok {
			return p.singleAtomCondition(id, false), true
		}
	}
	if expr.Kind == lang.ExprUnary && expr.Op == "exists" && isFastRuntimePredicate(expr) {
		return p.singleAtomCondition(p.addPredicateAtom(expr), false), true
	}
	if expr.Kind == lang.ExprCall && expr.Name == "missing" && isFastRuntimePredicate(expr) {
		return p.singleAtomCondition(p.addPredicateAtom(expr), false), true
	}
	if expr.Kind != lang.ExprBinary {
		if isFastRuntimePredicate(expr) {
			return p.singleAtomCondition(p.addPredicateAtom(expr), false), true
		}
		return fastCondition{}, false
	}
	switch expr.Op {
	case "and":
		left, ok := p.compileBool(expr.Left)
		if !ok {
			return fastCondition{}, false
		}
		right, ok := p.compileBool(expr.Right)
		if !ok {
			return fastCondition{}, false
		}
		if len(left.clauses)*len(right.clauses) > MaxFastClauses {
			p.addDNFErr()
			return fastCondition{slow: []*lang.Expr{expr}, slowPrograms: []*exprProgram{p.compiler.compile(expr)}}, true
		}
		return p.andConditions(left, right), true
	case "or":
		left, ok := p.compileBool(expr.Left)
		if !ok {
			return fastCondition{}, false
		}
		right, ok := p.compileBool(expr.Right)
		if !ok {
			return fastCondition{}, false
		}
		if len(left.clauses)+len(right.clauses) > MaxFastClauses {
			p.addDNFErr()
			return fastCondition{slow: []*lang.Expr{expr}, slowPrograms: []*exprProgram{p.compiler.compile(expr)}}, true
		}
		return p.orConditions(left, right), true
	case "implies":
		left, ok := p.compileBool(expr.Left)
		if !ok || len(left.clauses) != 1 || !emptyBits(left.clauses[0].forbidden) || countBits(left.clauses[0].required) != 1 {
			return fastCondition{}, false
		}
		right, ok := p.compileBool(expr.Right)
		if !ok {
			return fastCondition{}, false
		}
		id, _ := left.clauses[0].required.first()
		return p.orConditions(p.singleAtomCondition(id, true), right), true
	case "==", "!=", ">", ">=", "<", "<=", "in":
		if isFastRuntimePredicate(expr) {
			return p.singleAtomCondition(p.addPredicateAtom(expr), false), true
		}
	}
	return fastCondition{}, false
}

func (p *fastPlan) singleAtomCondition(id int, forbidden bool) fastCondition {
	clause := &fastClause{required: newBitset(len(p.atoms)), forbidden: newBitset(len(p.atoms))}
	if forbidden {
		clause.forbidden.set(id)
	} else {
		clause.required.set(id)
	}
	return fastCondition{clauses: []*fastClause{clause}}
}

func (p *fastPlan) andConditions(left, right fastCondition) fastCondition {
	out := fastCondition{
		slow:         append(append([]*lang.Expr{}, left.slow...), right.slow...),
		slowPrograms: append(append([]*exprProgram{}, left.slowPrograms...), right.slowPrograms...),
	}
	for _, l := range left.clauses {
		for _, r := range right.clauses {
			clause := &fastClause{required: mergeBits(l.required, r.required), forbidden: mergeBits(l.forbidden, r.forbidden)}
			out.clauses = append(out.clauses, clause)
		}
	}
	return out
}

func (p *fastPlan) orConditions(left, right fastCondition) fastCondition {
	out := fastCondition{
		slow:         append(append([]*lang.Expr{}, left.slow...), right.slow...),
		slowPrograms: append(append([]*exprProgram{}, left.slowPrograms...), right.slowPrograms...),
	}
	out.clauses = append(out.clauses, left.clauses...)
	out.clauses = append(out.clauses, right.clauses...)
	return out
}

func (p *fastPlan) addStrictErr(owner string, expr *lang.Expr) {
	if !p.strict {
		return
	}
	p.strictErrs = append(p.strictErrs, diag.Error{
		Code:    "AX701",
		Kind:    "compile",
		Entity:  owner,
		Message: fmt.Sprintf("expression is not supported by strict fast runtime: %s", lang.ExprString(expr)),
		Hint:    "Use boolean refs, literal comparisons, exists/missing, in with literal lists, and/or, or simple implies.",
	})
}

func (p *fastPlan) addDNFErr() {
	if !p.strict {
		return
	}
	p.strictErrs = append(p.strictErrs, diag.Error{
		Code:    "AX702",
		Kind:    "compile",
		Message: fmt.Sprintf("fast condition exceeds MaxFastClauses=%d", MaxFastClauses),
		Hint:    "Split the condition or simplify nested or/and expressions.",
	})
}

func (p *fastPlan) strictError() error {
	if len(p.strictErrs) == 0 {
		return nil
	}
	return diag.Errors(p.strictErrs)
}

func (p *fastPlan) withBit(existing bitset, id int, size int) bitset {
	if len(existing) == 0 || len(existing)*64 < size {
		next := newBitset(size)
		next.or(existing)
		existing = next
	}
	existing.set(id)
	return existing
}

func (p *fastPlan) resizeIndexes() {
	size := len(p.atoms)
	for fieldID, bits := range p.fieldAtoms {
		p.fieldAtoms[fieldID] = resizeBitset(bits, size)
	}
	for id, bits := range p.atomDeps {
		p.atomDeps[id] = resizeBitset(bits, size)
	}
	ruleSize := len(p.rules)
	for signalID, bits := range p.signalRules {
		p.signalRules[signalID] = resizeBitset(bits, ruleSize)
	}
	for fieldID, bits := range p.fieldRules {
		p.fieldRules[fieldID] = resizeBitset(bits, ruleSize)
	}
	for id, bits := range p.atomRules {
		p.atomRules[id] = resizeBitset(bits, ruleSize)
	}
	for id, bits := range p.atomClaims {
		p.atomClaims[id] = resizeBitset(bits, len(p.claims))
	}
	for _, rule := range p.rules {
		rule.when.resize(size)
		rule.require.resize(size)
	}
	for _, claim := range p.claims {
		claim.resize(size)
	}
}

func resizeBitset(bits bitset, size int) bitset {
	next := newBitset(size)
	next.or(bits)
	return next
}

func mergeBits(left, right bitset) bitset {
	size := len(left)
	if len(right) > size {
		size = len(right)
	}
	out := make(bitset, size)
	out.or(left)
	out.or(right)
	return out
}

func (c fastCondition) resize(size int) {
	for _, clause := range c.clauses {
		clause.required = resizeBitset(clause.required, size)
		clause.forbidden = resizeBitset(clause.forbidden, size)
	}
}

func (p *fastPlan) indexAtomDeps(atomID int, exprs []*lang.Expr) {
	for _, expr := range exprs {
		for _, ref := range lang.ExprRefs(expr) {
			if depID, ok := p.atomsByName[ref]; ok {
				p.atomDeps[depID] = p.withBit(p.atomDeps[depID], atomID, len(p.atoms))
				continue
			}
			if field := contextRefRoot(ref); field != "" {
				if fieldID, ok := p.fieldID(field); ok {
					p.fieldAtoms[fieldID] = p.withBit(p.fieldAtoms[fieldID], atomID, len(p.atoms))
				}
			}
		}
	}
}

func (p *fastPlan) indexRuleDeps(ruleID int, cond fastCondition) {
	for _, id := range cond.atomIDs() {
		p.atomRules[id] = p.withBit(p.atomRules[id], ruleID, len(p.rules))
	}
	for _, expr := range cond.slow {
		for _, ref := range lang.ExprRefs(expr) {
			if atomID, ok := p.atomsByName[ref]; ok {
				p.atomRules[atomID] = p.withBit(p.atomRules[atomID], ruleID, len(p.rules))
				continue
			}
			if field := contextRefRoot(ref); field != "" {
				if fieldID, ok := p.fieldID(field); ok {
					p.fieldRules[fieldID] = p.withBit(p.fieldRules[fieldID], ruleID, len(p.rules))
				}
			}
		}
	}
}

func (p *fastPlan) indexClaimDeps(claimID int, cond fastCondition) {
	for _, id := range cond.atomIDs() {
		p.atomClaims[id] = p.withBit(p.atomClaims[id], claimID, len(p.claims))
	}
	for _, expr := range cond.slow {
		for _, ref := range lang.ExprRefs(expr) {
			if atomID, ok := p.atomsByName[ref]; ok {
				p.atomClaims[atomID] = p.withBit(p.atomClaims[atomID], claimID, len(p.claims))
				continue
			}
			if field := contextRefRoot(ref); field != "" {
				if fieldID, ok := p.fieldID(field); ok {
					p.fieldAtoms[fieldID].forEach(func(atomID int) {
						p.atomClaims[atomID] = p.withBit(p.atomClaims[atomID], claimID, len(p.claims))
					})
				}
			}
		}
	}
}

func (c fastCondition) atomIDs() []int {
	seen := map[int]struct{}{}
	for _, clause := range c.clauses {
		clause.required.forEach(func(id int) {
			seen[id] = struct{}{}
		})
		clause.forbidden.forEach(func(id int) {
			seen[id] = struct{}{}
		})
	}
	var out []int
	for id := range seen {
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}

func (p *fastPlan) state(execution *Execution) bitset {
	if execution == nil {
		return newBitset(len(p.atoms))
	}
	ensureExecutionState(execution, len(p.fields), len(p.atoms))
	return bitset(execution.RuntimeState.ActiveAtoms)
}

type Dirty struct {
	Fields bitset
	Atoms  bitset
	Full   bool
}

func (p *fastPlan) dirtyFromChanged(changed map[string]struct{}) Dirty {
	if changed == nil {
		return Dirty{Full: true}
	}
	dirty := Dirty{Fields: newBitset(len(p.fields))}
	for field := range changed {
		if fieldID, ok := p.fieldID(contextFieldName(field)); ok {
			dirty.Fields.set(fieldID)
		}
	}
	return dirty
}

func (p *fastPlan) recompute(execution *Execution, changed map[string]struct{}) ([]string, error) {
	return p.recomputeDirty(execution, p.dirtyFromChanged(changed))
}

func (p *fastPlan) recomputeDirty(execution *Execution, changed Dirty) ([]string, error) {
	if p == nil {
		return nil, nil
	}
	ensureExecutionState(execution, len(p.fields), len(p.atoms))
	state := p.state(execution)
	dirty := newBitset(len(p.atoms))
	if changed.Full {
		for id := range p.atoms {
			dirty.set(id)
		}
	} else {
		dirty.or(changed.Atoms)
		dirty.orMany(p.fieldAtoms, changed.Fields)
	}
	changedAtoms := newBitset(len(p.atoms))
	nextDirty := newBitset(len(p.atoms))
	for !dirty.empty() {
		ids := dirty
		nextDirty.clearAll()
		var evalErr error
		ids.forEach(func(id int) {
			if evalErr != nil {
				return
			}
			if id >= len(p.atoms) {
				return
			}
			next, err := p.evalAtom(id, execution, state)
			if err != nil {
				evalErr = err
				return
			}
			if state.has(id) == next {
				return
			}
			if next {
				state.set(id)
			} else {
				state.clear(id)
			}
			changedAtoms.set(id)
			nextDirty.or(p.atomDeps[id])
		})
		if evalErr != nil {
			return nil, evalErr
		}
		dirty, nextDirty = nextDirty, dirty
	}
	p.syncExecutionFromAtoms(execution, state)
	changedCount := changedAtoms.count()
	if changedCount == 0 {
		return nil, nil
	}
	changedNames := make([]string, 0, changedCount)
	changedAtoms.forEach(func(id int) {
		changedNames = append(changedNames, p.atoms[id].name)
	})
	sort.Strings(changedNames)
	return changedNames, nil
}

func (p *fastPlan) evalAtom(id int, execution *Execution, state bitset) (bool, error) {
	atom := p.atoms[id]
	env := p.evalEnv(execution, nil)
	switch atom.kind {
	case fastAtomPredicate:
		return p.evalBoolProgram(atom.vm, atom.expr, env)
	case fastAtomComputed:
		value, err := p.evalProgram(atom.vm, atom.expr, env)
		if err != nil {
			return false, fmt.Errorf("computed %s: %w", atom.name, err)
		}
		if current, ok := execution.Computed[atom.name]; !ok || !typedEqual(current, value) {
			execution.Computed[atom.name] = value
		}
		ensureExecutionState(execution, len(p.fields), len(p.atoms))
		execution.RuntimeState.AtomValues[uint32(atom.id)] = valueOf(value)
		return truthy(value), nil
	case fastAtomFact:
		ok, err := p.evalAllPrograms(atom.vms, atom.exprs, env)
		if err != nil {
			return false, fmt.Errorf("fact %s: %w", atom.name, err)
		}
		next := FactValue{True: ok, Exposed: map[string]any{}}
		exposedValues := map[string]Value{}
		if ok {
			for _, expose := range atom.expose {
				value, err := evalExpr(expose.Expr, env)
				if err != nil {
					return false, fmt.Errorf("fact %s expose %s: %w", atom.name, expose.Name, err)
				}
				next.Exposed[expose.Name] = value
				exposedValues[expose.Name] = valueOf(value)
			}
		}
		if ok {
			execution.Facts[atom.name] = next
			ensureExecutionState(execution, len(p.fields), len(p.atoms))
			execution.RuntimeState.FactValues[uint32(atom.id)] = exposedValues
		} else {
			delete(execution.Facts, atom.name)
			if execution.RuntimeState.FactValues != nil {
				delete(execution.RuntimeState.FactValues, uint32(atom.id))
			}
		}
		return ok, nil
	default:
		_ = state
		return false, nil
	}
}

func (p *fastPlan) syncExecutionFromAtoms(execution *Execution, state bitset) {
	for _, atom := range p.atoms {
		switch atom.kind {
		case fastAtomComputed:
			if !state.has(atom.id) {
				delete(execution.Computed, atom.name)
				if execution.RuntimeState.AtomValues != nil {
					delete(execution.RuntimeState.AtomValues, uint32(atom.id))
				}
			}
		case fastAtomFact:
			if !state.has(atom.id) {
				delete(execution.Facts, atom.name)
				if execution.RuntimeState.FactValues != nil {
					delete(execution.RuntimeState.FactValues, uint32(atom.id))
				}
			}
		}
	}
}

func (p *fastPlan) ruleQueueForSignal(signal string) []string {
	if signalID, ok := p.signalID(signal); ok {
		return p.ruleNames(p.signalRules[signalID])
	}
	return nil
}

func (p *fastPlan) rulesForChanged(changed []string, atomNames []string) []string {
	bits := newBitset(len(p.rules))
	for _, field := range changed {
		if fieldID, ok := p.fieldID(contextFieldName(field)); ok {
			bits.or(p.fieldRules[fieldID])
		}
	}
	for _, name := range atomNames {
		if id, ok := p.atomID(name); ok {
			bits.or(p.atomRules[id])
		}
	}
	return p.ruleNames(bits)
}

func (p *fastPlan) ruleNames(bits bitset) []string {
	out := make([]string, 0, bits.count())
	bits.forEach(func(id int) {
		if id < len(p.rules) {
			out = append(out, p.rules[id].name)
		}
	})
	return out
}

func (p *fastPlan) ruleReady(ruleName string, env evalEnv) (bool, string, error) {
	id, ok := p.ruleIDs[ruleName]
	if !ok {
		return true, "", nil
	}
	rule := p.rules[id]
	state := p.state(env.execution)
	ok, err := p.conditionOK(rule.when, state, env)
	if err != nil || !ok {
		return ok, "when", err
	}
	ok, err = p.conditionOK(rule.require, state, env)
	if err != nil || !ok {
		return ok, "require", err
	}
	return true, "", nil
}

func (p *fastPlan) checkClaims(execution *Execution, changedAtoms []string) error {
	state := p.state(execution)
	bits := newBitset(len(p.claims))
	if len(changedAtoms) == 0 {
		for id := range p.claims {
			bits.set(id)
		}
	} else {
		for _, name := range changedAtoms {
			if atomID, ok := p.atomID(name); ok {
				bits.or(p.atomClaims[atomID])
			}
		}
	}
	env := p.evalEnv(execution, nil)
	var firstErr error
	bits.forEach(func(id int) {
		if firstErr != nil {
			return
		}
		if id >= len(p.claims) {
			return
		}
		ok, err := p.conditionOK(p.claims[id], state, env)
		if err != nil {
			firstErr = fmt.Errorf("claim %s: %w", p.claimNames[id], err)
			return
		}
		if !ok {
			firstErr = diag.Error{Code: "AX403", Kind: "runtime", Entity: p.claimNames[id], Message: fmt.Sprintf("claim failed: %s", p.claimNames[id]), Hint: "Inspect the rule write or patch that made the claim false."}
			return
		}
	})
	if firstErr != nil {
		return firstErr
	}
	return nil
}

func (p *fastPlan) atomID(name string) (int, bool) {
	if id, ok := p.atomsByName[name]; ok {
		return id, true
	}
	id, ok := p.predicateKey[name]
	return id, ok
}

func (p *fastPlan) fieldID(name string) (int, bool) {
	id, ok := p.fieldIDs[name]
	return id, ok
}

func (p *fastPlan) signalID(name string) (int, bool) {
	id, ok := p.signalIDs[name]
	return id, ok
}

func (p *fastPlan) conditionOK(cond fastCondition, state bitset, env evalEnv) (bool, error) {
	clauseOK := len(cond.clauses) == 0
	for _, clause := range cond.clauses {
		if state.containsAll(clause.required) && state.containsNone(clause.forbidden) {
			clauseOK = true
			break
		}
	}
	if !clauseOK {
		return false, nil
	}
	for i, expr := range cond.slow {
		var program *exprProgram
		if i < len(cond.slowPrograms) {
			program = cond.slowPrograms[i]
		}
		ok, err := p.evalBoolProgram(program, expr, env)
		if err != nil || !ok {
			return ok, err
		}
	}
	return true, nil
}

func isFastRuntimePredicate(expr *lang.Expr) bool {
	for _, ref := range lang.ExprRefs(expr) {
		if strings.HasPrefix(ref, "signal.") || strings.HasPrefix(ref, "output.") || strings.HasPrefix(ref, "runtime.") {
			return false
		}
	}
	if expr.Kind != lang.ExprBinary {
		return true
	}
	switch expr.Op {
	case "==", "!=":
		return (isRefExpr(expr.Left) && isLiteralExpr(expr.Right)) || (isLiteralExpr(expr.Left) && isRefExpr(expr.Right))
	case ">", ">=", "<", "<=":
		return isRefExpr(expr.Left) && isNumberLiteralExpr(expr.Right)
	case "in":
		return isRefExpr(expr.Left) && isLiteralListExpr(expr.Right)
	default:
		return true
	}
}

func isRefExpr(expr *lang.Expr) bool {
	return expr != nil && expr.Kind == lang.ExprRef
}

func isLiteralExpr(expr *lang.Expr) bool {
	return expr != nil && expr.Kind == lang.ExprLiteral
}

func isNumberLiteralExpr(expr *lang.Expr) bool {
	if !isLiteralExpr(expr) {
		return false
	}
	_, ok := number(expr.Value)
	return ok
}

func isLiteralListExpr(expr *lang.Expr) bool {
	if expr == nil || expr.Kind != lang.ExprCall || expr.Name != "list" {
		return false
	}
	for _, arg := range expr.Args {
		if !isLiteralExpr(arg) {
			return false
		}
	}
	return true
}

func contextRefRoot(ref string) string {
	if ref == "" || strings.HasPrefix(ref, "signal.") || strings.HasPrefix(ref, "output.") || strings.HasPrefix(ref, "runtime.") {
		return ""
	}
	parts := strings.Split(ref, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}

func exprKey(expr *lang.Expr) string {
	return lang.ExprString(expr)
}

func emptyBits(bits bitset) bool {
	for _, word := range bits {
		if word != 0 {
			return false
		}
	}
	return true
}

func countBits(bits bitset) int {
	count := 0
	for _, word := range bits {
		for word != 0 {
			word &= word - 1
			count++
		}
	}
	return count
}
