// Command distributed-production demonstrates running coordinator/worker separation,
// lease fencing, and adaptive provider routing using Axiom's Distributed Production profile.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Homiakus/axiom/adgo"
	"github.com/Homiakus/axiom/profile"
)

func main() {
	// 1. Compile execution plan
	plan, err := adgo.Compile(adgo.Definition{
		ID:      "order-pipeline",
		Version: "1",
		Nodes: []adgo.Node{
			{ID: "validate", Kind: adgo.NodeActivity, Activity: "validate_order", Next: []adgo.Transition{{To: "charge"}}},
			{ID: "charge", Kind: adgo.NodeActivity, Activity: "charge_card", DependsOn: []string{"validate"}},
		},
	})
	if err != nil {
		log.Fatalf("compile plan: %v", err)
	}

	// 2. Register worker activity handlers
	reg := adgo.NewRegistry()
	reg.Activity("validate_order", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		fmt.Println("[Worker] Validating order...")
		return adgo.ActivityResult{Facts: map[string]any{"valid": true}}, nil
	})
	reg.Activity("charge_card", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		fmt.Println("[Worker] Charging payment...")
		return adgo.ActivityResult{Facts: map[string]any{"charged": true}}, nil
	})

	// 3. Open Distributed Production profile
	dir := "./data/dist-cluster"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	cfg := profile.DefaultDistributedProductionConfig("coordinator-node-01", dir)
	cfg.PollInterval = 50 * time.Millisecond

	cluster, err := profile.OpenDistributedProduction(plan, reg, cfg)
	if err != nil {
		log.Fatalf("open profile: %v", err)
	}
	defer cluster.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 4. Start an execution
	execID := fmt.Sprintf("order-%d", time.Now().Unix())
	if _, err := cluster.Production().Engine.Start(ctx, execID, map[string]any{"amount": 99.99}, adgo.BudgetLimit{}); err != nil {
		log.Fatalf("start execution: %v", err)
	}

	// 5. Run coordinator and worker service
	workerSpec := adgo.WorkerSpec{
		ID:           "worker-payments-01",
		Activities:   []string{"validate_order", "charge_card"},
		Concurrency:  4,
		LeaseTTL:     10 * time.Second,
		PollInterval: 50 * time.Millisecond,
	}

	go func() {
		if err := cluster.Serve(ctx, workerSpec); err != nil && ctx.Err() == nil {
			log.Printf("serve error: %v", err)
		}
	}()

	// Wait for execution completion
	for {
		exec, err := cluster.Production().Engine.Get(ctx, execID)
		if err == nil && (exec.Status == adgo.StatusCompleted || exec.Status == adgo.StatusFailed) {
			fmt.Printf("Execution %s reached final status: %s\n", execID, exec.Status)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}
