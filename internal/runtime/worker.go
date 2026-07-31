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

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, 1)
	reportError := func(err error) {
		if err == nil {
			return
		}
		select {
		case errs <- err:
			cancel()
		default:
		}
	}

	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(opts.PollInterval)
			defer ticker.Stop()

			for {
				select {
				case <-workerCtx.Done():
					return
				default:
				}

				if opts.ExecutionID != "" {
					if recoverer, ok := e.store.(interface {
						RecoverExpiredLeases(context.Context, string, time.Duration) (int, error)
					}); ok {
						if _, err := recoverer.RecoverExpiredLeases(workerCtx, opts.ExecutionID, opts.LeaseTTL); err != nil {
							reportError(err)
							return
						}
					}
					if err := e.runUntilIdleWithPolicies(workerCtx, opts.ExecutionID); err != nil {
						reportError(err)
						return
					}
				}

				select {
				case <-workerCtx.Done():
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
		cancel()
		<-done
		return ctx.Err()
	case err := <-errs:
		cancel()
		<-done
		return err
	case <-done:
		select {
		case err := <-errs:
			return err
		default:
		}
		return ctx.Err()
	}
}

func (e *Engine) Replay(ctx context.Context, executionID string) (*Execution, error) {
	history, err := e.store.ListHistory(ctx, executionID)
	if err != nil {
		return nil, err
	}
	return ReplayFromHistoryContext(ctx, e.module, history)
}
