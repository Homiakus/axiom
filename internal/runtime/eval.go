package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
		a, aok := number(left)
		b, bok := number(right)
		if !aok || !bok {
			return nil, fmt.Errorf("operator - requires numbers")
		}
		if isIntLike(left) && isIntLike(right) {
			return int(a - b), nil
		}
		return a - b, nil
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
		return resolvePath(env.signal, strings.TrimPrefix(ref, "signal."))
	}
	if strings.HasPrefix(ref, "output.") {
		return resolvePath(env.output, strings.TrimPrefix(ref, "output."))
	}
	if strings.HasPrefix(ref, "runtime.") {
		return nil
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
	parts := strings.Split(ref, ".")
	if len(parts) >= 2 {
		if fact, ok := env.execution.Facts[parts[0]]; ok {
			return resolvePath(fact.Exposed, strings.Join(parts[1:], "."))
		}
		if ctx, ok := env.execution.Context[parts[0]]; ok {
			return resolvePath(ctx, strings.Join(parts[1:], "."))
		}
	}
	return nil
}

func resolvePath(root map[string]any, path string) any {
	if root == nil || path == "" {
		return nil
	}
	parts := strings.Split(path, ".")
	var current any = root
	for _, part := range parts {
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
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	default:
		return true
	}
}

func compareNumbers(left any, right any, cmp func(float64, float64) bool) (bool, error) {
	a, aok := number(left)
	b, bok := number(right)
	if !aok || !bok {
		return false, fmt.Errorf("comparison requires numbers")
	}
	return cmp(a, b), nil
}

func number(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func isIntLike(value any) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64:
		return true
	default:
		return false
	}
}

func addValues(left any, right any) (any, error) {
	a, aok := number(left)
	b, bok := number(right)
	if aok && bok {
		if isIntLike(left) && isIntLike(right) {
			return int(a + b), nil
		}
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
