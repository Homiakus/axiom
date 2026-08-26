package typedconv_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/Homiakus/axiom/internal/typedconv"
)

type SimpleUser struct {
	ID        int64  `axiom:"user_id"`
	Email     string `json:"email_address"`
	Age       int    `json:"age,omitempty"`
	Secret    string `json:"-"`
	Nickname  string
	IsActive  bool   `json:"is_active"`
	Score     float64 `json:"score"`
}

type NestedProfile struct {
	User     SimpleUser  `json:"user"`
	UserPtr  *SimpleUser `json:"user_ptr"`
	Tags     []string    `json:"tags"`
	Metadata map[string]int `json:"meta"`
}

type BaseEmbedded struct {
	BaseID string `json:"base_id"`
}

type ExtendedEmbedded struct {
	BaseEmbedded
	Role string `json:"role"`
}

func TestCompileInput_SimpleStruct(t *testing.T) {
	conv, err := typedconv.CompileInput[SimpleUser]()
	if err != nil {
		t.Fatalf("CompileInput failed: %v", err)
	}

	input := map[string]any{
		"user_id":       int64(9007199254740993),
		"email_address": "test@example.com",
		"age":           25,
		"Secret":        "hidden",
		"nickname":      "tester",
		"is_active":     true,
		"score":         98.5,
	}

	u, err := conv(input)
	if err != nil {
		t.Fatalf("conv failed: %v", err)
	}

	if u.ID != 9007199254740993 {
		t.Errorf("ID = %d, want 9007199254740993", u.ID)
	}
	if u.Email != "test@example.com" {
		t.Errorf("Email = %q, want test@example.com", u.Email)
	}
	if u.Age != 25 {
		t.Errorf("Age = %d, want 25", u.Age)
	}
	if u.Secret != "" {
		t.Errorf("Secret = %q, want empty (ignored)", u.Secret)
	}
	if u.Nickname != "tester" {
		t.Errorf("Nickname = %q, want tester", u.Nickname)
	}
	if !u.IsActive {
		t.Errorf("IsActive = false, want true")
	}
	if u.Score != 98.5 {
		t.Errorf("Score = %f, want 98.5", u.Score)
	}
}

func TestCompileInput_PointerStruct(t *testing.T) {
	conv, err := typedconv.CompileInput[*SimpleUser]()
	if err != nil {
		t.Fatalf("CompileInput failed: %v", err)
	}

	input := map[string]any{
		"user_id": int64(123),
	}

	u, err := conv(input)
	if err != nil {
		t.Fatalf("conv failed: %v", err)
	}
	if u == nil {
		t.Fatal("expected non-nil pointer")
	}
	if u.ID != 123 {
		t.Errorf("ID = %d, want 123", u.ID)
	}
}

func TestCompileInput_NestedAndCollections(t *testing.T) {
	conv, err := typedconv.CompileInput[NestedProfile]()
	if err != nil {
		t.Fatalf("CompileInput failed: %v", err)
	}

	input := map[string]any{
		"user": map[string]any{
			"user_id": int64(10),
		},
		"user_ptr": map[string]any{
			"user_id": int64(20),
		},
		"tags": []any{"admin", "ops"},
		"meta": map[string]any{
			"level": 5,
		},
	}

	prof, err := conv(input)
	if err != nil {
		t.Fatalf("conv failed: %v", err)
	}

	if prof.User.ID != 10 {
		t.Errorf("User.ID = %d, want 10", prof.User.ID)
	}
	if prof.UserPtr == nil || prof.UserPtr.ID != 20 {
		t.Errorf("UserPtr.ID = %v, want 20", prof.UserPtr)
	}
	if len(prof.Tags) != 2 || prof.Tags[0] != "admin" {
		t.Errorf("Tags = %v", prof.Tags)
	}
	if prof.Metadata["level"] != 5 {
		t.Errorf("Metadata.level = %d, want 5", prof.Metadata["level"])
	}
}

func TestCompileInput_EmbeddedStruct(t *testing.T) {
	conv, err := typedconv.CompileInput[ExtendedEmbedded]()
	if err != nil {
		t.Fatalf("CompileInput failed: %v", err)
	}

	input := map[string]any{
		"base_id": "b-100",
		"role":    "admin",
	}

	ext, err := conv(input)
	if err != nil {
		t.Fatalf("conv failed: %v", err)
	}

	if ext.BaseID != "b-100" {
		t.Errorf("BaseID = %q, want 'b-100'", ext.BaseID)
	}
	if ext.Role != "admin" {
		t.Errorf("Role = %q, want 'admin'", ext.Role)
	}
}

