package model

import (
	"strings"
	"testing"
	"time"
)

type policyTestState struct {
	Done bool `json:"done"`
}

type policyTestEvent struct {
	ID string `json:"id"`
}

func TestPolicyUsesCanonicalSyntaxAndCompiles(t *testing.T) {
	definition := New("PolicyModel")
	state := State[policyTestState](definition, "State").Default("Done", false)
	event := Event[policyTestEvent](definition, "Run")

	definition.Policy("hardware").
		Retry(2).
		Timeout(time.Second).
		Concurrency("once").
		Idempotency("required")

	definition.Activity("DoWork").
		Input("id", event.Field("ID")).
		Output("done", "Bool").
		Effect("external").
		IdempotencyKey(event.Field("ID")).
		Policy("hardware")

	definition.Rule("run").
		On(event.Trigger()).
		Run("DoWork").
		Set(state.Field("Done"), Ref("output.done"))

	source := definition.Source()
	for _, expected := range []string{
		"  concurrency: once",
		"  idempotency: required",
		"  retry: 2",
		"  timeout: 1s",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("generated source does not contain %q:\n%s", expected, source)
		}
	}
	if strings.Contains(source, "concurrency = once") {
		t.Fatalf("policy was rendered as an expression map:\n%s", source)
	}

	if _, err := definition.Compile(); err != nil {
		t.Fatalf("compile generated policy: %v\n%s", err, source)
	}
}

func TestArithmeticAndTypedConstructors(t *testing.T) {
	exprAdd := Add(Ref("A"), Int(10))
	if exprAdd.String() != "(A + 10)" {
		t.Fatalf("Add got %q, want (A + 10)", exprAdd.String())
	}

	exprSub := Sub(Ref("B"), Float(2.5))
	if exprSub.String() != "(B - 2.5)" {
		t.Fatalf("Sub got %q, want (B - 2.5)", exprSub.String())
	}

	exprMul := Mul(Ref("C"), Int64(5))
	if exprMul.String() != "(C * 5)" {
		t.Fatalf("Mul got %q, want (C * 5)", exprMul.String())
	}

	exprDiv := Div(Ref("D"), Int(2))
	if exprDiv.String() != "(D / 2)" {
		t.Fatalf("Div got %q, want (D / 2)", exprDiv.String())
	}

	exprMod := Mod(Ref("E"), Int(3))
	if exprMod.String() != "(E % 3)" {
		t.Fatalf("Mod got %q, want (E %% 3)", exprMod.String())
	}

	exprStr := String("hello")
	if exprStr.String() != `"hello"` {
		t.Fatalf("String got %q, want \"hello\"", exprStr.String())
	}

	exprBool := Bool(true)
	if exprBool.String() != "true" {
		t.Fatalf("Bool got %q, want true", exprBool.String())
	}
}
