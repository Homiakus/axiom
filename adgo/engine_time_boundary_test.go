package adgo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/durabletime"
)

type boundaryAdvanceScheduler struct {
	delegate Scheduler
	advance  func()
}

func (s *boundaryAdvanceScheduler) Select(plan *Plan, execution *Execution, candidates []Candidate) []Candidate {
	if s.advance != nil {
		advance := s.advance
		s.advance = nil
		advance()
	}
	return s.delegate.Select(plan, execution, candidates)
}

func TestAdvanceDoesNotDeadlockWhenRetryDeadlineCrossesDecisionBoundary(t *testing.T) {
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	clock := durabletime.NewManualClock(start)
	plan, err := Compile(Definition{
		ID:      "retry-boundary",
		Version: "1",
		Nodes: []Node{{
			ID:       "work",
			Kind:     NodeActivity,
			Activity: "work",
			Retry: RetryPolicy{
				MaxAttempts:      2,
				BaseDelay:        time.Second,
				MaxDelay:         time.Second,
				MaxRetryDuration: time.Minute,
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	store := NewMemoryStore()
	engine, err := NewEngine(plan, store, NewRegistry(), WithEngineClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := engine.Start(ctx, "retry-boundary-1", nil, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(ctx, "retry-boundary-1"); err != nil {
		t.Fatal(err)
	}
	item, err := engine.Poll(ctx, WorkerSpec{ID: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Fail(ctx, item.Token, Fail(FailureTransient, errors.New("temporary")), 0); err != nil {
		t.Fatal(err)
	}

	execution, err := store.Load(ctx, "retry-boundary-1")
	if err != nil {
		t.Fatal(err)
	}
	wantNotBefore := start.Add(time.Second)
	if !execution.Nodes["work"].NotBefore.Equal(wantNotBefore) {
		t.Fatalf("NotBefore=%v want=%v", execution.Nodes["work"].NotBefore, wantNotBefore)
	}

	engine.scheduler = &boundaryAdvanceScheduler{
		delegate: DefaultScheduler(),
		advance: func() {
			if err := clock.Advance(2 * time.Second); err != nil {
				t.Fatalf("advance clock: %v", err)
			}
		},
	}

	result, err := engine.Advance(ctx, "retry-boundary-1")
	if errors.Is(err, ErrDeadlock) {
		t.Fatalf("retry crossing NotBefore during one Advance was committed as deadlock: %+v", result)
	}
	if err != nil {
		t.Fatal(err)
	}
	execution, err = store.Load(ctx, "retry-boundary-1")
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != StatusWaiting {
		t.Fatalf("status=%s want=%s", execution.Status, StatusWaiting)
	}
	if len(result.QueuedTasks) != 0 {
		t.Fatalf("unexpected queue before next decision step: %+v", result.QueuedTasks)
	}

	engine.scheduler = DefaultScheduler()
	result, err = engine.Advance(ctx, "retry-boundary-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.QueuedTasks) != 1 {
		t.Fatalf("queued=%v want one retry task", result.QueuedTasks)
	}
}
