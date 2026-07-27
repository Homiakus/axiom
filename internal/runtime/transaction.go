package runtime

import "context"

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
	if err := fn(working); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
