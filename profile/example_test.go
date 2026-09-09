package profile_test

import (
	"context"
	"fmt"
	"os"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/adgo"
	"github.com/Homiakus/axiom/profile"
)

type userState struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type activateUserEvent struct {
	Name string `json:"name"`
}

func ExampleOpenEmbedded() {
	// 1. Initialize the Embedded profile (in-process, memory-backed).
	emb := profile.NewEmbedded()
	defer emb.Close()

	// 2. Define a Go-first Flow.
	fl := axiom.NewFlow("user", userState{Active: false})
	axiom.Handle(fl, func(ctx context.Context, s userState, e activateUserEvent) (axiom.FlowResult[userState], error) {
		s.Name = e.Name
		s.Active = true
		return axiom.Next(s), nil
	})

	// 3. Open FlowEngine using the Embedded profile.
	engine, err := profile.OpenEmbeddedFlow(emb, fl)
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	exec := engine.Execution("user-100")
	if err := exec.Dispatch(ctx, activateUserEvent{Name: "Alice"}); err != nil {
		panic(err)
	}

	st, _ := exec.State(ctx)
	fmt.Printf("User %s active: %t\n", st.Name, st.Active)
	// Output:
	// User Alice active: true
}

func ExampleOpenDurableSingleNode() {
	dir, err := os.MkdirTemp("", "axiom-durable-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	// Open the Durable Single Node profile backed by synchronous Pebble WAL.
	p, err := profile.OpenDurableSingleNode(profile.DurableSingleNodeConfig{
		Dir:        dir,
		SyncWrites: true,
	})
	if err != nil {
		panic(err)
	}
	defer p.Close()

	fl := axiom.NewFlow("order", userState{})
	axiom.Handle(fl, func(ctx context.Context, s userState, e activateUserEvent) (axiom.FlowResult[userState], error) {
		s.Name = e.Name
		s.Active = true
		return axiom.Next(s), nil
	})

	flowEngine, err := profile.OpenDurableFlow(p, fl)
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	exec := flowEngine.Execution("order-42")
	if err := exec.Dispatch(ctx, activateUserEvent{Name: "Order42"}); err != nil {
		panic(err)
	}

	st, _ := exec.State(ctx)
	fmt.Printf("Durable name: %s\n", st.Name)
	// Output:
	// Durable name: Order42
}

func ExampleOpenDistributedProduction() {
	dir, err := os.MkdirTemp("", "axiom-dist-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	plan, err := adgo.Compile(adgo.Definition{
		ID:      "pipeline",
		Version: "1",
		Nodes: []adgo.Node{
			{ID: "step1", Kind: adgo.NodeActivity, Activity: "step1"},
		},
	})
	if err != nil {
		panic(err)
	}

	reg := adgo.NewRegistry()
	reg.Activity("step1", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		return adgo.ActivityResult{Facts: map[string]any{"ok": true}}, nil
	})

	cfg := profile.DefaultDistributedProductionConfig("node-alpha", dir)
	prod, err := profile.OpenDistributedProduction(plan, reg, cfg)
	if err != nil {
		panic(err)
	}
	defer prod.Close()

	g := prod.Guarantees()
	fmt.Println("Profile:", g.Name)
	fmt.Println("Persistence:", g.Persistence)
	// Output:
	// Profile: DistributedProduction
	// Persistence: Shared Transactional Store with Leased Fencing
}
