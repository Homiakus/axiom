package adgo

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPWorkerProtocolAuthCompleteAndFencing(t *testing.T) {
	plan, err := Compile(Definition{ID: "http-worker", Version: "1", Nodes: []Node{
		{ID: "work", Kind: NodeActivity, Activity: "work", Produces: []string{"answer"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	engine, err := NewEngine(plan, store, NewRegistry(), WithEngineLeaseTTL(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := engine.Start(ctx, "remote-1", nil, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(ctx, "remote-1"); err != nil {
		t.Fatal(err)
	}

	handler, err := NewHTTPWorkerServer(engine, HTTPWorkerServerOptions{BearerToken: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	unauthorized := &HTTPWorkerClient{BaseURL: server.URL, BearerToken: "wrong", Client: server.Client()}
	if _, err := unauthorized.Poll(ctx, WorkerSpec{ID: "bad"}); err == nil {
		t.Fatal("expected unauthorized worker to fail")
	}

	client := &HTTPWorkerClient{BaseURL: server.URL, BearerToken: "secret", Client: server.Client()}
	work, err := client.Poll(ctx, WorkerSpec{ID: "remote-worker", LeaseTTL: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if work.Token.ExecutionID != "remote-1" || work.Node.ID != "work" || work.Activity != "work" {
		t.Fatalf("work=%+v", work)
	}
	if err := client.Heartbeat(ctx, work.Token, map[string]any{"progress": .5}); err != nil {
		t.Fatal(err)
	}
	if err := client.Complete(ctx, work.Token, ActivityResult{Facts: map[string]any{"answer": 42}}, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := client.Complete(ctx, work.Token, ActivityResult{}, time.Millisecond); !errors.Is(err, ErrStaleTask) {
		t.Fatalf("late duplicate complete err=%v", err)
	}
	if _, err := engine.Advance(ctx, "remote-1"); err != nil {
		t.Fatal(err)
	}
	execution, err := store.Load(ctx, "remote-1")
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != StatusCompleted {
		t.Fatalf("status=%s", execution.Status)
	}
	if _, ok := execution.Data["answer"]; !ok {
		t.Fatal("remote result fact was not committed")
	}
}
