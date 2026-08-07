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
	Status      string  `json:"status"`
	Total       int     `json:"total"`
	PaymentID   *string `json:"paymentId"`
	ReceiptSent bool    `json:"receiptSent"`
}

type OrderCreated struct {
	Total int `json:"total"`
}

type PaymentCaptured struct {
	PaymentID string `json:"paymentId"`
}

type SendReceiptInput struct {
	PaymentID *string `json:"paymentId"`
}

type SendReceiptOutput struct {
	Sent bool `json:"sent"`
}

var (
	orderStatus      = model.Key[Order, string]("Status")
	orderTotal       = model.Key[Order, int]("Total")
	orderPaymentID   = model.Key[Order, string]("PaymentID")
	orderReceiptSent = model.Key[Order, bool]("ReceiptSent")

	createdTotal      = model.Key[OrderCreated, int]("Total")
	capturedPaymentID = model.Key[PaymentCaptured, string]("PaymentID")
)

func main() {
	ctx := context.Background()
	definition := buildModel()

	dataDir, err := os.MkdirTemp("", "axiom-order-*")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	store, err := axiom.OpenPebble(filepath.Join(dataDir, "store"))
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	engine, err := axiom.Open(
		definition,
		axiom.WithStore(store),
		axiom.WithProductionMode(),
		axiom.ActTyped("SendReceipt", func(_ context.Context, input SendReceiptInput) (SendReceiptOutput, error) {
			if input.PaymentID == nil || *input.PaymentID == "" {
				return SendReceiptOutput{}, fmt.Errorf("payment id is required")
			}
			fmt.Printf("send receipt for %s\n", *input.PaymentID)
			return SendReceiptOutput{Sent: true}, nil
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	run := engine.Execution("order-42")
	if err := run.Dispatch(ctx, OrderCreated{Total: 1599}); err != nil {
		log.Fatal(err)
	}
	if err := run.Dispatch(ctx, PaymentCaptured{PaymentID: "pay-42"}); err != nil {
		log.Fatal(err)
	}

	var state Order
	if err := run.State(ctx, &state); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("status=%s total=%d receiptSent=%t\n", state.Status, state.Total, state.ReceiptSent)
}

func buildModel() *model.Definition {
	definition := model.New("OrderLifecycle").Version("1")
	order := model.Bind[Order](definition, "Order")
	created := model.EventOf[OrderCreated](definition)
	captured := model.EventOf[PaymentCaptured](definition)

	model.StateDefault(order, orderStatus, "new")
	model.StateDefault(order, orderTotal, 0)
	model.StateDefault(order, orderReceiptSent, false)

	status := model.StateField(order, orderStatus)
	total := model.StateField(order, orderTotal)
	paymentID := model.StateField(order, orderPaymentID)
	receiptSent := model.StateField(order, orderReceiptSent)
	incomingTotal := model.EventField(created, createdTotal)
	incomingPaymentID := model.EventField(captured, capturedPaymentID)

	definition.Policy("receiptPolicy").
		Retry(3).
		Timeout(3 * time.Second).
		Concurrency("once").
		Idempotency("required")

	definition.Activity("SendReceipt").
		Input("paymentId", paymentID).
		Output("sent", "Bool").
		Effect("external").
		IdempotencyKey(paymentID).
		Policy("receiptPolicy")

	definition.Rule("createOrder").
		On(created.Trigger()).
		When(incomingTotal.GreaterThan(0)).
		Set(total, incomingTotal).
		Set(status, "pending")

	definition.Rule("capturePayment").
		On(captured.Trigger()).
		When(status.Equal("pending")).
		Set(paymentID, incomingPaymentID).
		Set(status, "paid")

	definition.Rule("sendReceipt").
		On(model.StateChanged(order, orderStatus)).
		When(status.Equal("paid"), receiptSent.Equal(false)).
		Run("SendReceipt").
		Set(receiptSent, model.OutputBool("sent"))

	definition.Claim(
		"paidRequiresPayment",
		model.Implies(status.Equal("paid"), model.Exists(paymentID.Expr())),
	)
	definition.Claim(
		"receiptRequiresPaidOrder",
		model.Implies(receiptSent.Equal(true), status.Equal("paid")),
	)

	return definition
}
