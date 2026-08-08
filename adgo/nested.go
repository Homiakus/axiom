package adgo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// NestedInvocation binds a child execution to the durable identity of the
// parent activity application. ParentApplicationID should include the parent's
// idempotency/revision and logical-input identity. Redelivery of the same
// application therefore resumes the same child, while a legitimate parent
// revision gets a new child execution.
type NestedInvocation struct {
	ParentExecutionID  string
	ParentNodeID       string
	ParentApplicationID string
	FlowName           string
}

// NestedLocalOptions configures embedded execution of a child plan through the
// production Engine protocol. It intentionally exposes Engine options instead
// of reproducing scheduler/lease policy in an application adapter.
type NestedLocalOptions struct {
	Budget          BudgetLimit
	Worker          WorkerSpec
	WaitForExternal bool
	EngineOptions   []EngineOption
}

// NestedExecutionID derives the child identity through Axiom's canonical
// ChildExecutionID scheme. The application identity is hashed so IDs stay
// compact and do not leak arbitrary input material into storage paths/logs.
func NestedExecutionID(inv NestedInvocation) (string, error) {
	if strings.TrimSpace(inv.ParentExecutionID) == "" {
		return "", fmt.Errorf("adgo: nested parent execution id is required")
	}
	if strings.TrimSpace(inv.ParentNodeID) == "" {
		return "", fmt.Errorf("adgo: nested parent node id is required")
	}
	if strings.TrimSpace(inv.FlowName) == "" {
		return "", fmt.Errorf("adgo: nested flow name is required")
	}
	if strings.TrimSpace(inv.ParentApplicationID) == "" {
		return "", fmt.Errorf("adgo: nested parent application id is required")
	}
	sum := sha256.Sum256([]byte(inv.ParentApplicationID))
	itemID := inv.FlowName + "-" + hex.EncodeToString(sum[:8])
	return ChildExecutionID(inv.ParentExecutionID, inv.ParentNodeID, itemID), nil
}

// RunNestedLocal compiles and executes a child workflow using the same Store as
// its parent and the production Engine task/lease/fencing protocol.
//
// It is the embedded counterpart to Host child workflows: applications may
// construct dynamic child Definitions, but child identity, plan pinning,
// StartOrLoad semantics, retries, workers and recovery remain Axiom-owned.
func RunNestedLocal(
	ctx context.Context,
	store Store,
	definition Definition,
	registry *Registry,
	invocation NestedInvocation,
	initial map[string]any,
	options NestedLocalOptions,
) (*Execution, error) {
	if store == nil {
		return nil, fmt.Errorf("adgo: nested store is required")
	}
	childID, err := NestedExecutionID(invocation)
	if err != nil {
		return nil, err
	}
	plan, err := Compile(definition)
	if err != nil {
		return nil, fmt.Errorf("adgo: compile nested flow %q: %w", invocation.FlowName, err)
	}
	engine, err := NewEngine(plan, store, registry, options.EngineOptions...)
	if err != nil {
		return nil, fmt.Errorf("adgo: create nested engine %q: %w", invocation.FlowName, err)
	}
	if _, err := engine.StartOrLoad(ctx, childID, initial, options.Budget); err != nil {
		return nil, fmt.Errorf("adgo: start nested flow %q: %w", invocation.FlowName, err)
	}
	worker := options.Worker
	if strings.TrimSpace(worker.ID) == "" {
		worker.ID = "nested/" + invocation.FlowName + "/" + shortNestedID(childID)
	}
	execution, err := engine.RunLocal(ctx, childID, LocalRunOptions{
		Worker:          worker,
		WaitForExternal: options.WaitForExternal,
	})
	if err != nil {
		return execution, fmt.Errorf("adgo: run nested flow %q: %w", invocation.FlowName, err)
	}
	return execution, nil
}

func shortNestedID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:6])
}
