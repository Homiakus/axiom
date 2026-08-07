package main

import (
	"context"
	"log"
	"time"

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

var (
	userID          = model.Key[User, string]("ID")
	userEmail       = model.Key[User, string]("Email")
	userWelcomeSent = model.Key[User, bool]("WelcomeSent")

	registeredUserID = model.Key[UserRegistered, string]("UserID")
	registeredEmail  = model.Key[UserRegistered, string]("Email")
)

func main() {
	ctx := context.Background()
	definition := model.New("Welcome")
	user := model.Bind[User](definition, "User")
	registered := model.EventOf[UserRegistered](definition)

	// Field keys keep names in one place. Their owner/value type is checked when
	// the model uses them, while json/axiom tags still control serialized names.
	model.StateDefault(user, userWelcomeSent, false)
	id := model.StateField(user, userID)
	email := model.StateField(user, userEmail)
	welcomeSent := model.StateField(user, userWelcomeSent)
	incomingID := model.EventField(registered, registeredUserID)
	incomingEmail := model.EventField(registered, registeredEmail)

	// External effects require idempotency. Retry and timeout are durable
	// runtime guarantees; concurrency "once" is serialized within one Engine.
	definition.Policy("emailPolicy").
		Retry(3).
		Timeout(5 * time.Second).
		Concurrency("once").
		Idempotency("required")

	definition.Activity("SendWelcomeEmail").
		Input("userId", id).
		Input("email", email).
		Output("sent", "Bool").
		Effect("external").
		IdempotencyKey(id).
		Policy("emailPolicy")

	definition.Rule("captureRegistration").
		On(registered.Trigger()).
		Set(id, incomingID).
		Set(email, incomingEmail)

	definition.Rule("sendWelcomeEmail").
		On(model.StateChanged(user, userEmail)).
		When(welcomeSent.Equal(false)).
		Run("SendWelcomeEmail").
		Set(welcomeSent, model.OutputBool("sent"))

	definition.Claim(
		"welcomeSentRequiresEmail",
		model.Implies(welcomeSent.Equal(true), model.Exists(email.Expr())),
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

	run := engine.Execution("user-1")
	if err := run.Dispatch(ctx, UserRegistered{UserID: "user-1", Email: "user@example.com"}); err != nil {
		log.Fatal(err)
	}

	var state User
	if err := run.State(ctx, &state); err != nil {
		log.Fatal(err)
	}
	log.Printf("welcome sent: %v", state.WelcomeSent)
}
