package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrExternalActivityClaimInvalid = errors.New("axiom: invalid external activity claim")
	ErrExternalActivityClaimStale   = errors.New("axiom: stale external activity claim")
)

const maxExternalActivityLease = time.Hour

// ExternalActivityToken is a fencing token for one leased activity attempt.
// Attempt changes on every re-claim, so a late worker cannot complete a newer
// attempt even if it happens to reuse the same worker id.
type ExternalActivityToken struct {
	ExecutionID string `json:"executionId"`
	TaskID      string `json:"taskId"`
	WorkerID    string `json:"workerId"`
	Attempt     int    `json:"attempt"`
}

// ExternalActivityClaim is the transport-safe activity work item returned to a
// worker outside the Engine process. Input is a defensive copy.
type ExternalActivityClaim struct {
	Token          ExternalActivityToken `json:"token"`
	RuleName       string                `json:"ruleName"`
	ActivityName   string                `json:"activityName"`
	IdempotencyKey string                `json:"idempotencyKey"`
	Input          map[string]any        `json:"input,omitempty"`
	Attempt        int                   `json:"attempt"`
	MaxAttempts    int                   `json:"maxAttempts"`
	LeaseUntil     time.Time             `json:"leaseUntil"`
}

// ClaimExternalActivity atomically recovers external-worker leases whose
// persisted LockedUntil deadline has expired and leases at most one due task.
// The claimant's requested TTL is never used to judge an older worker's lease:
// doing so would let a short-TTL claimant steal a still-valid long lease.
// Legacy running tasks without LockedUntil are left untouched rather than
// guessed; an explicit store/runtime recovery path can handle those separately.
func (e *Engine) ClaimExternalActivity(ctx context.Context, executionID, workerID string, leaseTTL time.Duration) (*ExternalActivityClaim, error) {
	if e == nil || !safeExternalToken(executionID, 256) || !safeExternalToken(workerID, 128) || leaseTTL <= 0 || leaseTTL > maxExternalActivityLease {
		return nil, ErrExternalActivityClaimInvalid
	}
	var task *ActivityTask
	err := e.withStoreTransaction(ctx, func(working *Engine) error {
		if _, err := working.recoverExpiredExternalActivityLeases(ctx, executionID); err != nil {
			return err
		}
		claimed, err := working.store.PollTaskWithLease(ctx, executionID, workerID, leaseTTL)
		if err != nil {
			return err
		}
		if claimed != nil {
			task = cloneExternalActivityTask(claimed)
		}
		return nil
	})
	if err != nil || task == nil {
		return nil, err
	}
	return externalClaim(task), nil
}

// HeartbeatExternalActivity renews a currently valid claim. Worker id, attempt,
// running status and lease expiry are checked transactionally. Renewal preserves
// the lease duration stored on the claimed task instead of depending on a
// store-specific default TTL.
func (e *Engine) HeartbeatExternalActivity(ctx context.Context, token ExternalActivityToken) error {
	if e == nil || !validExternalActivityToken(token) {
		return ErrExternalActivityClaimInvalid
	}
	return e.withStoreTransaction(ctx, func(working *Engine) error {
		task, err := working.requireExternalActivityClaim(ctx, token)
		if err != nil {
			return err
		}
		leaseTTL := task.LockedUntil.Sub(task.UpdatedAt)
		if leaseTTL <= 0 || leaseTTL > maxExternalActivityLease {
			return ErrExternalActivityClaimStale
		}
		now := working.now()
		next := cloneExternalActivityTask(task)
		next.LockedUntil = now.Add(leaseTTL)
		next.UpdatedAt = now
		return working.store.UpdateTask(ctx, next)
	})
}

// CompleteExternalActivity applies the same output validation, writes and
// downstream rule processing as an in-process Activity handler, but only after
// validating the fencing token inside the same Engine transaction.
func (e *Engine) CompleteExternalActivity(ctx context.Context, token ExternalActivityToken, result map[string]any) error {
	if e == nil || !validExternalActivityToken(token) {
		return ErrExternalActivityClaimInvalid
	}
	return e.withStoreTransaction(ctx, func(working *Engine) error {
		task, err := working.requireExternalActivityClaim(ctx, token)
		if err != nil {
			return err
		}
		return working.completeActivity(ctx, token.ExecutionID, task, cloneAnyMap(result), nil)
	})
}

