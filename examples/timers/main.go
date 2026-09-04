package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/model"
)

type OrderState struct {
	ExpiresAt string `json:"expiresAt"`
	Expired   bool   `json:"expired"`
}

func main() {
	definition := model.New("TimerExample")
	order := model.Bind[OrderState](definition, "Order")
	order.Default("ExpiresAt", "2000-01-01T00:00:00Z")
	order.Default("Expired", false)

	// Timer expressions are currently expressed through the AXM-compatible raw
	// trigger helper. The deadline itself remains a typed model field.
	definition.Rule("expire").
		On(model.OnTimer("Order.expiresAt")).
		Set(order.Bool("Expired"), true)

	engine, err := axiom.Open(definition)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	run := engine.Execution("order-42")
	if err := engine.Start(ctx, run.ID(), nil); err != nil {
		log.Fatal(err)
	}

	next, err := run.NextTimer(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("next timer: %s at %s\n", next.Rule, next.DueAt.Format(time.RFC3339))

	fired, err := run.RunDueTimers(ctx, time.Now())
	if err != nil {
		log.Fatal(err)
	}

	var state OrderState
	if err := run.State(ctx, &state); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("fired=%d expired=%v\n", fired, state.Expired)
}
