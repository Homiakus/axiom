package adgo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type ProductionBackend string

const (
	BackendMemory ProductionBackend = "memory"
	BackendFile   ProductionBackend = "file"
	BackendPebble ProductionBackend = "pebble"
)

type ProductionConfig struct {
	Backend             ProductionBackend
	Root                string
	LeaseTTL            time.Duration
	PollInterval        time.Duration
	CoordinatorInterval time.Duration
	MaxLeaseRecoveries  int
	Router               RouterConfig
	PebbleNoSync         bool
}

func DefaultProductionConfig(root string) ProductionConfig {
	return ProductionConfig{
		Backend:             BackendPebble,
		Root:                root,
		LeaseTTL:            30 * time.Second,
		PollInterval:        100 * time.Millisecond,
		CoordinatorInterval: 50 * time.Millisecond,
		MaxLeaseRecoveries:  5,
		Router:               DefaultRouterConfig(),
	}
}

// Production bundles the core engine with the durable auxiliary control-plane
// services normally required by a real deployment: provider health, global
// admission, activity-result cache and schedules. Applications can still replace
// any of these primitives individually, but do not need to assemble them by hand.
type Production struct {
	Engine         *Engine
	Store          Store
	Router         *AdaptiveRouter
	Admission      AdmissionController
	Cache          ActivityCache
	Schedules      ScheduleStore
	ScheduleRunner *ScheduleRunner

	close func() error
}

func OpenProduction(plan *Plan, registry *Registry, config ProductionConfig) (*Production, error) {
	if plan == nil {
		return nil, fmt.Errorf("adgo: production plan is required")
	}
	if registry == nil {
		registry = NewRegistry()
	}
	if config.Backend == "" {
		config.Backend = BackendPebble
	}
	if config.Backend != BackendMemory && config.Root == "" {
		return nil, fmt.Errorf("adgo: production root is required for backend %s", config.Backend)
	}
	if config.LeaseTTL <= 0 {
		config.LeaseTTL = 30 * time.Second
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 100 * time.Millisecond
	}
	if config.CoordinatorInterval <= 0 {
		config.CoordinatorInterval = 50 * time.Millisecond
	}
	if config.MaxLeaseRecoveries <= 0 {
		config.MaxLeaseRecoveries = 5
	}
	config.Router = normalizeRouterConfig(config.Router)

	production := &Production{}
	var healthStore ProviderHealthStore

	switch config.Backend {
	case BackendMemory:
		production.Store = NewMemoryStore()
		healthStore = NewMemoryProviderHealthStore()
		production.Admission = NewMemoryAdmissionController()
		production.Cache = NewMemoryActivityCache()
		production.Schedules = NewMemoryScheduleStore()
		production.close = func() error { return nil }

	case BackendFile:
		if err := os.MkdirAll(config.Root, privateStateDirMode); err != nil {
			return nil, err
		}
		state, err := NewFileStore(filepath.Join(config.Root, "state"))
		if err != nil {
			return nil, err
		}
		production.Store = state
		healthStore, err = NewFileProviderHealthStore(filepath.Join(config.Root, "control"))
		if err != nil {
			return nil, err
		}
		production.Admission, err = NewFileAdmissionController(filepath.Join(config.Root, "control"))
		if err != nil {
			return nil, err
		}
		production.Cache, err = NewFileActivityCache(filepath.Join(config.Root, "control"))
		if err != nil {
			return nil, err
		}
		production.Schedules, err = NewFileScheduleStore(filepath.Join(config.Root, "control"))
		if err != nil {
			return nil, err
		}
		production.close = func() error { return nil }

	case BackendPebble:
		if err := os.MkdirAll(config.Root, privateStateDirMode); err != nil {
			return nil, err
		}
		pebbleOptions := []PebbleStoreOption{}
		if config.PebbleNoSync {
			pebbleOptions = append(pebbleOptions, WithPebbleNoSync())
		}
		state, err := OpenPebbleStore(filepath.Join(config.Root, "pebble"), pebbleOptions...)
		if err != nil {
			return nil, err
		}
		production.Store = state
		control := filepath.Join(config.Root, "control")
		healthStore, err = NewFileProviderHealthStore(control)
		if err != nil {
			return nil, errors.Join(err, state.Close())
		}
		production.Admission, err = NewFileAdmissionController(control)
		if err != nil {
			return nil, errors.Join(err, state.Close())
		}
		production.Cache, err = NewFileActivityCache(control)
		if err != nil {
			return nil, errors.Join(err, state.Close())
		}
		production.Schedules, err = NewFileScheduleStore(control)
		if err != nil {
			return nil, errors.Join(err, state.Close())
		}
		production.close = state.Close

	default:
		return nil, fmt.Errorf("adgo: unsupported production backend %q", config.Backend)
	}

	production.Router = NewDurableAdaptiveRouter(registry, config.Router, healthStore)
	engine, err := NewEngine(
		plan,
		production.Store,
		registry,
		WithEngineLeaseTTL(config.LeaseTTL),
		WithEnginePollInterval(config.PollInterval),
		WithCoordinatorInterval(config.CoordinatorInterval),
		WithMaxLeaseRecoveries(config.MaxLeaseRecoveries),
		WithAdaptiveRouter(production.Router),
	)
	if err != nil {
		return nil, errors.Join(err, production.Close())
	}
	production.Engine = engine
	production.ScheduleRunner, err = NewScheduleRunner(engine, production.Schedules)
	if err != nil {
		return nil, errors.Join(err, production.Close())
	}
	return production, nil
}

func (p *Production) Close() error {
	if p == nil || p.close == nil {
		return nil
	}
	return p.close()
}

func (p *Production) Serve(ctx context.Context, workers ...WorkerSpec) error {
	if p == nil || p.Engine == nil {
		return fmt.Errorf("adgo: production runtime is not open")
	}
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, len(workers)+2)
	go func() { errCh <- p.Engine.RunResilientCoordinator(serveCtx) }()
	go func() { errCh <- p.ScheduleRunner.Run(serveCtx) }()
	for _, spec := range workers {
		spec := spec
		go func() { errCh <- p.Engine.RunWorker(serveCtx, spec) }()
	}
	err := <-errCh
	cancel()
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
