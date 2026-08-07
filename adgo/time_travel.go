package adgo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

type ForkOptions struct {
	Reason           string
	Actor            string
	CopyHistory      bool
	ResetBudgetUsage bool
	DataPatch        map[string]any
}

type ForkInfo struct {
	SourceExecution string `json:"sourceExecution"`
	SourceVersion   uint64 `json:"sourceVersion"`
	NewExecution    string `json:"newExecution"`
	PlanDigest      string `json:"planDigest"`
}

// InspectVersion returns an immutable committed execution snapshot. version=0
// means latest. Historical inspection requires VersionedStore.
func InspectVersion(ctx context.Context, store Store, executionID string, version uint64) (*Execution, error) {
	if version == 0 {
		return store.Load(ctx, executionID)
	}
	versioned, ok := store.(VersionedStore)
	if !ok {
		return nil, fmt.Errorf("adgo: historical inspection requires VersionedStore")
	}
	versions, err := versioned.ListVersions(ctx, executionID)
	if err != nil {
		return nil, err
	}
	index := sort.Search(len(versions), func(i int) bool { return versions[i].Version >= version })
	if index >= len(versions) || versions[index].Version != version {
		return nil, fmt.Errorf("adgo: execution %s version %d not found", executionID, version)
	}
	return cloneExecution(versions[index])
}

// ForkExecution creates a new durable execution from a committed snapshot. It
// never reuses active worker leases or inbox deduplication state. Completed work,
// facts, artifacts and repair counters are preserved by default, which makes the
// fork suitable for deterministic debugging and alternative plan experiments.
func ForkExecution(ctx context.Context, store Store, plan *Plan, sourceID string, sourceVersion uint64, newID string, options ForkOptions) (*Execution, ForkInfo, error) {
	if plan == nil {
		return nil, ForkInfo{}, fmt.Errorf("adgo: plan is required")
	}
	if newID == "" {
		return nil, ForkInfo{}, fmt.Errorf("adgo: fork execution id is required")
	}
	if sourceID == newID {
		return nil, ForkInfo{}, fmt.Errorf("adgo: fork id must differ from source id")
	}
	source, err := InspectVersion(ctx, store, sourceID, sourceVersion)
	if err != nil {
		return nil, ForkInfo{}, err
	}
	if source.PlanID != plan.ID || source.PlanDigest != plan.Digest {
		return nil, ForkInfo{}, fmt.Errorf("adgo: source execution is pinned to another plan")
	}
	fork, err := cloneExecution(source)
	if err != nil {
		return nil, ForkInfo{}, err
	}
	now := time.Now().UTC()
	fork.ID = newID
	fork.Version = 1
	fork.Status = StatusRunning
	fork.Failure = ""
	fork.CancelRequested = false
	fork.ActiveTasks = map[string]TaskRuntime{}
	fork.SeenEvents = map[string]bool{}
	fork.CreatedAt = now
	fork.UpdatedAt = now
	if options.ResetBudgetUsage {
		fork.BudgetUsage = BudgetUsage{}
		fork.Metrics.Cost = 0
		fork.Metrics.Tokens = 0
	}
	for id, runtime := range fork.Nodes {
		if runtime == nil {
			continue
		}
		if runtime.Status == NodeRunning {
			runtime.Status = NodePending
			runtime.NotBefore = time.Time{}
			runtime.LastError = "fork recovered in-flight work as pending"
		}
		if runtime.Status == NodeWaiting && fork.WaitingFor[id] == "timer" && !runtime.NotBefore.IsZero() && !runtime.NotBefore.After(now) {
			runtime.Status = NodePending
			delete(fork.WaitingFor, id)
			runtime.NotBefore = time.Time{}
		}
	}
	for key, value := range options.DataPatch {
		if err := SetData(fork, key, value); err != nil {
			return nil, ForkInfo{}, err
		}
	}
	if !options.CopyHistory {
		fork.History = nil
	}
	appendHistory(fork, "execution_forked", "", options.Reason, map[string]any{
		"sourceExecution": source.ID,
		"sourceVersion":   source.Version,
		"actor":           options.Actor,
	})
	if err := store.Create(ctx, fork); err != nil {
		if errors.Is(err, ErrExecutionExists) {
			return nil, ForkInfo{}, ErrExecutionExists
		}
		return nil, ForkInfo{}, err
	}
	created, err := store.Load(ctx, newID)
	if err != nil {
		return nil, ForkInfo{}, err
	}
	return created, ForkInfo{SourceExecution: source.ID, SourceVersion: source.Version, NewExecution: newID, PlanDigest: plan.Digest}, nil
}
