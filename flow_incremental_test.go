package axiom

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

type incrementalTestStore struct {
	mu              sync.Mutex
	state           []byte
	history         []FlowHistoryEntry
	found           bool
	legacyLoads     int
	legacySaves     int
	incrementalLoad int
	incrementalSave int
}

func (s *incrementalTestStore) Load(context.Context, string, string) ([]byte, []FlowHistoryEntry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.legacyLoads++
	return append([]byte(nil), s.state...), append([]FlowHistoryEntry(nil), s.history...), s.found, nil
}

func (s *incrementalTestStore) Save(_ context.Context, _, _ string, state []byte, history []FlowHistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.legacySaves++
	s.state = append([]byte(nil), state...)
	s.history = append([]FlowHistoryEntry(nil), history...)
	s.found = true
	return nil
}

func (s *incrementalTestStore) LoadState(context.Context, string, string) ([]byte, int, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.incrementalLoad++
	return append([]byte(nil), s.state...), len(s.history), s.found, nil
}

func (s *incrementalTestStore) SaveStateAndAppend(_ context.Context, _, _ string, state []byte, entries []FlowHistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.incrementalSave++
	s.state = append([]byte(nil), state...)
	s.history = append(s.history, entries...)
	s.found = true
	return nil
}

func (s *incrementalTestStore) LoadHistory(context.Context, string, string) ([]FlowHistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]FlowHistoryEntry(nil), s.history...), nil
}

func TestFlowUsesIncrementalStoreCapability(t *testing.T) {
	type state struct{ Count int }
	type increment struct{ By int }

	flow := NewFlow("incremental", state{})
	Handle(flow, func(_ context.Context, current state, event increment) (FlowResult[state], error) {
		current.Count += event.By
		return Next(current), nil
	})

	store := &incrementalTestStore{}
	engine, err := OpenFlow(flow, WithFlowStore(store))
	if err != nil {
		t.Fatal(err)
	}

	const operations = 100
	for i := 0; i < operations; i++ {
		if err := engine.Execution("one").Dispatch(context.Background(), increment{By: 1}); err != nil {
			t.Fatal(err)
		}
	}

	current, err := engine.Execution("one").State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Count != operations {
		t.Fatalf("Count = %d, want %d", current.Count, operations)
	}

	history, err := engine.Execution("one").History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != operations {
		t.Fatalf("history length = %d, want %d", len(history), operations)
	}
	for index, entry := range history {
		if want := index + 1; entry.Sequence != want {
			t.Fatalf("history[%d].Sequence = %d, want %d", index, entry.Sequence, want)
		}
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.legacyLoads != 0 || store.legacySaves != 0 {
		t.Fatalf("legacy store methods were used: loads=%d saves=%d", store.legacyLoads, store.legacySaves)
	}
	if store.incrementalSave != operations {
		t.Fatalf("incremental saves = %d, want %d", store.incrementalSave, operations)
	}
}

func TestOpenFlowFreezesDefinition(t *testing.T) {
	type state struct{ Value int }
	type event struct{ Value int }

	flow := NewFlow("frozen", state{})
	Handle(flow, func(_ context.Context, _ state, incoming event) (FlowResult[state], error) {
		return Next(state{Value: incoming.Value}), nil
	})
	engine, err := OpenFlow(flow)
	if err != nil {
		t.Fatal(err)
	}

	Handle(flow, func(context.Context, state, event) (FlowResult[state], error) {
		return FlowResult[state]{}, fmt.Errorf("late handler must not affect opened engine")
	})

	if err := engine.Execution("one").Dispatch(context.Background(), event{Value: 7}); err != nil {
		t.Fatal(err)
	}
	current, err := engine.Execution("one").State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Value != 7 {
		t.Fatalf("Value = %d, want 7", current.Value)
	}
}
