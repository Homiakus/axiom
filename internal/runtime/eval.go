package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Homiakus/axiom/internal/lang"
)

type evalEnv struct {
	execution *Execution
	signal    map[string]any
	output    map[string]any
	changed   map[string]struct{}
	dirty     Dirty
	fieldIDs  map[string]int
}

func evalExpr(expr *lang.Expr, env evalEnv) (any, error) {
	if expr == nil {
		return nil, nil
	}
	switch expr.Kind {
	case lang.ExprLiteral:
		return expr.Value, nil
	case lang.ExprRef:
		return resolveRef(expr.Name, env), nil
	case lang.ExprUnary:
		value, err := evalExpr(expr.Left, env)
		if err != nil {
			return nil, err
		}
		switch expr.Op {
		case "exists":
			return exists(value), nil
		case "not":
			return !truthy(value), nil
		case "-":
			return negateValue(value)
		default:
			return nil, fmt.Errorf("unsupported unary operator %s", expr.Op)
		}
	case lang.ExprBinary:
		return evalBinary(expr, env)
	case lang.ExprCall:
		return evalCall(expr, env)
	default:
		return nil, fmt.Errorf("unsupported expression kind %s", expr.Kind)
	}
}

func evalBool(expr *lang.Expr, env evalEnv) (bool, error) {
	value, err := evalExpr(expr, env)
	if err != nil {
		return false, err
	}
	return truthy(value), nil
}

func evalAll(exprs []*lang.Expr, env evalEnv) (bool, error) {
	for _, expr := range exprs {
		ok, err := evalBool(expr, env)
		if err != nil || !ok {
			return ok, err
		}
	}
	return true, nil
}

func evalBinary(expr *lang.Expr, env evalEnv) (any, error) {
	switch expr.Op {
	case "and":
		left, err := evalBool(expr.Left, env)
		if err != nil || !left {
			return left, err
		}
		return evalBool(expr.Right, env)
	case "or":
		left, err := evalBool(expr.Left, env)
		if err != nil || left {
			return left, err
		}
		return evalBool(expr.Right, env)
	case "implies":
		left, err := evalBool(expr.Left, env)
		if err != nil {
			return false, err
		}
		if !left {
			return true, nil
		}
		return evalBool(expr.Right, env)
	}
	left, err := evalExpr(expr.Left, env)
	if err != nil {
		return nil, err
	}
	right, err := evalExpr(expr.Right, env)
	if err != nil {
		return nil, err
	}
	switch expr.Op {
	case "==":
		return typedEqual(left, right), nil
	case "!=":
		return !typedEqual(left, right), nil
	case ">":
		return compareNumbers(left, right, func(a, b float64) bool { return a > b })
	case ">=":
		return compareNumbers(left, right, func(a, b float64) bool { return a >= b })
	case "<":
		return compareNumbers(left, right, func(a, b float64) bool { return a < b })
	case "<=":
		return compareNumbers(left, right, func(a, b float64) bool { return a <= b })
	case "in":
		return containsTyped(right, left), nil
	case "+":
		return addValues(left, right)
	case "-":
		return subtractValues(left, right)
	case "*":
		return multiplyValues(left, right)
	case "/":
		return divideValues(left, right)
	case "%":
		return moduloValues(left, right)
	default:
		return nil, fmt.Errorf("unsupported binary operator %s", expr.Op)
	}
}

