package adgo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ServeResilient is the recommended single-process production topology: one
// compensation-aware coordinator plus one or more worker pools.
func (e *Engine) ServeResilient(ctx context.Context, workers ...WorkerSpec) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, len(workers)+1)
	go func() { errCh <- e.RunResilientCoordinator(serveCtx) }()
	for _, spec := range workers {
		spec := spec
		go func() { errCh <- e.RunWorker(serveCtx, spec) }()
	}
	err := <-errCh
	cancel()
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (h *Host) ServeResilient(ctx context.Context, workers ...WorkerSpec) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, len(workers)+1)
	go func() { errCh <- h.RunResilientCoordinator(serveCtx) }()
	for _, spec := range workers {
		spec := spec
		go func() { errCh <- h.RunWorker(serveCtx, spec) }()
	}
	err := <-errCh
	cancel()
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

type WorkerServiceStatus struct {
	WorkerID string `json:"workerId"`
	Active   int64  `json:"active"`
	Draining bool   `json:"draining"`
	Stopped  bool   `json:"stopped"`
}

// WorkerService separates graceful drain from hard process cancellation. Drain
// closes polling first; already claimed handlers keep their original context and
// heartbeat until they finish. Hard cancellation still comes from Run's ctx.
type WorkerService struct {
	engine *Engine
	spec   WorkerSpec

	drain     chan struct{}
	drainOnce sync.Once
	done      chan struct{}
	active    atomic.Int64
	draining  atomic.Bool
	stopped   atomic.Bool
}

func NewWorkerService(engine *Engine, spec WorkerSpec) (*WorkerService, error) {
	if engine == nil {
		return nil, fmt.Errorf("adgo: worker service engine is required")
	}
	spec = engine.normalizeWorker(spec)
	return &WorkerService{engine: engine, spec: spec, drain: make(chan struct{}), done: make(chan struct{})}, nil
}

func (s *WorkerService) Status() WorkerServiceStatus {
	return WorkerServiceStatus{WorkerID: s.spec.ID, Active: s.active.Load(), Draining: s.draining.Load(), Stopped: s.stopped.Load()}
}

func (s *WorkerService) Run(ctx context.Context) error {
	defer close(s.done)
	defer s.stopped.Store(true)
	var wg sync.WaitGroup
	errCh := make(chan error, s.spec.Concurrency)
	for slot := 0; slot < s.spec.Concurrency; slot++ {
		wg.Add(1)
		local := s.spec
		if s.spec.Concurrency > 1 {
			local.ID = fmt.Sprintf("%s/%d", s.spec.ID, slot+1)
		}
		go func() {
			defer wg.Done()
			errCh <- s.slot(ctx, local)
		}()
	}
	go func() {
		wg.Wait()
		close(errCh)
	}()
	for err := range errCh {
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	if ctx.Err() != nil && !s.draining.Load() {
		return ctx.Err()
	}
	return nil
}

func (s *WorkerService) slot(ctx context.Context, spec WorkerSpec) error {
	for {
		select {
		case <-s.drain:
			return nil
		default:
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		item, err := s.engine.Poll(ctx, spec)
		if errors.Is(err, ErrNoWork) {
			timer := time.NewTimer(spec.PollInterval)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-s.drain:
				if !timer.Stop() {
					<-timer.C
				}
				return nil
			case <-timer.C:
				continue
			}
		}
		if err != nil {
			return err
		}
		s.active.Add(1)
		execErr := s.engine.executeWorkItem(ctx, spec, item)
		s.active.Add(-1)
		if execErr != nil && !errors.Is(execErr, ErrStaleTask) {
			return execErr
		}
		if _, err := s.engine.Advance(ctx, item.Token.ExecutionID); err != nil && !errors.Is(err, ErrDeadlock) {
			return err
		}
	}
}

func (s *WorkerService) Drain(ctx context.Context) error {
	s.draining.Store(true)
	s.drainOnce.Do(func() { close(s.drain) })
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
