package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/Homiakus/axiom/internal/lang"
)

var evalStackPool = sync.Pool{
	New: func() any {
		s := make([]any, 0, 16)
		return &s
	},
}

func (program *exprProgram) eval(env evalEnv) (any, error) {
	if program == nil || len(program.instrs) == 0 {
		return nil, nil
	}
	stackPtr := evalStackPool.Get().(*[]any)
	stack := (*stackPtr)[:0]
	defer func() {
		for i := range stack {
			stack[i] = nil
		}
		*stackPtr = stack[:0]
		evalStackPool.Put(stackPtr)
	}()

	for _, in := range program.instrs {
		switch in.op {
		case opLoadConst:
			stack = append(stack, in.value)
		case opLoadField:
			stack = append(stack, loadField(env, int(in.a), in.ref, in.s))
		case opLoadAtom:
			stack = append(stack, loadAtom(env, int(in.a), in.ref))
		case opLoadSignal:
			stack = append(stack, resolvePath(env.signal, in.s))
		case opLoadOutput:
			stack = append(stack, resolvePath(env.output, in.s))
		case opLoadRef:
			stack = append(stack, resolveRef(in.ref, env))
		case opExists:
			val := popVMValue(&stack)
			stack = append(stack, exists(val))
		case opMissing:
			val := popVMValue(&stack)
			stack = append(stack, !exists(val))
		case opNot:
			val := popVMValue(&stack)
			stack = append(stack, !truthy(val))
		case opNeg:
			val := popVMValue(&stack)
			value, err := negateValue(val)
			if err != nil {
				return nil, err
			}
			stack = append(stack, value)
		case opChangedField:
			stack = append(stack, env.dirty.Fields != nil && env.dirty.Fields.has(int(in.a)))
		case opChangedRef:
			_, ok := env.changed[in.ref]
			stack = append(stack, ok)
		case opEq, opNe, opGt, opGe, opLt, opLe, opIn, opAnd, opOr, opImplies, opAdd, opSub, opMul, opDiv, opMod:
			var right, left any
			if len(stack) >= 2 {
				right = stack[len(stack)-1]
				left = stack[len(stack)-2]
				stack = stack[:len(stack)-2]
			} else if len(stack) == 1 {
				right = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
			value, err := evalVMBinary(in.op, left, right)
			if err != nil {
				return nil, err
			}
			stack = append(stack, value)
		case opList:
			n := in.n
			var values []any
			if n > 0 && len(stack) >= n {
				values = append([]any(nil), stack[len(stack)-n:]...)
				stack = stack[:len(stack)-n]
			}
			stack = append(stack, values)
		case opMap:
			n := in.n
			var values []any
			if n > 0 && len(stack) >= n {
				values = stack[len(stack)-n:]
				stack = stack[:len(stack)-n]
			}
			out := make(map[string]any, n/2)
			for i := 0; i+1 < len(values); i += 2 {
				out[fmt.Sprint(values[i])] = values[i+1]
			}
			stack = append(stack, out)
		case opHash:
			n := in.n
			var values []any
			if n > 0 && len(stack) >= n {
				values = stack[len(stack)-n:]
				stack = stack[:len(stack)-n]
			}
			data, err := json.Marshal(values)
			if err != nil {
				return nil, err
			}
			sum := sha256.Sum256(data)
			stack = append(stack, hex.EncodeToString(sum[:]))
		case opPureCall:
			n := in.n
			var values []any
			if n > 0 && len(stack) >= n {
				values = stack[len(stack)-n:]
				stack = stack[:len(stack)-n]
			}
			switch in.s {
			case "fixed", "exponential":
				parts := make([]string, 0, len(values))
				for _, value := range values {
					parts = append(parts, fmt.Sprint(value))
				}
				stack = append(stack, in.s+"("+strings.Join(parts, ",")+")")
			default:
				return nil, fmt.Errorf("unsupported pure call %s", in.s)
			}
		}
	}
	if len(stack) == 0 {
		return nil, nil
	}
	return stack[len(stack)-1], nil
}

func popVMValue(stack *[]any) any {
	values := *stack
	if len(values) == 0 {
		return nil
	}
	value := values[len(values)-1]
	*stack = values[:len(values)-1]
	return value
}

type opCode uint8

const (
	opLoadConst opCode = iota
	opLoadField
	opLoadAtom
	opLoadSignal
	opLoadOutput
	opLoadRef
	opExists
	opMissing
	opNot
	opNeg
	opChangedField
	opChangedRef
	opEq
	opNe
	opGt
	opGe
	opLt
	opLe
	opIn
	opAnd
	opOr
	opImplies
	opAdd
	opSub
	opMul
	opDiv
	opMod
	opList
	opMap
	opHash
	opPureCall
)

type instr struct {
	op    opCode
	a     uint32
	n     int
	s     string
	ref   string
	value any
}

type exprProgram struct {
	instrs []instr
}

type exprCompiler struct {
	fieldIDs map[string]int
	atomIDs  map[string]int
}

func (c exprCompiler) compile(expr *lang.Expr) *exprProgram {
	if expr == nil {
		return nil
	}
	program := &exprProgram{}
	c.emit(program, expr)
	return program
}

func (c exprCompiler) emit(program *exprProgram, expr *lang.Expr) {
	if expr == nil {
		program.instrs = append(program.instrs, instr{op: opLoadConst})
		return
	}
	switch expr.Kind {
	case lang.ExprLiteral:
		program.instrs = append(program.instrs, instr{op: opLoadConst, value: expr.Value})
	case lang.ExprRef:
		c.emitRef(program, expr.Name)
	case lang.ExprUnary:
		c.emit(program, expr.Left)
		switch expr.Op {
		case "exists":
			program.instrs = append(program.instrs, instr{op: opExists})
		case "not":
			program.instrs = append(program.instrs, instr{op: opNot})
		case "-":
			program.instrs = append(program.instrs, instr{op: opNeg})
		default:
			program.instrs = append(program.instrs, instr{op: opPureCall, s: expr.Op, n: 1})
		}
	case lang.ExprBinary:
		c.emit(program, expr.Left)
		c.emit(program, expr.Right)
		program.instrs = append(program.instrs, instr{op: binaryOp(expr.Op)})
	case lang.ExprCall:
		c.emitCall(program, expr)
	default:
		program.instrs = append(program.instrs, instr{op: opLoadConst})
	}
}

func (c exprCompiler) emitRef(program *exprProgram, ref string) {
	if strings.HasPrefix(ref, "signal.") {
		program.instrs = append(program.instrs, instr{op: opLoadSignal, s: strings.TrimPrefix(ref, "signal.")})
		return
	}
	if strings.HasPrefix(ref, "output.") {
		program.instrs = append(program.instrs, instr{op: opLoadOutput, s: strings.TrimPrefix(ref, "output.")})
		return
	}
	if id, ok := c.atomIDs[ref]; ok {
		program.instrs = append(program.instrs, instr{op: opLoadAtom, a: uint32(id), ref: ref})
		return
	}
	root := contextFieldName(ref)
	if id, ok := c.fieldIDs[root]; ok {
		tail := strings.TrimPrefix(ref, root)
		tail = strings.TrimPrefix(tail, ".")
		program.instrs = append(program.instrs, instr{op: opLoadField, a: uint32(id), s: tail, ref: ref})
		return
	}
	program.instrs = append(program.instrs, instr{op: opLoadRef, ref: ref})
}

func (c exprCompiler) emitCall(program *exprProgram, expr *lang.Expr) {
	switch expr.Name {
	case "missing":
		if len(expr.Args) == 1 {
			c.emit(program, expr.Args[0])
			program.instrs = append(program.instrs, instr{op: opMissing})
			return
		}
	case "changed":
		if len(expr.Args) == 1 && expr.Args[0].Kind == lang.ExprRef {
			ref := contextFieldName(expr.Args[0].Name)
			if id, ok := c.fieldIDs[ref]; ok {
				program.instrs = append(program.instrs, instr{op: opChangedField, a: uint32(id)})
			} else {
				program.instrs = append(program.instrs, instr{op: opChangedRef, ref: expr.Args[0].Name})
			}
			return
		}
	case "list":
		for _, arg := range expr.Args {
			c.emit(program, arg)
		}
		program.instrs = append(program.instrs, instr{op: opList, n: len(expr.Args)})
		return
	case "map":
		for _, arg := range expr.Args {
			c.emit(program, arg)
		}
		program.instrs = append(program.instrs, instr{op: opMap, n: len(expr.Args)})
		return
	case "hash":
		for _, arg := range expr.Args {
			c.emit(program, arg)
		}
		program.instrs = append(program.instrs, instr{op: opHash, n: len(expr.Args)})
		return
	case "fixed", "exponential":
		for _, arg := range expr.Args {
			c.emit(program, arg)
		}
		program.instrs = append(program.instrs, instr{op: opPureCall, s: expr.Name, n: len(expr.Args)})
		return
	case "timer":
		program.instrs = append(program.instrs, instr{op: opLoadConst, value: expr.Value})
		return
	}
	for _, arg := range expr.Args {
		c.emit(program, arg)
	}
	program.instrs = append(program.instrs, instr{op: opPureCall, s: expr.Name, n: len(expr.Args)})
}

func binaryOp(op string) opCode {
	switch op {
	case "==":
		return opEq
	case "!=":
		return opNe
	case ">":
		return opGt
	case ">=":
		return opGe
	case "<":
		return opLt
	case "<=":
		return opLe
	case "in":
		return opIn
	case "and":
		return opAnd
	case "or":
		return opOr
	case "implies":
		return opImplies
	case "+":
		return opAdd
	case "-":
		return opSub
	case "*":
		return opMul
	case "/":
		return opDiv
	case "%":
		return opMod
	default:
		return opPureCall
	}
}

func (p *fastPlan) evalEnv(execution *Execution, dirty *Dirty) evalEnv {
	env := evalEnv{execution: execution, fieldIDs: p.fieldIDs}
	if dirty != nil {
		env.dirty = *dirty
	}
	return env
}

func (p *fastPlan) evalProgram(program *exprProgram, expr *lang.Expr, env evalEnv) (any, error) {
	if program == nil {
		return evalExpr(expr, env)
	}
	value, err := program.eval(env)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (p *fastPlan) evalBoolProgram(program *exprProgram, expr *lang.Expr, env evalEnv) (bool, error) {
	value, err := p.evalProgram(program, expr, env)
	if err != nil {
		return false, err
	}
	return truthy(value), nil
}

func (p *fastPlan) evalAllPrograms(programs []*exprProgram, exprs []*lang.Expr, env evalEnv) (bool, error) {
	for i, expr := range exprs {
		var program *exprProgram
		if i < len(programs) {
			program = programs[i]
		}
		ok, err := p.evalBoolProgram(program, expr, env)
		if err != nil || !ok {
			return ok, err
		}
	}
	return true, nil
}

func (p *fastPlan) compileExpr(expr *lang.Expr) *exprProgram {
	if p == nil {
		return nil
	}
	return p.compiler.compile(expr)
}

func loadField(env evalEnv, fieldID int, ref string, tail string) any {
	execution := env.execution
	if execution == nil {
		return nil
	}
	state := execution.RuntimeState
	if fieldID >= 0 && fieldID/64 < len(state.Present) && bitset(state.Present).has(fieldID) {
		var root any
		if value, ok := state.Values[uint32(fieldID)]; ok {
			root = value.Interface()
		} else {
			root = bitset(state.BoolValues).has(fieldID)
		}
		if tail == "" {
			return root
		}
		if object, ok := root.(map[string]any); ok {
			return resolvePath(object, tail)
		}
	}
	return resolveRef(ref, env)
}

func loadAtom(env evalEnv, atomID int, ref string) any {
	execution := env.execution
	if execution == nil {
		return nil
	}
	state := execution.RuntimeState
	if atomID >= 0 && atomID/64 < len(state.ActiveAtoms) {
		return bitset(state.ActiveAtoms).has(atomID)
	}
	return resolveRef(ref, env)
}

func evalVMBinary(op opCode, left any, right any) (any, error) {
	switch op {
	case opEq:
		return typedEqual(left, right), nil
	case opNe:
		return !typedEqual(left, right), nil
	case opGt:
		return compareNumbers(left, right, func(a, b float64) bool { return a > b })
	case opGe:
		return compareNumbers(left, right, func(a, b float64) bool { return a >= b })
	case opLt:
		return compareNumbers(left, right, func(a, b float64) bool { return a < b })
	case opLe:
		return compareNumbers(left, right, func(a, b float64) bool { return a <= b })
	case opIn:
		return containsTyped(right, left), nil
	case opAnd:
		return truthy(left) && truthy(right), nil
	case opOr:
		return truthy(left) || truthy(right), nil
	case opImplies:
		return !truthy(left) || truthy(right), nil
	case opAdd:
		return addValues(left, right)
	case opSub:
		return subtractValues(left, right)
	case opMul:
		return multiplyValues(left, right)
	case opDiv:
		return divideValues(left, right)
	case opMod:
		return moduloValues(left, right)
	default:
		return nil, fmt.Errorf("unsupported binary opcode %d", op)
	}
}

func typedEqual(left any, right any) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if lb, ok := left.(bool); ok {
		rb, rok := right.(bool)
		return rok && lb == rb
	}
	if ls, ok := left.(string); ok {
		rs, rok := right.(string)
		return rok && ls == rs
	}
	if la, ok := number(left); ok {
		rb, rok := number(right)
		return rok && la == rb
	}
	return reflect.DeepEqual(left, right)
}

func containsTyped(collection any, needle any) bool {
	switch values := collection.(type) {
	case []any:
		for _, value := range values {
			if typedEqual(value, needle) {
				return true
			}
		}
	case []string:
		for _, value := range values {
			if typedEqual(value, needle) {
				return true
			}
		}
	}
	return false
}
