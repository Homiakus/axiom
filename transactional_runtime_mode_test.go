package axiom_test

import (
	"testing"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/model"
)

type arithmeticState struct {
	Left  int `json:"left"`
	Right int `json:"right"`
}

type arithmeticEvent struct {
	Value int `json:"value"`
}

func arithmeticPlan(t *testing.T) *axiom.Plan {
	t.Helper()
	definition := model.New("ArithmeticStoreMode")
	state := model.State[arithmeticState](definition, "State").
		Default("Left", 0).
		Default("Right", 0)
	event := model.Event[arithmeticEvent](definition, "Add")
	definition.Rule("add").
		On(event.Trigger()).
		When(model.GT(event.Field("Value"), model.Lit(0))).
		Set(state.Field("Left"), model.Raw("State.left + signal.value"))
	definition.Claim(
		"sumIsNonNegative",
		model.GTE(model.Raw("State.left + State.right"), model.Lit(0)),
	)
	plan, err := definition.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestPebbleDoesNotImplicitlyEnableStrictRuntime(t *testing.T) {
	plan := arithmeticPlan(t)
	store, err := axiom.OpenPebble(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := plan.New(axiom.WithStore(store)); err != nil {
		t.Fatalf("Pebble unexpectedly enabled strict runtime: %v", err)
	}
}

func TestStrictRuntimeRemainsExplicit(t *testing.T) {
	plan := arithmeticPlan(t)
	store, err := axiom.OpenPebble(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := plan.New(
		axiom.WithStore(store),
		axiom.WithStrictFastRuntime(),
	); err == nil {
		t.Fatal("strict runtime accepted an unsupported arithmetic condition")
	}
}