func TestCompileOutput_SimpleStruct(t *testing.T) {
	conv, err := typedconv.CompileOutput[SimpleUser]()
	if err != nil {
		t.Fatalf("CompileOutput failed: %v", err)
	}

	u := SimpleUser{
		ID:        1001,
		Email:     "u@ex.com",
		Age:       30,
		Secret:    "secret",
		Nickname:  "nick",
		IsActive:  true,
		Score:     4.5,
	}

	out, err := conv(u)
	if err != nil {
		t.Fatalf("conv failed: %v", err)
	}

	if out["user_id"] != int64(1001) {
		t.Errorf("user_id = %v, want 1001", out["user_id"])
	}
	if out["email_address"] != "u@ex.com" {
		t.Errorf("email_address = %v", out["email_address"])
	}
	if out["age"] != 30 {
		t.Errorf("age = %v", out["age"])
	}
	if _, exists := out["secret"]; exists {
		t.Errorf("secret must be ignored")
	}
	if out["nickname"] != "nick" {
		t.Errorf("nickname = %v", out["nickname"])
	}
}

func TestCompileOutput_EmbeddedStruct(t *testing.T) {
	conv, err := typedconv.CompileOutput[ExtendedEmbedded]()
	if err != nil {
		t.Fatalf("CompileOutput failed: %v", err)
	}

	ext := ExtendedEmbedded{
		BaseEmbedded: BaseEmbedded{BaseID: "b-200"},
		Role:         "manager",
	}

	out, err := conv(ext)
	if err != nil {
		t.Fatalf("conv failed: %v", err)
	}

	if out["base_id"] != "b-200" {
		t.Errorf("base_id = %v, want 'b-200'", out["base_id"])
	}
	if out["role"] != "manager" {
		t.Errorf("role = %v, want 'manager'", out["role"])
	}
}

func TestCompileOutput_NamedMap(t *testing.T) {
	type CustomMap map[string]int
	conv, err := typedconv.CompileOutput[CustomMap]()
	if err != nil {
		t.Fatalf("CompileOutput failed: %v", err)
	}

	m := CustomMap{"a": 1, "b": 2}
	out, err := conv(m)
	if err != nil {
		t.Fatalf("conv failed: %v", err)
	}

	if out["a"] != 1 || out["b"] != 2 {
		t.Errorf("out = %v", out)
	}
}

func TestCompileInput_LargeIntegerPrecision(t *testing.T) {
	type BigInt struct {
		BigVal int64 `json:"big_val"`
	}

	conv, err := typedconv.CompileInput[BigInt]()
	if err != nil {
		t.Fatalf("CompileInput failed: %v", err)
	}

	const expected = int64(1<<53 + 1)
	res, err := conv(map[string]any{
		"big_val": expected,
	})
	if err != nil {
		t.Fatalf("conv failed: %v", err)
	}

	if res.BigVal != expected {
		t.Errorf("BigVal = %d, want %d", res.BigVal, expected)
	}
}

func TestCompileInput_MaxInt64(t *testing.T) {
	type BigInt struct {
		BigVal int64 `json:"big_val"`
	}

	conv, err := typedconv.CompileInput[BigInt]()
	if err != nil {
		t.Fatalf("CompileInput failed: %v", err)
	}

	const expected = int64(math.MaxInt64)
	res, err := conv(map[string]any{
		"big_val": expected,
	})
	if err != nil {
		t.Fatalf("conv failed: %v", err)
	}

	if res.BigVal != expected {
		t.Errorf("BigVal = %d, want %d", res.BigVal, expected)
	}
}

func TestCompileInput_RejectsScalar(t *testing.T) {
	_, err := typedconv.CompileInput[int]()
	if err == nil {
		t.Fatal("expected error compiling scalar input")
	}
}

func TestCompileOutput_RejectsScalar(t *testing.T) {
	_, err := typedconv.CompileOutput[string]()
	if err == nil {
		t.Fatal("expected error compiling scalar output")
	}
}

func TestCompileInput_PlanCaching(t *testing.T) {
	conv1, err := typedconv.CompileInput[SimpleUser]()
	if err != nil {
		t.Fatal(err)
	}
	conv2, err := typedconv.CompileInput[SimpleUser]()
	if err != nil {
		t.Fatal(err)
	}

	if reflect.ValueOf(conv1).Pointer() == 0 || reflect.ValueOf(conv2).Pointer() == 0 {
		t.Fatal("expected valid function pointers")
	}
}
