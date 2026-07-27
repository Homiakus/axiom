package lang

import "testing"

const welcomeSource = `
domain Welcome

signal UserRegistered:
  userId: String
  email: String

context User:
  id: String?
  email: String?
  welcomeSent: Bool = false

computed userReady: Bool =
  User.id exists and User.email exists

fact RegisteredUser when:
  userReady
expose:
  id = User.id
  email = User.email

policy emailPolicy:
  retry: 2
  timeout: 5s
  concurrency: once
  idempotency: required

activity SendWelcomeEmail:
  require:
    RegisteredUser
  input:
    userId = RegisteredUser.id
    email = RegisteredUser.email
  output:
    sent: Bool
  effect: external
  idempotencyKey: RegisteredUser.id
  policy: emailPolicy

rule captureRegistration:
  on UserRegistered
  write:
    User.id = signal.userId
    User.email = signal.email

rule sendWelcomeEmail:
  on changed(User.email)
  when:
    User.welcomeSent == false
  require:
    RegisteredUser
  run: SendWelcomeEmail
  write:
    User.welcomeSent = output.sent

claim welcomeSentRequiresEmail:
  always:
    User.welcomeSent == true implies User.email exists
`

func TestParseWelcomeFlow(t *testing.T) {
	module, err := Parse([]byte(welcomeSource))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if module.Domain != "Welcome" {
		t.Fatalf("domain = %q", module.Domain)
	}
	if len(module.Signals) != 1 || len(module.Contexts) != 1 || len(module.Rules) != 2 {
		t.Fatalf("unexpected module shape: signals=%d contexts=%d rules=%d", len(module.Signals), len(module.Contexts), len(module.Rules))
	}
	if len(module.Facts[0].Expose) != 2 {
		t.Fatalf("expected fact expose bindings")
	}
}

func TestParseTimerRuleAndQuery(t *testing.T) {
	source := `
domain Reminder

context Order:
  id: String
  createdAt: Time

context Payment:
  status: String = "waiting"

fact PaymentWaiting when:
  Payment.status == "waiting"

policy emailPolicy:
  retry: 2
  timeout: 5s
  concurrency: once
  idempotency: required

activity SendPaymentReminder:
  require:
    PaymentWaiting
  input:
    orderId = Order.id
  output:
    sent: Bool
  effect: external
  idempotencyKey: hash(Order.id, "payment-reminder")
  policy: emailPolicy

rule sendPaymentReminder:
  on timer(24h after Order.createdAt)
  require:
    PaymentWaiting
  run: SendPaymentReminder
  write:
    Payment.status = "reminded"

query ReminderStatus:
  return:
    payment = Payment.status
    ready = PaymentWaiting
`
	module, err := Parse([]byte(source))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := module.Rules[0].Triggers[0].Kind; got != TriggerTimer {
		t.Fatalf("trigger kind = %s", got)
	}
	if len(module.Queries) != 1 {
		t.Fatalf("queries = %d", len(module.Queries))
	}
}
