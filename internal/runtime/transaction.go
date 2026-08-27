package runtime

import (
	"context"
	"errors"

	"github.com/Homiakus/axiom/internal/diag"
)

// DurableStateError is implemented by domain or flow-control errors whose state changes
// have been staged in the transaction and must be committed to the store even though
// an error is returned to the caller.
type DurableStateError interface {
	error
	ShouldCommitState() bool
}

// shouldCommitTransactionError identifies domain failures whose state changes
// have already been written to the transaction and must remain durable even
// though a control-flow error is returned to the caller.
func shouldCommitTransactionError(err error) bool {
	if err == nil {
		return false
	}
	if dse, ok := err.(DurableStateError); ok && dse.ShouldCommitState() {
		return true
	}
	var dsePtr DurableStateError
	if errors.As(err, &dsePtr) && dsePtr != nil && dsePtr.ShouldCommitState() {
		return true
	}
	if _, ok := retryScheduled(err); ok {
		return true
	}
	var diagnostic *diag.Error
	if errors.As(err, &diagnostic) && diagnostic != nil {
		return diagnostic.Code == "AX505"
	}
	var diagVal diag.Error
	if errors.As(err, &diagVal) {
		return diagVal.Code == "AX505"
	}
	return false
}

func (e *Engine) withStoreTransaction(ctx context.Context, fn func(*Engine) error) error {
	transactional, ok := e.store.(TransactionalStore)
	if !ok {
		return fn(e)
	}

	e.storeMu.Lock()
	defer e.storeMu.Unlock()

	tx, err := transactional.BeginTransaction(ctx)
	if err != nil {
		return err
	}

	working := &Engine{
		module:             e.module,
		store:              tx,
		activities:         e.activities,
		externalActivities: e.externalActivities,
		maxSteps:           e.maxSteps,
		fast:               e.fast,
		strictFast:         e.strictFast,
		traceLevel:         e.traceLevel,
		clock:              e.clock,
		executionLocks:     e.executionLocks,
	}

	callbackErr := fn(working)
	if callbackErr != nil && !shouldCommitTransactionError(callbackErr) {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return errors.Join(callbackErr, rollbackErr)
		}
		return callbackErr
	}

	if commitErr := tx.Commit(); commitErr != nil {
		if callbackErr != nil {
			// A retry marker is only meaningful after its checkpoint committed.
			// Never let callers treat a failed commit as a successfully scheduled retry.
			if _, ok := retryScheduled(callbackErr); ok {
				return commitErr
			}
			return errors.Join(callbackErr, commitErr)
		}
		return commitErr
	}
	return callbackErr
}
