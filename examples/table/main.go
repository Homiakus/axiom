package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/table"
)

type UserRegistered struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
}

type User struct {
	ID          *string `json:"id"`
	Email       *string `json:"email"`
	WelcomeSent bool    `json:"welcomeSent"`
}

type SendWelcomeEmailInput struct {
	UserID *string `json:"userId"`
	Email  *string `json:"email"`
}

type SendWelcomeEmailOutput struct {
	Sent bool `json:"sent"`
}

func main() {
	ctx := context.Background()
	plan, err := table.Load(filepath.Join("examples", "table", "welcome.toml"))
	if err != nil {
		log.Fatal(err)
	}

	engine, err := plan.New(
		axiom.ActTyped("SendWelcomeEmail", func(_ context.Context, input SendWelcomeEmailInput) (SendWelcomeEmailOutput, error) {
			fmt.Printf("send welcome email to %v\n", input.Email)
			return SendWelcomeEmailOutput{Sent: true}, nil
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	run := engine.Execution("user-1")
	if err := run.Dispatch(ctx, UserRegistered{UserID: "user-1", Email: "user@example.com"}); err != nil {
		log.Fatal(err)
	}

	var state User
	if err := run.State(ctx, &state); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("welcomeSent=%t\n", state.WelcomeSent)
}