func evalCall(expr *lang.Expr, env evalEnv) (any, error) {
	switch expr.Name {
	case "list":
		values := make([]any, 0, len(expr.Args))
		for _, arg := range expr.Args {
			value, err := evalExpr(arg, env)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	case "map":
		values := map[string]any{}
		for i := 0; i+1 < len(expr.Args); i += 2 {
			key, err := evalExpr(expr.Args[i], env)
			if err != nil {
				return nil, err
			}
			value, err := evalExpr(expr.Args[i+1], env)
			if err != nil {
				return nil, err
			}
			values[fmt.Sprint(key)] = value
		}
		return values, nil
	case "missing":
		if len(expr.Args) != 1 {
			return nil, fmt.Errorf("missing() expects one argument")
		}
		value, err := evalExpr(expr.Args[0], env)
		if err != nil {
			return nil, err
		}
		return !exists(value), nil
	case "changed":
		if len(expr.Args) != 1 || expr.Args[0].Kind != lang.ExprRef {
			return nil, fmt.Errorf("changed() expects a reference")
		}
		if env.fieldIDs != nil && env.dirty.Fields != nil {
			if fieldID, ok := env.fieldIDs[contextFieldName(expr.Args[0].Name)]; ok {
				return env.dirty.Fields.has(fieldID), nil
			}
		}
		_, ok := env.changed[expr.Args[0].Name]
		return ok, nil
	case "hash":
		values := make([]any, 0, len(expr.Args))
		for _, arg := range expr.Args {
			value, err := evalExpr(arg, env)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		data, err := json.Marshal(values)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	case "fixed", "exponential":
		values := make([]string, 0, len(expr.Args))
		for _, arg := range expr.Args {
			value, err := evalExpr(arg, env)
			if err != nil {
				return nil, err
			}
			values = append(values, fmt.Sprint(value))
		}
		return expr.Name + "(" + strings.Join(values, ",") + ")", nil
	case "timer":
		return expr.Value, nil
	default:
		return nil, fmt.Errorf("unsupported pure call %s", expr.Name)
	}
}

func resolveRef(ref string, env evalEnv) any {
	if ref == "" {
		return nil
	}
	if strings.HasPrefix(ref, "signal.") {
		return resolvePath(env.signal, ref[7:])
	}
	if strings.HasPrefix(ref, "output.") {
		return resolvePath(env.output, ref[7:])
	}
	if strings.HasPrefix(ref, "runtime.") {
		return resolveRuntimeRef(env.execution, strings.TrimPrefix(ref, "runtime."))
	}
	if env.execution == nil {
		return nil
	}
	if value, ok := env.execution.Computed[ref]; ok {
		return value
	}
	if fact, ok := env.execution.Facts[ref]; ok {
		return fact.True
	}
	dot := strings.IndexByte(ref, '.')
	if dot >= 0 {
		rootName := ref[:dot]
		fieldPath := ref[dot+1:]
		if fact, ok := env.execution.Facts[rootName]; ok {
			return resolvePath(fact.Exposed, fieldPath)
		}
		if ctx, ok := env.execution.Context[rootName]; ok {
			return resolvePath(ctx, fieldPath)
		}
	}
	return nil
}

func resolvePath(root map[string]any, path string) any {
	if root == nil || path == "" {
		return nil
	}
	var current any = root
	remaining := path
	for remaining != "" {
		var part string
		if idx := strings.IndexByte(remaining, '.'); idx >= 0 {
			part = remaining[:idx]
			remaining = remaining[idx+1:]
		} else {
			part = remaining
			remaining = ""
		}
		switch value := current.(type) {
		case map[string]any:
			current = value[part]
		case map[any]any:
			current = value[part]
		case []any:
			if part == "length" {
				current = len(value)
				continue
			}
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(value) {
				return nil
			}
			current = value[idx]
		case []string:
			if part == "length" {
				current = len(value)
				continue
			}
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(value) {
				return nil
			}
			current = value[idx]
		default:
			return nil
		}
	}
	return current
}

func exists(value any) bool {
	return value != nil
}

func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case nil:
		return false
	case string:
		return v != ""
	}
	if integer, ok := signedInteger(value); ok {
		return integer != 0
	}
	if number, ok := floatingNumber(value); ok {
		return number != 0
	}
	return true
}

func compareNumbers(left any, right any, cmp func(float64, float64) bool) (bool, error) {
	if a, aok := signedInteger(left); aok {
		if b, bok := signedInteger(right); bok {
			switch {
			case a < b:
				return cmp(-1, 0), nil
			case a > b:
				return cmp(1, 0), nil
			default:
				return cmp(0, 0), nil
			}
		}
	}
	a, aok := number(left)
	b, bok := number(right)
	if !aok || !bok {
		return false, fmt.Errorf("comparison requires numbers")
	}
	return cmp(a, b), nil
}

func signedInteger(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	default:
		return 0, false
	}
}

func floatingNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float32:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func number(value any) (float64, bool) {
	if v, ok := signedInteger(value); ok {
		return float64(v), true
	}
	return floatingNumber(value)
}

