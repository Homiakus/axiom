package model

import (
	"math"
	"strings"
	"testing"
)

func TestTryLitReturnsEncodingErrorImmediately(t *testing.T) {
	_, err := TryLit(math.Inf(1))
	if err == nil {
		t.Fatal("expected TryLit error for +Inf")
	}
	if !strings.Contains(err.Error(), "unsupported value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileReportsInvalidDefaultLiteralAsAX510(t *testing.T) {
	type StateValue struct {
		Value float64 `json:"value"`
	}

	definition := New("InvalidLiteral")
	state := State[StateValue](definition, "State")
	state.Default("Value", math.Inf(1))

	_, err := definition.Compile()
	if err == nil {
		t.Fatal("expected AX510 literal diagnostic")
	}
	text := err.Error()
	if !strings.Contains(text, "AX510") || !strings.Contains(text, "state.State.value.default") || !strings.Contains(text, "unsupported value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompilePreservesLiteralErrorThroughComposedExpression(t *testing.T) {
	definition := New("InvalidExpression")
	definition.Claim("finite", Eq(Int(1), Lit(math.Inf(1))))

	_, err := definition.Compile()
	if err == nil {
		t.Fatal("expected AX510 literal diagnostic")
	}
	text := err.Error()
	if !strings.Contains(text, "AX510") || !strings.Contains(text, "claim.finite[0]") || !strings.Contains(text, "unsupported value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileReportsUnsupportedLiteralType(t *testing.T) {
	definition := New("InvalidType")
	definition.Query("Bad", map[string]Expr{
		"value": Lit(make(chan int)),
	})

	_, err := definition.Compile()
	if err == nil {
		t.Fatal("expected AX510 literal diagnostic")
	}
	text := err.Error()
	if !strings.Contains(text, "AX510") || !strings.Contains(text, "query.Bad.value") || !strings.Contains(text, "unsupported type") {
		t.Fatalf("unexpected error: %v", err)
	}
}
