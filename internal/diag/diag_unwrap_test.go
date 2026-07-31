package diag

import (
	"errors"
	"testing"
)

func TestErrorsUnwrapsAllCauses(t *testing.T) {
	first := errors.New("first")
	second := errors.New("second")
	aggregated := Errors{
		{Code: "AX001", Cause: first},
		{Code: "AX002", Cause: second},
	}
	if !errors.Is(aggregated, first) {
		t.Fatal("errors.Is did not find the first cause")
	}
	if !errors.Is(aggregated, second) {
		t.Fatal("errors.Is did not find the second cause")
	}
}
