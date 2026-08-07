package model

import (
	"strings"
	"testing"
)

type catchPaymentDeclined struct{}
type catchGenericFailure struct{}

func TestPolicyCatchHelpersRenderAndCompile(t *testing.T) {
	definition := New("CatchModel")
	Event[catchPaymentDeclined](definition, "PaymentDeclinedSignal")
	Event[catchGenericFailure](definition, "GenericFailureSignal")
	definition.Policy("payment").
		Retry(2).
		Catch("PaymentDeclined", "PaymentDeclinedSignal").
		CatchAll("GenericFailureSignal")

	source := definition.Source()
	for _, expected := range []string{
		"  catch:",
		"    * -> GenericFailureSignal",
		"    PaymentDeclined -> PaymentDeclinedSignal",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("Source() = %q, missing %q", source, expected)
		}
	}
	if _, err := definition.Compile(); err != nil {
		t.Fatalf("Compile() error = %v\nsource:\n%s", err, source)
	}
}

func TestPolicyCatchRejectsEmptyBuilderArguments(t *testing.T) {
	definition := New("CatchDiagnostics")
	definition.Policy("payment").Catch("", "Signal").Catch("PaymentDeclined", "")
	if _, err := definition.Compile(); err == nil {
		t.Fatal("Compile() succeeded, want builder diagnostics")
	}
}
