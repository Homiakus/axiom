package axiom_test

import (
	"strings"
	"testing"

	"github.com/Homiakus/axiom"
)

// ──────────────────────────────────────────────────────────────────────────────
// TestBundleComprehensive tests module governance: CompileBundle, Diff,
// Impact analysis, and Compatibility validation.
// ──────────────────────────────────────────────────────────────────────────────

const bundleBaseSource = `domain GovernanceBase

signal CreateUser:
  id: String
  email: String

context User:
  id: String?
  email: String?

computed hasUser: Bool =
  User.id exists

fact UserExists when:
  hasUser

policy defaultPolicy:
  retry: 1
  timeout: 5s
  idempotency: required

activity SendNotification:
  require:
    UserExists
  input:
    id = User.id
  output:
    sent: Bool
  effect: external
  idempotencyKey: User.id
  policy: defaultPolicy

rule onCreateUser:
  on CreateUser
  write:
    User.id = signal.id
    User.email = signal.email

rule notifyOnUser:
  on changed(User.id)
  run: SendNotification
  write:
    User.id = User.id

claim userIdNonEmpty:
  always:
    User.id exists
`

func TestCompileBundleStructure(t *testing.T) {
	bundle, err := axiom.CompileBundle([]byte(bundleBaseSource))
	if err != nil {
		t.Fatalf("CompileBundle() error = %v", err)
	}

	if bundle.SourceHash == "" {
		t.Fatal("SourceHash is empty")
	}
	if bundle.CompiledHash == "" {
		t.Fatal("CompiledHash is empty")
	}
	if len(bundle.Activities) != 1 || bundle.Activities[0] != "SendNotification" {
		t.Fatalf("Activities = %#v", bundle.Activities)
	}
	if len(bundle.ContextFields) != 2 {
		t.Fatalf("ContextFields = %#v", bundle.ContextFields)
	}
	if len(bundle.Rules) != 2 {
		t.Fatalf("Rules = %#v", bundle.Rules)
	}
	if len(bundle.Claims) != 1 || bundle.Claims[0] != "userIdNonEmpty" {
		t.Fatalf("Claims = %#v", bundle.Claims)
	}
}

func TestBundleDiffAdditive(t *testing.T) {
	base, err := axiom.CompileBundle([]byte(bundleBaseSource))
	if err != nil {
		t.Fatal(err)
	}

	// Add a new context field and a new rule.
	additiveSource := bundleBaseSource + "\ncontext Extra:\n  flag: Bool = true\n\nrule extraRule:\n  on CreateUser\n  write:\n    Extra.flag = true\n"
	additive, err := axiom.CompileBundle([]byte(additiveSource))
	if err != nil {
		t.Fatal(err)
	}

	diff := base.Diff(additive)
	if len(diff.AddedFields) != 1 || diff.AddedFields[0] != "Extra.flag" {
		t.Fatalf("AddedFields = %#v", diff.AddedFields)
	}
	if len(diff.AddedRules) != 1 || diff.AddedRules[0] != "extraRule" {
		t.Fatalf("AddedRules = %#v", diff.AddedRules)
	}
	if len(diff.RemovedFields) != 0 || len(diff.RemovedActivities) != 0 {
		t.Fatalf("unexpected removals in additive diff: %#v", diff)
	}
}

func TestBundleDiffSubtractive(t *testing.T) {
	base, err := axiom.CompileBundle([]byte(bundleBaseSource))
	if err != nil {
		t.Fatal(err)
	}

	// Remove rule notifyOnUser from source.
	subtractiveSource := strings.Replace(bundleBaseSource, "rule notifyOnUser:", "# removed rule notifyOnUser:", 1)
	subtractive, err := axiom.CompileBundle([]byte(subtractiveSource))
	if err != nil {
		t.Fatal(err)
	}

	diff := base.Diff(subtractive)
	if base.CompiledHash == subtractive.CompiledHash {
		t.Fatal("hashes should differ after removing rule")
	}
	if len(diff.RemovedRules) != 1 || diff.RemovedRules[0] != "notifyOnUser" {
		t.Fatalf("RemovedRules = %#v", diff.RemovedRules)
	}
}

func TestBundleImpactAnalysis(t *testing.T) {
	bundle, err := axiom.CompileBundle([]byte(bundleBaseSource))
	if err != nil {
		t.Fatal(err)
	}

	impact := bundle.Impact([]string{"User.id"})
	if len(impact.Fields) != 1 || impact.Fields[0] != "User.id" {
		t.Fatalf("Impact.Fields = %#v", impact.Fields)
	}
	// User.id should impact rules that depend on or are triggered by it.
	if len(impact.Rules) == 0 {
		t.Fatal("expected User.id change to impact rules")
	}
	if len(impact.Activities) == 0 {
		t.Fatal("expected User.id change to impact activities")
	}
}

func TestBundleCompatibilityValidation(t *testing.T) {
	base, err := axiom.CompileBundle([]byte(bundleBaseSource))
	if err != nil {
		t.Fatal(err)
	}

	// Additive change is compatible.
	additiveSource := bundleBaseSource + "\ncontext Extra:\n  x: Int = 0\n"
	additive, err := axiom.CompileBundle([]byte(additiveSource))
	if err != nil {
		t.Fatal(err)
	}
	if err := additive.ValidateCompatibility(base); err != nil {
		t.Fatalf("additive change failed compatibility: %v", err)
	}

	// Removing an activity is a breaking change.
	smallerSource := strings.ReplaceAll(bundleBaseSource, "SendNotification", "AnotherNotification")
	smaller, err := axiom.CompileBundle([]byte(smallerSource))
	if err != nil {
		t.Fatalf("CompileBundle(smallerSource) error = %v", err)
	}
	err = smaller.ValidateCompatibility(base)
	if err == nil {
		t.Fatal("expected breaking change error when previous bundle has removed activity")
	}
}
