// Command embedded demonstrates running an in-process, zero-dependency workflow
// using Axiom's Embedded integration profile.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/profile"
)

type CartState struct {
	Items []string `json:"items"`
	Total float64  `json:"total"`
}

type AddItemEvent struct {
	Item  string  `json:"item"`
	Price float64 `json:"price"`
}

func main() {
	// 1. Initialize Embedded profile
	emb := profile.NewEmbedded()
	defer emb.Close()

	// 2. Define Flow
	cartFlow := axiom.NewFlow("cart", CartState{Items: []string{}, Total: 0.0})
	axiom.Handle(cartFlow, func(ctx context.Context, s CartState, e AddItemEvent) (axiom.FlowResult[CartState], error) {
		s.Items = append(s.Items, e.Item)
		s.Total += e.Price
		return axiom.Next(s), nil
	})

	// 3. Open FlowEngine
	engine, err := profile.OpenEmbeddedFlow(emb, cartFlow)
	if err != nil {
		log.Fatalf("failed to open flow engine: %v", err)
	}

	ctx := context.Background()
	exec := engine.Execution("session-404")

	if err := exec.Dispatch(ctx, AddItemEvent{Item: "Espresso", Price: 3.50}); err != nil {
		log.Fatalf("dispatch: %v", err)
	}
	if err := exec.Dispatch(ctx, AddItemEvent{Item: "Croissant", Price: 4.00}); err != nil {
		log.Fatalf("dispatch: %v", err)
	}

	state, err := exec.State(ctx)
	if err != nil {
		log.Fatalf("get state: %v", err)
	}

	fmt.Printf("Cart items: %v | Total: $%.2f\n", state.Items, state.Total)
}
