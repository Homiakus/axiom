package adgo

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestHostPollSurvivesRoundRobinCounterRollover(t *testing.T) {
	store := NewMemoryStore()
	host, err := NewHost(store)
	if err != nil {
		t.Fatal(err)
	}

	planA, err := Compile(Definition{ID: "rollover", Version: "1", Nodes: []Node{{ID: "a", Kind: NodeActivity, Activity: "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	planB, err := Compile(Definition{ID: "rollover", Version: "2", Nodes: []Node{{ID: "b", Kind: NodeActivity, Activity: "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	regA := NewRegistry()
	regA.Activity("a", noopActivity)
	regB := NewRegistry()
	regB.Activity("b", noopActivity)
	if _, err := host.Register(planA, regA); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Register(planB, regB); err != nil {
		t.Fatal(err)
	}

	// Force the next atomic increment to wrap to zero. The previous
	// uint64 -> int narrowing turned the resulting MaxUint64 sequence into -1
	// on 64-bit systems and could index h.order with a negative value.
	host.next.Store(math.MaxUint64)
	if _, err := host.Poll(context.Background(), WorkerSpec{ID: "rollover-worker"}); !errors.Is(err, ErrNoWork) {
		t.Fatalf("Poll after round-robin counter rollover err=%v, want ErrNoWork", err)
	}
}
