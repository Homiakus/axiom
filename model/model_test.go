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
