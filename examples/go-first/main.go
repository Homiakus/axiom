package main

import (
	"context"
	"fmt"

	"github.com/Homiakus/axiom"
)

type State struct {
	Count int `json:"count"`
}
type Increment struct {
	By int `json:"by"`
}
type LogCount struct{ Count int }

func main() {
	flow := axiom.NewFlow("counter", State{})
	axiom.Handle(flow, func(_ context.Context, state State, event Increment) (axiom.FlowResult[State], error) {
		state.Count += event.By
		return axiom.Next(state, axiom.Call(LogCount{Count: state.Count})), nil
	})
	axiom.EffectHandler(flow, func(_ context.Context, command LogCount) error {
		fmt.Println(command.Count)
		return nil
	})
	engine, _ := axiom.OpenFlow(flow)
	_ = engine.Execution("counter-1").Dispatch(context.Background(), Increment{By: 2})
}
