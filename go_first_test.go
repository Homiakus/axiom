package axiom_test

import (
	"context"
	"testing"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/model"
	"github.com/Homiakus/axiom/table"
)

type counterState struct {
	Count int `json:"count"`
}
type increment struct {
	By int `json:"by"`
}

func TestGoFirstFlow(t *testing.T) {
	flow := axiom.NewFlow("counter", counterState{})
	axiom.Handle(flow, func(_ context.Context, state counterState, event increment) (axiom.FlowResult[counterState], error) {
		state.Count += event.By
		return axiom.Next(state), nil
	})
	engine, err := axiom.OpenFlow(flow)
	if err != nil {
		t.Fatal(err)
	}
	run := engine.Execution("one")
	if err := run.Dispatch(context.Background(), increment{By: 3}); err != nil {
		t.Fatal(err)
	}
	state, err := run.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Count != 3 {
		t.Fatalf("count = %d", state.Count)
	}
}

func TestModelBuilderCompiles(t *testing.T) {
	type state struct {
		Value int `json:"value"`
	}
	type event struct {
		Value int `json:"value"`
	}
	definition := model.New("Builder")
	current := model.State[state](definition, "Current")
	incoming := model.Event[event](definition, "SetValue")
	definition.Rule("set").On(incoming.Trigger()).Set(current.Field("Value"), incoming.Field("Value"))
	if _, err := definition.Compile(); err != nil {
		t.Fatal(err)
	}
}

func TestTOMLCompiles(t *testing.T) {
	source := []byte(`[workflow]
name = "Table"
[state.Counter]
value = 0
[[event]]
name = "Increment"
[event.fields]
by = "Int"
[[transition]]
name = "increment"
on = "Increment"
[transition.set]
"Counter.value" = "Counter.value + signal.by"
`)
	if _, err := table.Parse(source); err != nil {
		t.Fatal(err)
	}
}
