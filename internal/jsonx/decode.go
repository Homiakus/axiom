// Package jsonx contains JSON boundary helpers used by runtime and stores.
package jsonx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	jsonNumberType = reflect.TypeOf(json.Number(""))
	timeType       = reflect.TypeOf(time.Time{})
)

// Decode behaves like encoding/json.Unmarshal but preserves integral JSON
// numbers as Go int/int64 values inside interface-typed fields.
func Decode(data []byte, target any) error {
	if target == nil {
		return fmt.Errorf("jsonx: target is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("jsonx: target must be a non-nil pointer")
	}
	normalized, err := normalize(value.Elem())
	if err != nil {
		return err
	}
	value.Elem().Set(normalized)
	return nil
}

func normalize(value reflect.Value) (reflect.Value, error) {
	if !value.IsValid() {
		return value, nil
	}
	if value.Type() == jsonNumberType {
		return parseNumber(value.Interface().(json.Number))
	}
	if value.Type() == timeType {
		return value, nil
	}

	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		normalized, err := normalize(value.Elem())
		if err != nil {
			return reflect.Value{}, err
		}
		wrapped := reflect.New(value.Type()).Elem()
		wrapped.Set(normalized)
		return wrapped, nil
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		normalized, err := normalize(value.Elem())
		if err != nil {
			return reflect.Value{}, err
		}
		pointer := reflect.New(value.Type().Elem())
		pointer.Elem().Set(normalized)
		return pointer, nil
	case reflect.Struct:
		result := reflect.New(value.Type()).Elem()
		for index := 0; index < value.NumField(); index++ {
			destination := result.Field(index)
			if !destination.CanSet() || !value.Field(index).CanInterface() {
				continue
			}
			normalized, err := normalize(value.Field(index))
			if err != nil {
				return reflect.Value{}, err
			}
			if normalized.IsValid() && normalized.Type().AssignableTo(destination.Type()) {
				destination.Set(normalized)
			}
		}
		return result, nil
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			normalized, err := normalize(iterator.Value())
			if err != nil {
				return reflect.Value{}, err
			}
			if normalized.Type().AssignableTo(value.Type().Elem()) {
				result.SetMapIndex(iterator.Key(), normalized)
				continue
			}
			if value.Type().Elem().Kind() == reflect.Interface {
				result.SetMapIndex(iterator.Key(), normalized)
				continue
			}
			return reflect.Value{}, fmt.Errorf("jsonx: normalized %s is not assignable to %s", normalized.Type(), value.Type().Elem())
		}
		return result, nil
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			normalized, err := normalize(value.Index(index))
			if err != nil {
				return reflect.Value{}, err
			}
			if normalized.Type().AssignableTo(result.Index(index).Type()) {
				result.Index(index).Set(normalized)
			} else if result.Index(index).Kind() == reflect.Interface {
				result.Index(index).Set(normalized)
			}
		}
		return result, nil
	case reflect.Array:
		result := reflect.New(value.Type()).Elem()
		for index := 0; index < value.Len(); index++ {
			normalized, err := normalize(value.Index(index))
			if err != nil {
				return reflect.Value{}, err
			}
			if normalized.Type().AssignableTo(result.Index(index).Type()) {
				result.Index(index).Set(normalized)
			}
		}
		return result, nil
	default:
		return value, nil
	}
}

func parseNumber(number json.Number) (reflect.Value, error) {
	text := number.String()
	if strings.ContainsAny(text, ".eE") {
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(parsed), nil
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return reflect.Value{}, err
	}
	if strconv.IntSize == 64 || (parsed >= -1<<31 && parsed <= 1<<31-1) {
		return reflect.ValueOf(int(parsed)), nil
	}
	return reflect.ValueOf(parsed), nil
}
