package main

import (
	"context"
	"log"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/model"
)

type User struct {
	ID          *string `json:"id"`
	Email       *string `json:"email"`
	WelcomeSent bool    `json:"welcomeSent"`
}

type UserRegistered struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
}

type SendWelcomeEmailInput struct {
	UserID *string `json:"userId"`
	Email  *string `json:"email"`
}

type SendWelcomeEmailOutput struct {
	Sent bool `json:"sent"`
}

func main() {
	definition := model.New("Welcome")
	user := model.Bind[User](definition, "User").Default("WelcomeSent", false)
	registered := model.EventOf[UserRegistered](definition)

	// External effects require an idempotency policy. Retry/timeout/concurrency
	// are intentionally not shown here because the current inline runtime does
	// not yet enforce them as complete runtime guarantees.
	definition.Policy("emailPolicy").Idempotency("required")

	definition.Activity("SendWelcomeEmail").
		Input("userId", user.String("ID")).
		Input("email", user.String("Email")).
		Output("sent", "Bool").
		Effect("external").
		IdempotencyKey(user.String("ID")).
		Policy("emailPolicy")

	definition.Rule("captureRegistration").
		On(registered.Trigger()).
		Set(user.String("ID"), registered.String("UserID")).
		Set(user.String("Email"), registered.String("Email"))

	definition.Rule("sendWelcomeEmail").
		On(user.Ref.Changed("Email")).
		When(user.Bool("WelcomeSent").Equal(false)).
		Run("SendWelcomeEmail").
		Set(user.Bool("WelcomeSent"), model.OutputBool("sent"))

	definition.Claim(
		"welcomeSentRequiresEmail",
		model.Implies(user.Bool("WelcomeSent").Equal(true), model.Exists(user.String("Email").Expr())),
	)

	engine, err := axiom.Open(
		definition,
		axiom.ActTyped("SendWelcomeEmail", func(_ context.Context, input SendWelcomeEmailInput) (SendWelcomeEmailOutput, error) {
			log.Printf("sending welcome email to %v for user %v", input.Email, input.UserID)
			return SendWelcomeEmailOutput{Sent: true}, nil
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	if err := engine.Execution("user-1").Dispatch(
		context.Background(),
		UserRegistered{UserID: "user-1", Email: "user@example.com"},
	); err != nil {
		log.Fatal(err)
	}
}
