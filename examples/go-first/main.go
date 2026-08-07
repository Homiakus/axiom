package main

import (
	"context"
	"fmt"
	"log"

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
	ctx := context.Background()

	flow := axiom.NewFlow("counter", State{})
	axiom.Handle(flow, func(_ context.Context, state State, event Increment) (axiom.FlowResult[State], error) {
		state.Count += event.By
		return axiom.Next(state, axiom.Call(LogCount{Count: state.Count})), nil
	})
	axiom.EffectHandler(flow, func(_ context.Context, command LogCount) error {
		fmt.Printf("effect count=%d\n", command.Count)
		return nil
	})

	engine, err := axiom.OpenFlow(flow)
	if err != nil {
		log.Fatal(err)
	}

	run := engine.Execution("counter-1")
	if err := run.Dispatch(ctx, Increment{By: 2}); err != nil {
		log.Fatal(err)
	}

	state, err := run.State(ctx)
	if err != nil {
		log.Fatal(err)
	}
	history, err := run.History(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("state count=%d history=%d\n", state.Count, len(history))
}
