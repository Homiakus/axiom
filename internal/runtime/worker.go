package runtime

import (
	"context"
	"sync"
	"time"
)

type WorkerOptions struct {
	ExecutionID  string
	Concurrency  int
	PollInterval time.Duration
	LeaseTTL     time.Duration
}

func (e *Engine) StartWorker(ctx context.Context, opts WorkerOptions) error {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 100 * time.Millisecond
	}
	var wg sync.WaitGroup
	errs := make(chan error, opts.Concurrency)
	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(opts.PollInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				if opts.ExecutionID != "" {
					if recoverer, ok := e.store.(interface {
						RecoverExpiredLeases(context.Context, string, time.Duration) (int, error)
					}); ok {
						if _, err := recoverer.RecoverExpiredLeases(ctx, opts.ExecutionID, opts.LeaseTTL); err != nil {
							errs <- err
							return
						}
					}
					if err := e.RunUntilIdle(ctx, opts.ExecutionID); err != nil {
						errs <- err
						return
					}
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		<-done
		return ctx.Err()
	case err := <-errs:
		return err
	case <-done:
		return nil
	}
}

func (e *Engine) Replay(ctx context.Context, executionID string) (*Execution, error) {
	history, err := e.store.ListHistory(ctx, executionID)
	if err != nil {
		return nil, err
	}
	return ReplayFromHistory(e.module, history)
}
