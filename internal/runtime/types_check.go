package runtime

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/Homiakus/axiom/internal/diag"
	"github.com/Homiakus/axiom/internal/lang"
)

func (e *Engine) checkContextValue(target string, value any) error {
	field := contextFieldName(target)
	symbol, ok := e.module.Symbols[field]
	if !ok {
		return diag.Error{Code: "AX405", Kind: "runtime", Entity: field, Message: fmt.Sprintf("unknown context field %s", field), Hint: "Use a context field declared in the .axm module."}
	}
	if target != field && acceptsNestedValue(symbol.Type) {
		return nil
	}
	if !valueMatchesType(value, symbol.Type) {
		return diag.Error{Code: "AX406", Kind: "runtime", Entity: field, Message: fmt.Sprintf("type mismatch for %s: got %T, want %s", field, value, symbol.Type), Hint: "Make the patch, initial context, or rule write value match the declared field type and the supported numeric range."}
	}
	return nil
}

func acceptsNestedValue(typeName string) bool {
	base := strings.TrimSuffix(typeName, "?")
	return base == "Object" || strings.HasPrefix(base, "Map<")
}

func valueMatchesType(value any, typeName string) bool {
	nullable := strings.HasSuffix(typeName, "?")
	base := strings.TrimSuffix(typeName, "?")
	if value == nil {
		return nullable
	}
	switch base {
	case "String", "Time":
		_, ok := value.(string)
		return ok
	case "Int":
		return isRuntimeInt(value)
	case "Float":
		_, ok := number(value)
		return ok
	case "Bool":
		_, ok := value.(bool)
		return ok
	case "Duration":
		_, ok := value.(lang.DurationLiteral)
		if ok {
			return true
		}
		_, ok = value.(string)
		return ok
	case "Object":
		_, ok := value.(map[string]any)
		return ok
	default:
		if strings.HasPrefix(base, "List<") {
			return isSlice(value)
		}
		if strings.HasPrefix(base, "Map<") {
			_, ok := value.(map[string]any)
			return ok
		}
		return true
	}
}

func isRuntimeInt(value any) bool {
	_, ok := signedInteger(value)
	return ok
}

func isSlice(value any) bool {
	kind := reflect.TypeOf(value).Kind()
	return kind == reflect.Slice || kind == reflect.Array
}
