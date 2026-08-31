package typedconv

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

type namedUint64 uint64

type narrowNumericInput struct {
	Signed   int8   `axiom:"signed"`
	Unsigned uint8  `axiom:"unsigned"`
	Float    float32 `axiom:"float"`
}

func TestConvertValueRejectsIntegerRangeCrossings(t *testing.T) {
	tests := []struct {
		name   string
		raw    any
		target reflect.Type
	}{
		{name: "uint64_to_int64", raw: uint64(1) << 63, target: reflect.TypeFor[int64]()},
		{name: "named_uint64_to_int64", raw: namedUint64(uint64(1) << 63), target: reflect.TypeFor[int64]()},
		{name: "negative_to_uint64", raw: int64(-1), target: reflect.TypeFor[uint64]()},
		{name: "int16_to_int8", raw: int16(128), target: reflect.TypeFor[int8]()},
		{name: "uint16_to_uint8", raw: uint16(256), target: reflect.TypeFor[uint8]()},
		{name: "positive_inf_to_int64", raw: math.Inf(1), target: reflect.TypeFor[int64]()},
		{name: "int64_upper_exclusive", raw: math.Exp2(63), target: reflect.TypeFor[int64]()},
		{name: "uint64_upper_exclusive", raw: math.Exp2(64), target: reflect.TypeFor[uint64]()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := convertValue(tt.raw, tt.target); err == nil {
				t.Fatalf("expected %T(%v) -> %s to fail", tt.raw, tt.raw, tt.target)
			}
		})
	}
}

func TestConvertValuePreservesValidNumericBoundaries(t *testing.T) {
	value, err := convertValue(uint64(math.MaxInt64), reflect.TypeFor[int64]())
	if err != nil {
		t.Fatal(err)
	}
	if got := value.Int(); got != math.MaxInt64 {
		t.Fatalf("got %d, want %d", got, int64(math.MaxInt64))
	}

	value, err = convertValue(int64(255), reflect.TypeFor[uint8]())
	if err != nil {
		t.Fatal(err)
	}
	if got := value.Uint(); got != 255 {
		t.Fatalf("got %d, want 255", got)
	}
}

func TestCompileInputRejectsNarrowNumericOverflow(t *testing.T) {
	convert, err := CompileInput[narrowNumericInput]()
	if err != nil {
		t.Fatal(err)
	}
	_, err = convert(map[string]any{"signed": int16(128), "unsigned": uint16(1), "float": float64(1)})
	if err == nil || !strings.Contains(err.Error(), "overflows int8") {
		t.Fatalf("expected int8 overflow error, got %v", err)
	}
	_, err = convert(map[string]any{"signed": int16(1), "unsigned": uint16(256), "float": float64(1)})
	if err == nil || !strings.Contains(err.Error(), "overflows uint8") {
		t.Fatalf("expected uint8 overflow error, got %v", err)
	}
}
