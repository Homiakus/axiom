package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Homiakus/axiom/adgo"
)

func main() {
	plan, err := adgo.Compile(adgo.Definition{
		ID:                "production-example",
		Version:           "1",
		GlobalConcurrency: 4,
		Nodes: []adgo.Node{
			{
				ID:             "prepare",
				Kind:           adgo.NodeActivity,
				Activity:       "Prepare",
				Produces:       []string{"prepared"},
				EstimatedCost:  0.1,
				EstimatedLatency: 20 * time.Millisecond,
				Next:           []adgo.Transition{{To: "finish"}},
			},
			{
				ID:             "finish",
				Kind:           adgo.NodeActivity,
				Activity:       "Finish",
				DependsOn:      []string{"prepare"},
				Requires:       []string{"prepared"},
				Produces:       []string{"result"},
				EstimatedCost:  0.1,
				EstimatedLatency: 10 * time.Millisecond,
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	registry := adgo.NewRegistry()
	registry.Activity("Prepare", func(ctx context.Context, request adgo.ActivityRequest) (adgo.ActivityResult, error) {
		_ = adgo.ActivityHeartbeat(ctx, map[string]any{"phase": "preparing"})
		time.Sleep(20 * time.Millisecond)
		return adgo.ActivityResult{
			Facts:  map[string]any{"prepared": true},
			Budget: adgo.BudgetUsage{Cost: 0.1},
		}, nil
	})
	registry.Activity("Finish", func(context.Context, adgo.ActivityRequest) (adgo.ActivityResult, error) {
		return adgo.ActivityResult{
			Facts:   map[string]any{"result": "done"},
			Quality: adgo.QualityVector{"quality": 1},
			Budget:  adgo.BudgetUsage{Cost: 0.1},
		}, nil
	})

	config := adgo.DefaultProductionConfig("")
	config.Backend = adgo.BackendMemory
	production, err := adgo.OpenProduction(plan, registry, config)
	if err != nil {
		log.Fatal(err)
	}
	defer production.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := production.Engine.StartOrLoad(ctx, "example-1", nil, adgo.BudgetLimit{MaxCost: 1}); err != nil {
		log.Fatal(err)
	}

	serviceCtx, stopServices := context.WithCancel(ctx)
	defer stopServices()
	errors := make(chan error, 2)
	go func() { errors <- production.Engine.RunResilientCoordinator(serviceCtx) }()
	go func() {
		errors <- production.Engine.RunWorker(serviceCtx, adgo.WorkerSpec{
			ID:          "local-worker",
			Concurrency: 2,
			LeaseTTL:    time.Second,
		})
	}()

	execution, err := production.Engine.Await(ctx, "example-1", adgo.AwaitOptions{})
	if err != nil {
		log.Fatal(err)
	}
	stopServices()

	diagnostics, err := production.Engine.Diagnostics(context.Background(), execution.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("execution=%s status=%s cost=%.2f quality=%.2f diagnostics=%d\n",
		execution.ID,
		execution.Status,
		execution.BudgetUsage.Cost,
		adgo.QualityUtility(execution.Quality),
		len(diagnostics.Diagnostics),
	)
}
