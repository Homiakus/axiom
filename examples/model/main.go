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

func (UserRegistered) AxiomEventName() string { return "UserRegistered" }

func main() {
	definition := model.New("Welcome")
	user := model.State[User](definition, "User").Default("WelcomeSent", false)
	registered := model.Event[UserRegistered](definition, "UserRegistered")
	definition.Policy("emailPolicy").Retry(2).Timeout(5 * time.Second).Concurrency("once").Idempotency("required")
	definition.Activity("SendWelcomeEmail").Input("userId", user.Field("ID")).Input("email", user.Field("Email")).Output("sent", "Bool").Effect("external").IdempotencyKey(user.Field("ID")).Policy("emailPolicy")
	definition.Rule("captureRegistration").On(registered.Trigger()).Set(user.Field("ID"), registered.Field("UserID")).Set(user.Field("Email"), registered.Field("Email"))
	definition.Rule("sendWelcomeEmail").On(user.Changed("Email")).When(model.Eq(user.Field("WelcomeSent"), model.Lit(false))).Run("SendWelcomeEmail").Set(user.Field("WelcomeSent"), model.Ref("output.sent"))
	definition.Claim("welcomeSentRequiresEmail", model.Implies(model.Eq(user.Field("WelcomeSent"), model.Lit(true)), model.Exists(user.Field("Email"))))

	engine, err := axiom.Open(definition, axiom.Act("SendWelcomeEmail", func(context.Context, axiom.Input) (axiom.Output, error) { return axiom.Output{"sent": true}, nil }))
	if err != nil {
		log.Fatal(err)
	}
	if err := engine.Execution("user-1").Dispatch(context.Background(), UserRegistered{UserID: "user-1", Email: "user@example.com"}); err != nil {
		log.Fatal(err)
	}
}
