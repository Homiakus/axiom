package adgo

import (
	"strings"
	"testing"
)

func TestChooseSpeculativeResultRejectsInvalidBudget(t *testing.T) {
	result, err := chooseSpeculativeResult([]variantResult{
		{
			name: "invalid",
			result: ActivityResult{
				Quality: QualityVector{"score": 1},
				Budget:  BudgetUsage{Cost: -1},
			},
		},
		{
			name: "valid",
			result: ActivityResult{
				Quality: QualityVector{"score": 1},
				Budget:  BudgetUsage{Cost: 1},
			},
		},
	}, 0)
	if err == nil {
		t.Fatal("expected invalid speculative budget to fail closed")
	}
	if !strings.Contains(err.Error(), `speculative variant "invalid" returned invalid budget`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Budget != (BudgetUsage{}) {
		t.Fatalf("invalid speculative result must not expose a partial budget: %+v", result.Budget)
	}
}
