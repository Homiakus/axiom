package compiler

import (
	"strings"
	"testing"
)

func TestCompileBuildsSymbolsAndIndexes(t *testing.T) {
	module, err := Compile([]byte(welcomeCompilerSource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if _, ok := module.Symbols["RegisteredUser.id"]; !ok {
		t.Fatalf("missing exposed field symbol")
	}
	if got := module.Indexes.SignalIndex["UserRegistered"]; len(got) != 1 || got[0] != "captureRegistration" {
		t.Fatalf("signal index = %#v", got)
	}
	if got := module.Indexes.ChangedIndex["User.email"]; len(got) != 1 || got[0] != "sendWelcomeEmail" {
		t.Fatalf("changed index = %#v", got)
	}
}

func TestCompileValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		code string
	}{
		{
			name: "bad output mapping",
			code: "AX302",
			src: `
domain InvalidWrite

signal Requested

context Risk:
  requested: Bool = false
  status: String = "unknown"

fact Ready when:
  Risk.requested

policy local:
  retry: 0
  timeout: 1s
  concurrency: once
  idempotency: none

activity CalculateRisk:
  require:
    Ready
  input:
    requested = Risk.requested
  output:
    status: String
  effect: none
  policy: local

rule badWrite:
  on Requested
  require:
    Ready
  run: CalculateRisk
  write:
    Risk.status = output.unknownStatus
`,
		},
		{
			name: "unsafe external activity",
			code: "AX305",
			src: `
domain UnsafePayment

context User:
  id: String?

fact AuthenticatedUser when:
  User.id exists

policy looseExternalCall:
  retry: 2
  timeout: 3s
  concurrency: latest
  idempotency: none

activity Pay:
  require:
    AuthenticatedUser
  input:
    userId = User.id
  output:
    status: String
  effect: external
  policy: looseExternalCall
`,
		},
		{
			name: "invalid catch target",
			code: "AX306",
			src: `
domain InvalidCatch

policy paymentCritical:
  retry: 0
  timeout: 10s
  concurrency: once
  idempotency: required
  catch:
    PaymentDeclined -> PaymentDeclinedReceived
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile([]byte(tt.src))
			if err == nil {
				t.Fatalf("Compile() expected error")
			}
			if !strings.Contains(err.Error(), tt.code) {
				t.Fatalf("Compile() error = %v, want code %s", err, tt.code)
			}
		})
	}
}

const welcomeCompilerSource = `
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