func isIntLike(value any) bool {
	_, ok := signedInteger(value)
	return ok
}

func integerResult(value int64) any {
	if strconv.IntSize == 64 || (value >= -1<<31 && value <= 1<<31-1) {
		return int(value)
	}
	return value
}

func safeAddInt64(a, b int64) (int64, bool) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, false
	}
	if b < 0 && a < math.MinInt64-b {
		return 0, false
	}
	return a + b, true
}

func safeSubInt64(a, b int64) (int64, bool) {
	if b > 0 && a < math.MinInt64+b {
		return 0, false
	}
	if b < 0 && a > math.MaxInt64+b {
		return 0, false
	}
	return a - b, true
}

func safeMulInt64(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if (a == math.MinInt64 && b == -1) || (b == math.MinInt64 && a == -1) {
		return 0, false
	}
	result := a * b
	if result/b != a {
		return 0, false
	}
	return result, true
}

func addValues(left any, right any) (any, error) {
	if a, aok := signedInteger(left); aok {
		if b, bok := signedInteger(right); bok {
			result, ok := safeAddInt64(a, b)
			if !ok {
				return nil, fmt.Errorf("integer overflow in operator +")
			}
			return integerResult(result), nil
		}
	}
	a, aok := number(left)
	b, bok := number(right)
	if aok && bok {
		return a + b, nil
	}
	if s, ok := left.(string); ok {
		return s + fmt.Sprint(right), nil
	}
	if s, ok := right.(string); ok {
		return fmt.Sprint(left) + s, nil
	}
	return nil, fmt.Errorf("operator + requires numbers or strings")
}

func negateValue(value any) (any, error) {
	if integer, ok := signedInteger(value); ok {
		if integer == math.MinInt64 {
			return nil, fmt.Errorf("integer overflow in unary -")
		}
		return integerResult(-integer), nil
	}
	n, ok := floatingNumber(value)
	if !ok {
		return nil, fmt.Errorf("unary - requires a number")
	}
	return -n, nil
}

func subtractValues(left any, right any) (any, error) {
	if a, aok := signedInteger(left); aok {
		if b, bok := signedInteger(right); bok {
			result, ok := safeSubInt64(a, b)
			if !ok {
				return nil, fmt.Errorf("integer overflow in operator -")
			}
			return integerResult(result), nil
		}
	}
	a, aok := number(left)
	b, bok := number(right)
	if !aok || !bok {
		return nil, fmt.Errorf("operator - requires numbers")
	}
	return a - b, nil
}

func multiplyValues(left any, right any) (any, error) {
	if a, aok := signedInteger(left); aok {
		if b, bok := signedInteger(right); bok {
			result, ok := safeMulInt64(a, b)
			if !ok {
				return nil, fmt.Errorf("integer overflow in operator *")
			}
			return integerResult(result), nil
		}
	}
	a, aok := number(left)
	b, bok := number(right)
	if !aok || !bok {
		return nil, fmt.Errorf("operator * requires numbers")
	}
	return a * b, nil
}

func divideValues(left any, right any) (any, error) {
	if a, aok := signedInteger(left); aok {
		if b, bok := signedInteger(right); bok {
			if b == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			if a == math.MinInt64 && b == -1 {
				return nil, fmt.Errorf("integer overflow in operator /")
			}
			return integerResult(a / b), nil
		}
	}
	a, aok := number(left)
	b, bok := number(right)
	if !aok || !bok {
		return nil, fmt.Errorf("operator / requires numbers")
	}
	if b == 0 {
		return nil, fmt.Errorf("division by zero")
	}
	return a / b, nil
}

func moduloValues(left any, right any) (any, error) {
	if a, aok := signedInteger(left); aok {
		if b, bok := signedInteger(right); bok {
			if b == 0 {
				return nil, fmt.Errorf("modulo by zero")
			}
			return integerResult(a % b), nil
		}
	}
	a, aok := number(left)
	b, bok := number(right)
	if !aok || !bok {
		return nil, fmt.Errorf("operator %% requires numbers")
	}
	if b == 0 {
		return nil, fmt.Errorf("modulo by zero")
	}
	return math.Mod(a, b), nil
}
