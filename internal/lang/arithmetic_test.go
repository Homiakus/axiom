package lang

import "testing"

func TestArithmeticPrecedence(t *testing.T) {
	expr, err := ParseExpr("1 + 2 * 3")
	if err != nil {
		t.Fatal(err)
	}
	if expr.Kind != ExprBinary || expr.Op != "+" {
		t.Fatalf("root = %#v, want binary +", expr)
	}
	if expr.Right == nil || expr.Right.Kind != ExprBinary || expr.Right.Op != "*" {
		t.Fatalf("right = %#v, want binary *", expr.Right)
	}
}

func TestMultiplicativeOperatorsAreLeftAssociative(t *testing.T) {
	expr, err := ParseExpr("8 / 2 % 3")
	if err != nil {
		t.Fatal(err)
	}
	if expr.Kind != ExprBinary || expr.Op != "%" {
		t.Fatalf("root = %#v, want binary %%", expr)
	}
	if expr.Left == nil || expr.Left.Kind != ExprBinary || expr.Left.Op != "/" {
		t.Fatalf("left = %#v, want binary /", expr.Left)
	}
}

func TestUnaryMinusBindsBeforeMultiplication(t *testing.T) {
	expr, err := ParseExpr("-State.value * 2")
	if err != nil {
		t.Fatal(err)
	}
	if expr.Kind != ExprBinary || expr.Op != "*" {
		t.Fatalf("root = %#v, want binary *", expr)
	}
	if expr.Left == nil || expr.Left.Kind != ExprUnary || expr.Left.Op != "-" {
		t.Fatalf("left = %#v, want unary -", expr.Left)
	}
}
