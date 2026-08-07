package adgo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrometheusHandlerUsesCommittedFleetState(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	first := &Execution{ID: "m-1", PlanID: "p", PlanVersion: "1", PlanDigest: "d", Version: 1, Status: StatusRunning, BudgetUsage: BudgetUsage{Cost: 1.5, Tokens: 10}}
	ensureExecution(first)
	first.ActiveTasks["t"] = TaskRuntime{ID: "t", NodeID: "n", Status: TaskPending}
	second := &Execution{ID: "m-2", PlanID: "p", PlanVersion: "1", PlanDigest: "d", Version: 1, Status: StatusCompleted, BudgetUsage: BudgetUsage{Cost: .5, Tokens: 5}}
	ensureExecution(second)
	if err := store.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, second); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	PrometheusHandler(store).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`adgo_executions{status="completed"} 1`,
		`adgo_executions{status="running"} 1`,
		`adgo_executions_total 2`,
		`adgo_active_tasks 1`,
		`adgo_budget_cost 2`,
		`adgo_budget_tokens 15`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q:\n%s", want, body)
		}
	}
}
