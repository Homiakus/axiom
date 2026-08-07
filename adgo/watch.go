package adgo

import (
	"context"
	"errors"
	"time"
)

type WatchOptions struct {
	FromSeq      uint64
	PollInterval time.Duration
	Buffer       int
	StopOnTerminal bool
}

type WatchEvent struct {
	ExecutionID string       `json:"executionId"`
	Version     uint64       `json:"version"`
	Status      ExecutionStatus `json:"status"`
	History     HistoryEntry `json:"history"`
}

// Watch exposes the durable History as a resumable stream. Consumers persist the
// last sequence number and can reconnect without losing events. The stream is a
// projection of committed state, not an in-memory callback bus.
func (e *Engine) Watch(ctx context.Context, executionID string, options WatchOptions) (<-chan WatchEvent, <-chan error) {
	if options.PollInterval <= 0 {
		options.PollInterval = 100 * time.Millisecond
	}
	if options.Buffer <= 0 {
		options.Buffer = 64
	}
	events := make(chan WatchEvent, options.Buffer)
	errCh := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errCh)
		seq := options.FromSeq
		for {
			if err := ctx.Err(); err != nil {
				if !errors.Is(err, context.Canceled) {
					errCh <- err
				}
				return
			}
			execution, err := e.store.Load(ctx, executionID)
			if err != nil {
				errCh <- err
				return
			}
			for _, history := range execution.History {
				if history.Seq <= seq {
					continue
				}
				select {
				case events <- WatchEvent{ExecutionID: execution.ID, Version: execution.Version, Status: execution.Status, History: history}:
					seq = history.Seq
				case <-ctx.Done():
					return
				}
			}
			if options.StopOnTerminal && terminal(execution.Status) {
				return
			}
			if err := sleepContext(ctx, options.PollInterval); err != nil {
				return
			}
		}
	}()
	return events, errCh
}