// FailExternalActivity records a classified external failure using the normal
// AXM retry policy. Only a bounded machine code is accepted; arbitrary provider
// error text is intentionally excluded from Axiom history.
func (e *Engine) FailExternalActivity(ctx context.Context, token ExternalActivityToken, code string) error {
	if e == nil || !validExternalActivityToken(token) || !safeExternalFailureCode(code) {
		return ErrExternalActivityClaimInvalid
	}
	return e.withStoreTransaction(ctx, func(working *Engine) error {
		task, err := working.requireExternalActivityClaim(ctx, token)
		if err != nil {
			return err
		}
		return working.completeActivity(ctx, token.ExecutionID, task, nil, fmt.Errorf("external activity failure %s", code))
	})
}

// recoverExpiredExternalActivityLeases performs portable, fail-safe recovery
// using the deadline persisted with each modern leased task. It deliberately
// does not call Store.RecoverExpiredLeases because that legacy API accepts a TTL
// parameter and some stores interpret it relative to UpdatedAt; a new worker's
// TTL must never participate in deciding whether an older lease is alive.
func (e *Engine) recoverExpiredExternalActivityLeases(ctx context.Context, executionID string) (int, error) {
	tasks, err := e.store.ListTasks(ctx, executionID)
	if err != nil {
		return 0, err
	}
	now := e.now()
	recovered := 0
	for _, task := range tasks {
		if task == nil || task.Status != TaskRunning || task.LockedUntil.IsZero() || task.LockedUntil.After(now) {
			continue
		}
		next := cloneExternalActivityTask(task)
		next.Status = TaskPending
		next.LockedBy = ""
		next.LockedUntil = time.Time{}
		next.UpdatedAt = now
		if err := e.store.UpdateTask(ctx, next); err != nil {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}

func (e *Engine) requireExternalActivityClaim(ctx context.Context, token ExternalActivityToken) (*ActivityTask, error) {
	tasks, err := e.store.ListTasks(ctx, token.ExecutionID)
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		if task == nil || task.ID != token.TaskID {
			continue
		}
		if task.ExecutionID != token.ExecutionID || task.Status != TaskRunning || task.LockedBy != token.WorkerID || task.Attempt != token.Attempt {
			return nil, ErrExternalActivityClaimStale
		}
		if task.LockedUntil.IsZero() || !task.LockedUntil.After(e.now()) {
			return nil, ErrExternalActivityClaimStale
		}
		return cloneExternalActivityTask(task), nil
	}
	return nil, ErrExternalActivityClaimStale
}

func externalClaim(task *ActivityTask) *ExternalActivityClaim {
	return &ExternalActivityClaim{
		Token: ExternalActivityToken{ExecutionID: task.ExecutionID, TaskID: task.ID, WorkerID: task.LockedBy, Attempt: task.Attempt},
		RuleName: task.RuleName, ActivityName: task.ActivityName, IdempotencyKey: task.IdempotencyKey,
		Input: cloneAnyMap(task.Input), Attempt: task.Attempt, MaxAttempts: task.MaxAttempts, LeaseUntil: task.LockedUntil,
	}
}

func cloneExternalActivityTask(task *ActivityTask) *ActivityTask {
	if task == nil {
		return nil
	}
	copy := *task
	copy.Input = cloneAnyMap(task.Input)
	copy.Result = cloneAnyMap(task.Result)
	return &copy
}

func validExternalActivityToken(token ExternalActivityToken) bool {
	return safeExternalToken(token.ExecutionID, 256) && safeExternalToken(token.TaskID, 256) && safeExternalToken(token.WorkerID, 128) && token.Attempt > 0
}

func safeExternalToken(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func safeExternalFailureCode(value string) bool {
	if value == "" || len(value) > 96 {
		return false
	}
	for i, r := range value {
		if (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9') || (i > 0 && r == '_') {
			continue
		}
		return false
	}
	return true
}
