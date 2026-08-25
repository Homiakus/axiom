package adgo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/durabletime"
)

func TestParallelSuperStep(t *testing.T) {
	plan, must := Compile(Definition{ID: "parallel", Version: "1", GlobalConcurrency: 4, Nodes: []Node{
		{ID: "start", Kind: NodeActivity, Activity: "start", Next: []Transition{{To: "left"}, {To: "right"}}},
		{ID: "left", Kind: NodeActivity, Activity: "parallel", DependsOn: []string{"start"}, Next: []Transition{{To: "join"}}},
		{ID: "right", Kind: NodeActivity, Activity: "parallel", DependsOn: []string{"start"}, Next: []Transition{{To: "join"}}},
		{ID: "join", Kind: NodeJoin, DependsOn: []string{"left", "right"}, Join: &JoinSpec{Mode: JoinAll}},
	}})
	if must != nil {
		t.Fatal(must)
	}
	reg := NewRegistry()
	reg.Activity("start", func(context.Context, ActivityRequest) (ActivityResult, error) { return ActivityResult{}, nil })
	var active, maxActive atomic.Int32
	reg.Activity("parallel", func(context.Context, ActivityRequest) (ActivityResult, error) {
		n := active.Add(1)
		for {
			m := maxActive.Load()
			if n <= m || maxActive.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return ActivityResult{}, nil
	})
	rt, _ := NewRuntime(plan, NewMemoryStore(), reg)
	if _, err := rt.Start(context.Background(), "e1", nil, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	e, err := rt.Run(context.Background(), "e1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusCompleted {
		t.Fatalf("status=%s", e.Status)
	}
	if maxActive.Load() < 2 {
		t.Fatalf("expected parallel execution, max active=%d", maxActive.Load())
	}
}

func TestHumanInterruptSurvivesSignal(t *testing.T) {
	plan, err := Compile(Definition{ID: "human", Version: "1", Nodes: []Node{
		{ID: "prepare", Kind: NodeActivity, Activity: "prepare", Next: []Transition{{To: "approve"}}},
		{ID: "approve", Kind: NodeHuman, DependsOn: []string{"prepare"}, Human: &HumanSpec{EventType: "Approved", Risk: RiskHigh}, Next: []Transition{{To: "finish"}}},
		{ID: "finish", Kind: NodeActivity, Activity: "finish", DependsOn: []string{"approve"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	reg.Activity("prepare", noopActivity)
	reg.Activity("finish", noopActivity)
	store := NewMemoryStore()
	rt, _ := NewRuntime(plan, store, reg)
	_, _ = rt.Start(context.Background(), "h1", nil, BudgetLimit{})
	e, err := rt.Run(context.Background(), "h1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusHuman {
		t.Fatalf("want human, got %s", e.Status)
	}
	if err := rt.Signal(context.Background(), "h1", Event{ID: "approve-1", Type: "Approved", TargetNode: "approve"}); err != nil {
		t.Fatal(err)
	}
	e, err = rt.Run(context.Background(), "h1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusCompleted {
		t.Fatalf("want completed, got %s", e.Status)
	}
}

func TestTargetedRepairPreservesUnrelatedWork(t *testing.T) {
	bound := &LoopBound{MaxIterations: 3, MaxCost: 10, MaxDuration: time.Minute, Epsilon: .0001}
	plan, err := Compile(Definition{ID: "repair", Version: "1", GlobalConcurrency: 3, Nodes: []Node{
		{ID: "draft", Kind: NodeActivity, Activity: "draft", Produces: []string{"draftVersion"}, Loop: bound, Next: []Transition{{To: "verify"}, {To: "editorial"}}},
		{ID: "verify", Kind: NodeActivity, Activity: "verify", DependsOn: []string{"draft"}, Requires: []string{"draftVersion"}, Next: []Transition{{To: "gate"}}},
		{ID: "editorial", Kind: NodeActivity, Activity: "editorial", DependsOn: []string{"draft"}},
		{ID: "gate", Kind: NodeGate, DependsOn: []string{"verify"}, Gate: &QualityGateSpec{HardFloors: map[string]float64{"factuality": .95}, RepairFrom: []string{"draft"}}, Next: []Transition{{To: "done", Outcome: OutcomePass}}},
		{ID: "done", Kind: NodeActivity, Activity: "done", DependsOn: []string{"gate"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	var drafts, editorials atomic.Int32
	reg.Activity("draft", func(context.Context, ActivityRequest) (ActivityResult, error) {
		v := drafts.Add(1)
		return ActivityResult{Facts: map[string]any{"draftVersion": int(v)}}, nil
	})
	reg.Activity("verify", func(_ context.Context, req ActivityRequest) (ActivityResult, error) {
		var v int
		_ = json.Unmarshal(req.Data["draftVersion"], &v)
		q := .80
		if v >= 2 {
			q = .98
		}
		return ActivityResult{Quality: QualityVector{"factuality": q}}, nil
	})
	reg.Activity("editorial", func(context.Context, ActivityRequest) (ActivityResult, error) {
		editorials.Add(1)
		return ActivityResult{}, nil
	})
	reg.Activity("done", noopActivity)
	rt, _ := NewRuntime(plan, NewMemoryStore(), reg)
	_, _ = rt.Start(context.Background(), "r1", nil, BudgetLimit{})
	e, err := rt.Run(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusCompleted {
		t.Fatalf("status=%s reason=%s", e.Status, e.Failure)
	}
	if drafts.Load() != 2 {
		t.Fatalf("expected 2 drafts, got %d", drafts.Load())
	}
	if editorials.Load() != 1 {
		t.Fatalf("editorial should be preserved, ran %d", editorials.Load())
	}
	if e.Metrics.Repairs != 1 {
		t.Fatalf("repairs=%d", e.Metrics.Repairs)
	}
}

func TestTransientRetry(t *testing.T) {
	plan, err := Compile(Definition{ID: "retry", Version: "1", Nodes: []Node{{ID: "call", Kind: NodeActivity, Activity: "call", Retry: RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxRetryDuration: time.Second}}}})
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	var calls atomic.Int32
	reg.Activity("call", func(context.Context, ActivityRequest) (ActivityResult, error) {
		if calls.Add(1) < 3 {
			return ActivityResult{}, Fail(FailureTransient, errors.New("temporary"))
		}
		return ActivityResult{}, nil
	})
	store := NewMemoryStore()
	rt, _ := NewRuntime(plan, store, reg)
	_, _ = rt.Start(context.Background(), "retry-1", nil, BudgetLimit{})
	for i := 0; i < 10; i++ {
		e, _ := store.Load(context.Background(), "retry-1")
		if terminal(e.Status) {
			break
		}
		_, err := rt.Step(context.Background(), "retry-1")
		if err != nil && !errors.Is(err, ErrDeadlock) {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	e, _ := store.Load(context.Background(), "retry-1")
	if e.Status != StatusCompleted {
		t.Fatalf("status=%s failure=%s", e.Status, e.Failure)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d", calls.Load())
	}
	if e.Metrics.Retries != 2 {
		t.Fatalf("retries=%d", e.Metrics.Retries)
	}
}

func TestFileStoreRecoveryOfExpiredLease(t *testing.T) {
	plan, err := Compile(Definition{ID: "recover", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	reg.Activity("work", noopActivity)
	rt, _ := NewRuntime(plan, store, reg, WithLeaseTTL(time.Millisecond))
	e, err := rt.Start(context.Background(), "d1", nil, BudgetLimit{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Commit(context.Background(), e.ID, e.Version, func(x *Execution) error {
		x.Nodes["work"].Status = NodeRunning
		x.ActiveTasks["old"] = TaskRuntime{ID: "old", NodeID: "work", Activity: "work", Status: TaskRunning, LeaseUntil: time.Now().Add(-time.Second)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	rt2, _ := NewRuntime(plan, reopened, reg)
	e, err = rt2.Run(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusCompleted {
		t.Fatalf("status=%s", e.Status)
	}
	if e.Metrics.RecoveryEvents != 1 {
		t.Fatalf("recovery events=%d", e.Metrics.RecoveryEvents)
	}
}

func TestArtifactStoreDeduplicates(t *testing.T) {
	s, err := NewContentAddressedStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Put("a.txt", "text/plain", strings.NewReader("same"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Put("b.txt", "text/plain", strings.NewReader("same"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("digest mismatch %s %s", a.Digest, b.Digest)
	}
	if !s.Exists(a) {
		t.Fatal("artifact missing")
	}
}

func noopActivity(context.Context, ActivityRequest) (ActivityResult, error) {
	return ActivityResult{}, nil
}

func TestDurableTimerResumes(t *testing.T) {
	const waitDuration = 5 * time.Second
	plan, err := Compile(Definition{ID: "timer", Version: "1", Nodes: []Node{
		{ID: "wait", Kind: NodeWait, Wait: &WaitSpec{Duration: waitDuration}, Next: []Transition{{To: "done"}}},
		{ID: "done", Kind: NodeActivity, Activity: "done", DependsOn: []string{"wait"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	reg.Activity("done", noopActivity)
	store := NewMemoryStore()
	start := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	clock := durabletime.NewManualClock(start)
	rt, err := NewRuntime(plan, store, reg, WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	created, err := rt.Start(context.Background(), "timer-1", nil, BudgetLimit{})
	if err != nil {
		t.Fatal(err)
	}
	if !created.CreatedAt.Equal(start) {
		t.Fatalf("created at %v, want injected clock time %v", created.CreatedAt, start)
	}

	e, err := rt.Run(context.Background(), "timer-1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusWaiting {
		t.Fatalf("expected waiting, got %s", e.Status)
	}
	deadline := start.Add(waitDuration)
	if got := e.Nodes["wait"].NotBefore; !got.Equal(deadline) {
		t.Fatalf("timer deadline %v, want %v", got, deadline)
	}

	if err := clock.Advance(waitDuration - time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	e, err = rt.Run(context.Background(), "timer-1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusWaiting {
		t.Fatalf("expected waiting before deadline, got %s", e.Status)
	}

	if err := clock.Advance(time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	e, err = rt.Run(context.Background(), "timer-1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusCompleted {
		t.Fatalf("expected completed at deadline, got %s", e.Status)
	}
}

func TestHighRiskExternalEffectRequiresApproval(t *testing.T) {
	plan, err := Compile(Definition{ID: "approval", Version: "1", Nodes: []Node{{
		ID: "publish", Kind: NodeActivity, Activity: "publish", ExternalEffect: true, Irreversible: true,
		Risk: RiskHigh, Timeout: time.Second, IdempotencyKey: "{execution}:{node}:{revision}",
		Retry: RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxRetryDuration: time.Second},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	reg := NewRegistry()
	reg.Activity("publish", func(context.Context, ActivityRequest) (ActivityResult, error) {
		calls.Add(1)
		return ActivityResult{}, nil
	})
	store := NewMemoryStore()
	rt, _ := NewRuntime(plan, store, reg)
	_, _ = rt.Start(context.Background(), "approval-1", nil, BudgetLimit{})
	e, err := rt.Run(context.Background(), "approval-1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusHuman || calls.Load() != 0 {
		t.Fatalf("expected human approval before call; status=%s calls=%d", e.Status, calls.Load())
	}
	if err := rt.Signal(context.Background(), "approval-1", Event{ID: "approve-publish", Type: "Approved", TargetNode: "publish"}); err != nil {
		t.Fatal(err)
	}
	e, err = rt.Run(context.Background(), "approval-1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusCompleted || calls.Load() != 1 {
		t.Fatalf("status=%s calls=%d", e.Status, calls.Load())
	}
}

func TestCancellationRunsCompensationInReverse(t *testing.T) {
	plan, err := Compile(Definition{ID: "comp", Version: "1", Nodes: []Node{
		{ID: "a", Kind: NodeActivity, Activity: "doA", ExternalEffect: true, Risk: RiskMedium, Timeout: time.Second, IdempotencyKey: "a:{execution}", Retry: RetryPolicy{MaxAttempts: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxRetryDuration: time.Second}, Compensation: "undoA", Next: []Transition{{To: "b"}}},
		{ID: "b", Kind: NodeActivity, Activity: "doB", ExternalEffect: true, Risk: RiskMedium, Timeout: time.Second, IdempotencyKey: "b:{execution}", Retry: RetryPolicy{MaxAttempts: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxRetryDuration: time.Second}, Compensation: "undoB", Next: []Transition{{To: "wait"}}},
		{ID: "wait", Kind: NodeWait, DependsOn: []string{"b"}, Wait: &WaitSpec{EventType: "Continue"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	reg.Activity("doA", noopActivity)
	reg.Activity("doB", noopActivity)
	order := []string{}
	reg.Compensation("undoA", func(context.Context, ActivityRequest) error { order = append(order, "A"); return nil })
	reg.Compensation("undoB", func(context.Context, ActivityRequest) error { order = append(order, "B"); return nil })
	store := NewMemoryStore()
	rt, _ := NewRuntime(plan, store, reg, WithApprovalThreshold(RiskCritical))
	_, _ = rt.Start(context.Background(), "comp-1", nil, BudgetLimit{})
	e, err := rt.Run(context.Background(), "comp-1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusWaiting {
		t.Fatalf("status=%s", e.Status)
	}
	if err := rt.Signal(context.Background(), "comp-1", Event{ID: "cancel-1", Type: "CancelRequested"}); err != nil {
		t.Fatal(err)
	}
	e, err = rt.Run(context.Background(), "comp-1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusCanceled {
		t.Fatalf("status=%s", e.Status)
	}
	if len(order) != 2 || order[0] != "B" || order[1] != "A" {
		t.Fatalf("comp order=%v", order)
	}
}

func TestRateLimitCreatesDurableThrottle(t *testing.T) {
	plan, err := Compile(Definition{ID: "rl", Version: "1", Nodes: []Node{{ID: "call", Kind: NodeActivity, Activity: "api", Retry: RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxRetryDuration: time.Second}}}})
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	var calls atomic.Int32
	reg.Activity("api", func(context.Context, ActivityRequest) (ActivityResult, error) {
		if calls.Add(1) == 1 {
			return ActivityResult{}, RateLimited(5*time.Millisecond, errors.New("429"))
		}
		return ActivityResult{}, nil
	})
	store := NewMemoryStore()
	rt, _ := NewRuntime(plan, store, reg)
	_, _ = rt.Start(context.Background(), "rl-1", nil, BudgetLimit{})
	_, err = rt.Step(context.Background(), "rl-1")
	if err != nil {
		t.Fatal(err)
	}
	e, _ := store.Load(context.Background(), "rl-1")
	if e.ThrottleUntil["api"].IsZero() {
		t.Fatal("expected durable throttle")
	}
	time.Sleep(7 * time.Millisecond)
	e, err = rt.Run(context.Background(), "rl-1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusCompleted {
		t.Fatalf("status=%s", e.Status)
	}
}

func TestConvergenceUsesGateToGateHistoryOnly(t *testing.T) {
	history := []QualitySnapshot{
		{NodeID: "draft", Utility: .70},
		{NodeID: "verify", Utility: .80},
		{NodeID: "gate", Utility: .80},
		{NodeID: "verify", Utility: .80},
		{NodeID: "other", Utility: .80},
	}
	if stagnatingAtNode(history, "gate", .001) {
		t.Fatal("one gate evaluation must not be treated as stagnation")
	}
	history = append(history,
		QualitySnapshot{NodeID: "gate", Utility: .8005},
		QualitySnapshot{NodeID: "gate", Utility: .8007},
	)
	if !stagnatingAtNode(history, "gate", .001) {
		t.Fatal("expected gate-to-gate stagnation")
	}
}
