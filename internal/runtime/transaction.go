package runtime

import "context"

func (e *Engine) withStoreTransaction(ctx context.Context, fn func() error) error {
	transactional, ok := e.store.(TransactionalStore)
	if !ok {
		return fn()
	}
	e.storeMu.Lock()
	defer e.storeMu.Unlock()
	tx, err := transactional.BeginTransaction(ctx)
	if err != nil {
		return err
	}
	original := e.store
	e.store = tx
	defer func() {
		e.store = original
	}()
	if err := fn(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
