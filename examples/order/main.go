package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/model"
)

type Order struct {
	ID            *string `json:"id"`
	CustomerEmail *string `json:"customerEmail"`
	Total         int     `json:"total"`
	Paid          bool    `json:"paid"`
	PaymentID     *string `json:"paymentId"`
	ReceiptSent   bool    `json:"receiptSent"`
}

type OrderCreated struct {
	OrderID       string `json:"orderId"`
	CustomerEmail string `json:"customerEmail"`
	Total         int    `json:"total"`
}

func (OrderCreated) AxiomEventName() string { return "OrderCreated" }

type PaymentCaptured struct {
	PaymentID string `json:"paymentId"`
}

func (PaymentCaptured) AxiomEventName() string { return "PaymentCaptured" }

func main() {
	ctx := context.Background()

	definition := model.New("Order").Version("1")
	order := model.State[Order](definition, "Order").
		Default("Paid", false).
		Default("ReceiptSent", false)
	created := model.Event[OrderCreated](definition, "OrderCreated")
	captured := model.Event[PaymentCaptured](definition, "PaymentCaptured")

	definition.Policy("receiptPolicy").
		Retry(3).
		Timeout(3 * time.Second).
		Concurrency("once").
		Idempotency("required")

	definition.Activity("SendReceipt").
		Input("orderId", order.Field("ID")).
		Input("email", order.Field("CustomerEmail")).
		Input("paymentId", order.Field("PaymentID")).
		Output("sent", "Bool").
		Effect("external").
		IdempotencyKey(order.Field("PaymentID")).
		Policy("receiptPolicy")

	definition.Rule("createOrder").
		On(created.Trigger()).
		Set(order.Field("ID"), created.Field("OrderID")).
		Set(order.Field("CustomerEmail"), created.Field("CustomerEmail")).
		Set(order.Field("Total"), created.Field("Total"))

	definition.Rule("capturePayment").
		On(captured.Trigger()).
		Set(order.Field("PaymentID"), captured.Field("PaymentID")).
		Set(order.Field("Paid"), model.Lit(true))

	definition.Rule("sendReceipt").
		On(order.Changed("Paid")).
		When(model.Eq(order.Field("Paid"), model.Lit(true))).
		Run("SendReceipt").
		Set(order.Field("ReceiptSent"), model.Ref("output.sent"))

	definition.Claim(
		"paidOrderHasPaymentID",
		model.Implies(
			model.Eq(order.Field("Paid"), model.Lit(true)),
			model.Exists(order.Field("PaymentID")),
		),
	)
	definition.Claim(
		"receiptRequiresPayment",
		model.Implies(
			model.Eq(order.Field("ReceiptSent"), model.Lit(true)),
			model.Eq(order.Field("Paid"), model.Lit(true)),
		),
	)

	plan, err := definition.Compile()
	if err != nil {
		log.Fatal(err)
	}

	dir, err := os.MkdirTemp("", "axiom-order-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := axiom.OpenPebble(filepath.Join(dir, "orders"))
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	engine, err := plan.New(
		axiom.WithStore(store),
		axiom.Act("SendReceipt", func(_ context.Context, input axiom.Input) (axiom.Output, error) {
			fmt.Printf("receipt sent to %v for order %v\n", input["email"], input["orderId"])
			return axiom.Output{"sent": true}, nil
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	run := engine.Execution("order-42")
	if err := run.Dispatch(ctx, OrderCreated{
		OrderID:       "order-42",
		CustomerEmail: "customer@example.com",
		Total:         12900,
	}); err != nil {
		log.Fatal(err)
	}
	if err := run.Dispatch(ctx, PaymentCaptured{PaymentID: "pay-9001"}); err != nil {
		log.Fatal(err)
	}

	var current Order
	if err := run.State(ctx, &current); err != nil {
		log.Fatal(err)
	}
	history, err := run.History(ctx)
	if err != nil {
		log.Fatal(err)
	}
	replayed, err := axiom.ReplayFromHistory(plan.Module(), history)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("paid=%t receiptSent=%t history=%d replayStatus=%s\n",
		current.Paid,
		current.ReceiptSent,
		len(history),
		replayed.Status,
	)
}
