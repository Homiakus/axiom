package runtime

import (
	"context"
	"errors"

	"github.com/Homiakus/axiom/internal/diag"
)

// shouldCommitTransactionError identifies domain failures whose state changes
// have already been written to the transaction and must remain durable even
// though a control-flow error is returned to the caller.
func shouldCommitTransactionError(err error) bool {
	if _, ok := retryScheduled(err); ok {
		return true
	}
	var diagnostic *diag.Error
	if !errors.As(err, &diagnostic) || diagnostic == nil {
		return false
	}
	switch diagnostic.Code {
	case "AX505": // terminal activity handler failure after task/history/state were updated
		return true
	default:
		return false
	}
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
		module:         e.module,
		store:          tx,
		activities:     e.activities,
		maxSteps:       e.maxSteps,
		fast:           e.fast,
		strictFast:     e.strictFast,
		traceLevel:     e.traceLevel,
		clock:          e.clock,
		executionLocks: e.executionLocks,
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
