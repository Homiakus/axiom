package profile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/adgo"
)

// ProfileName identifies one of the three canonical integration profiles.
type ProfileName string

const (
	ProfileEmbedded              ProfileName = "Embedded"
	ProfileDurableSingleNode     ProfileName = "DurableSingleNode"
	ProfileDistributedProduction ProfileName = "DistributedProduction"
)

// Guarantees reports documented formal guarantees and non-guarantees.
type Guarantees struct {
	Name          ProfileName `json:"name"`
	Persistence   string      `json:"persistence"`
	Guarantees    []string    `json:"guarantees"`
	NonGuarantees []string    `json:"non_guarantees"`
}

// ── Profile 1: Embedded ──────────────────────────────────────────────────

// EmbeddedConfig defines configuration for the in-process Embedded profile.
type EmbeddedConfig struct{}

// Embedded provides an in-process, zero-dependency, memory-backed execution runtime.
// Suitable for unit tests, embedded agents, CLI utilities, and sub-microsecond ephemeral workflows.
type Embedded struct {
	flowStore axiom.FlowStore
}

// OpenEmbedded creates a canonical Embedded profile instance.
func OpenEmbedded(_ EmbeddedConfig) (*Embedded, error) {
	return &Embedded{
		flowStore: axiom.NewMemoryFlowStore(),
	}, nil
}

// NewEmbedded returns an Embedded profile with default configuration.
func NewEmbedded() *Embedded {
	p, _ := OpenEmbedded(EmbeddedConfig{})
	return p
}

// Close is a no-op for the Embedded profile.
func (p *Embedded) Close() error { return nil }

// FlowStore returns the shared in-memory FlowStore.
func (p *Embedded) FlowStore() axiom.FlowStore { return p.flowStore }

// OpenEmbeddedFlow constructs an in-memory FlowEngine wired to the profile's flow store.
func OpenEmbeddedFlow[S any](p *Embedded, flow *axiom.Flow[S], opts ...axiom.FlowOption) (*axiom.FlowEngine[S], error) {
	if p == nil {
		return nil, fmt.Errorf("profile: embedded profile is required")
	}
	if flow == nil {
		return nil, fmt.Errorf("profile: flow is required")
	}
	allOpts := append([]axiom.FlowOption{axiom.WithFlowStore(p.flowStore)}, opts...)
	return axiom.OpenFlow(flow, allOpts...)
}

// NewEngine constructs an in-memory declarative engine from a compiled module.
func (p *Embedded) NewEngine(mod *axiom.Module, opts ...axiom.Option) (*axiom.Engine, error) {
	if mod == nil {
		return nil, fmt.Errorf("profile: module is required")
	}
	return axiom.New(mod, opts...)
}

// CompileAndNew compiles .axm source and creates an in-memory engine.
func (p *Embedded) CompileAndNew(source []byte, opts ...axiom.Option) (*axiom.Engine, error) {
	return axiom.CompileAndNew(source, opts...)
}

// Guarantees reports the guarantees and non-guarantees for the Embedded profile.
func (p *Embedded) Guarantees() Guarantees {
	return Guarantees{
		Name:        ProfileEmbedded,
		Persistence: "Ephemeral (In-Memory)",
		Guarantees: []string{
			"Sub-microsecond state transition latency",
			"Zero filesystem and zero external service dependencies",
			"Thread-safe concurrent execution within host process",
			"Deterministic execution ordering and state machine invariants",
		},
		NonGuarantees: []string{
			"State does NOT survive process termination, crash, or restart",
			"No multi-host coordination or distributed worker dispatch",
			"No durable transactional outbox across process boundaries",
		},
	}
}

// ── Profile 2: Durable Single Node ───────────────────────────────────────

// DurableSingleNodeConfig configures the single-node Pebble-backed durable profile.
type DurableSingleNodeConfig struct {
	Dir        string
	SyncWrites bool // if true (default), commits are synchronously fsync'ed to disk
}

// DurableSingleNode provides synchronous Pebble-backed durability for single-host
// services requiring restart recovery, crash resilience, and transactional outbox.
type DurableSingleNode struct {
	mu          sync.Mutex
	dir         string
	syncWrites  bool
	engineStore *axiom.PebbleStore
	flowStore   *axiom.PebbleFlowStore
}

// OpenDurableSingleNode opens or initializes a durable single-node profile in cfg.Dir.
func OpenDurableSingleNode(cfg DurableSingleNodeConfig) (*DurableSingleNode, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("profile: durable single node directory is required")
	}
	if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("profile: create dir: %w", err)
	}
	return &DurableSingleNode{
		dir:        cfg.Dir,
		syncWrites: cfg.SyncWrites,
	}, nil
}

// Dir returns the root storage directory for this profile.
func (p *DurableSingleNode) Dir() string { return p.dir }

// EngineStore returns or lazily opens the Pebble store for declarative modules.
func (p *DurableSingleNode) EngineStore() (*axiom.PebbleStore, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.engineStore != nil {
		return p.engineStore, nil
	}
	path := filepath.Join(p.dir, "engine")
	opts := []axiom.PebbleOption{}
	if !p.syncWrites {
		opts = append(opts, axiom.PebbleNoSync())
	}
	store, err := axiom.OpenPebble(path, opts...)
	if err != nil {
		return nil, err
	}
	p.engineStore = store
	return store, nil
}

// FlowStore returns or lazily opens the dedicated Pebble flow store.
func (p *DurableSingleNode) FlowStore() (*axiom.PebbleFlowStore, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.flowStore != nil {
		return p.flowStore, nil
	}
	path := filepath.Join(p.dir, "flow")
	store, err := axiom.OpenPebbleFlowStore(path)
	if err != nil {
		return nil, err
	}
	p.flowStore = store
	return store, nil
}

// OpenDurableFlow constructs a crash-durable FlowEngine wired to the Pebble flow store.
func OpenDurableFlow[S any](p *DurableSingleNode, flow *axiom.Flow[S], opts ...axiom.FlowOption) (*axiom.FlowEngine[S], error) {
	if p == nil {
		return nil, fmt.Errorf("profile: durable single node profile is required")
	}
	fs, err := p.FlowStore()
	if err != nil {
		return nil, err
	}
	allOpts := append([]axiom.FlowOption{
		axiom.WithFlowStore(fs),
		axiom.WithDurableFlowEffects(),
	}, opts...)
	return axiom.OpenFlow(flow, allOpts...)
}

// NewEngine constructs a crash-durable declarative engine from a compiled module.
func (p *DurableSingleNode) NewEngine(mod *axiom.Module, opts ...axiom.Option) (*axiom.Engine, error) {
	es, err := p.EngineStore()
	if err != nil {
		return nil, err
	}
	allOpts := append([]axiom.Option{
		axiom.WithStore(es),
		axiom.WithProductionMode(),
	}, opts...)
	return axiom.New(mod, allOpts...)
}

// CompileAndNew compiles .axm source and constructs a crash-durable declarative engine.
func (p *DurableSingleNode) CompileAndNew(source []byte, opts ...axiom.Option) (*axiom.Engine, error) {
	es, err := p.EngineStore()
	if err != nil {
		return nil, err
	}
	allOpts := append([]axiom.Option{
		axiom.WithStore(es),
		axiom.WithProductionMode(),
	}, opts...)
	return axiom.CompileAndNew(source, allOpts...)
}

// OpenAdgoProduction initializes an adgo.Production runtime backed by Pebble in this directory.
func (p *DurableSingleNode) OpenAdgoProduction(plan *adgo.Plan, registry *adgo.Registry) (*adgo.Production, error) {
	cfg := adgo.DefaultProductionConfig(filepath.Join(p.dir, "adgo"))
	cfg.Backend = adgo.BackendPebble
	cfg.PebbleNoSync = !p.syncWrites
	return adgo.OpenProduction(plan, registry, cfg)
}

// Close closes any opened Pebble storage handles.
func (p *DurableSingleNode) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var errs []error
	if p.engineStore != nil {
		if err := p.engineStore.Close(); err != nil {
			errs = append(errs, err)
		}
		p.engineStore = nil
	}
	if p.flowStore != nil {
		if err := p.flowStore.Close(); err != nil {
			errs = append(errs, err)
		}
		p.flowStore = nil
	}
	return errors.Join(errs...)
}

// Guarantees reports the guarantees and non-guarantees for the Durable Single Node profile.
func (p *DurableSingleNode) Guarantees() Guarantees {
	return Guarantees{
		Name:        ProfileDurableSingleNode,
		Persistence: "Synchronous Pebble WAL (StoreDurabilitySynchronous)",
		Guarantees: []string{
			"Crash boundary survival: all committed state transitions and outbox intents survive process termination and restart",
			"Synchronous WAL durability ensuring zero loss of committed state",
			"Monotonic state sequencing and idempotent replay",
			"Single-writer filesystem lock preventing concurrent dual-writer database corruption",
		},
		NonGuarantees: []string{
			"Multi-host active-active concurrent writes (requires single-writer access per storage directory)",
			"Automatic multi-host failover without shared networked storage or external replication",
		},
	}
}

// ── Profile 3: Distributed Production ─────────────────────────────────────

// DistributedProductionConfig configures the multi-role distributed production profile.
type DistributedProductionConfig struct {
	NodeID              string
	RootDir             string
	LeaseTTL            time.Duration
	PollInterval        time.Duration
	CoordinatorInterval time.Duration
	MaxLeaseRecoveries  int
	Router              adgo.RouterConfig
	PebbleNoSync        bool
}

// DefaultDistributedProductionConfig returns recommended default settings for distributed production.
func DefaultDistributedProductionConfig(nodeID, rootDir string) DistributedProductionConfig {
	return DistributedProductionConfig{
		NodeID:              nodeID,
		RootDir:             rootDir,
		LeaseTTL:            30 * time.Second,
		PollInterval:        100 * time.Millisecond,
		CoordinatorInterval: 50 * time.Millisecond,
		MaxLeaseRecoveries:  5,
		Router:              adgo.DefaultRouterConfig(),
	}
}

// DistributedProduction provides an enterprise profile with coordinator/worker separation,
// lease fencing, adaptive provider routing, admission control, and crash recovery.
type DistributedProduction struct {
	nodeID     string
	production *adgo.Production
}

// OpenDistributedProduction opens or connects to a distributed production runtime.
func OpenDistributedProduction(plan *adgo.Plan, registry *adgo.Registry, cfg DistributedProductionConfig) (*DistributedProduction, error) {
	if cfg.RootDir == "" {
		return nil, fmt.Errorf("profile: distributed production root dir is required")
	}
	prodCfg := adgo.ProductionConfig{
		Backend:             adgo.BackendPebble,
		Root:                cfg.RootDir,
		LeaseTTL:            cfg.LeaseTTL,
		PollInterval:        cfg.PollInterval,
		CoordinatorInterval: cfg.CoordinatorInterval,
		MaxLeaseRecoveries:  cfg.MaxLeaseRecoveries,
		Router:              cfg.Router,
		PebbleNoSync:        cfg.PebbleNoSync,
	}
	prod, err := adgo.OpenProduction(plan, registry, prodCfg)
	if err != nil {
		return nil, err
	}
	return &DistributedProduction{
		nodeID:     cfg.NodeID,
		production: prod,
	}, nil
}

// NodeID returns the node identifier.
func (p *DistributedProduction) NodeID() string { return p.nodeID }

// Production returns the underlying adgo.Production instance.
func (p *DistributedProduction) Production() *adgo.Production { return p.production }

// Close closes the distributed production runtime.
func (p *DistributedProduction) Close() error {
	if p == nil || p.production == nil {
		return nil
	}
	return p.production.Close()
}

// RunCoordinator executes the production coordinator loop until ctx is canceled.
func (p *DistributedProduction) RunCoordinator(ctx context.Context) error {
	if p == nil || p.production == nil || p.production.Engine == nil {
		return fmt.Errorf("profile: production runtime is not open")
	}
	return p.production.Engine.RunResilientCoordinator(ctx)
}

// RunWorker executes a worker loop for the given spec until ctx is canceled.
func (p *DistributedProduction) RunWorker(ctx context.Context, spec adgo.WorkerSpec) error {
	if p == nil || p.production == nil || p.production.Engine == nil {
		return fmt.Errorf("profile: production runtime is not open")
	}
	return p.production.Engine.RunWorker(ctx, spec)
}

// Serve runs both the coordinator and the given workers concurrently until ctx is canceled.
func (p *DistributedProduction) Serve(ctx context.Context, workers ...adgo.WorkerSpec) error {
	if p == nil || p.production == nil {
		return fmt.Errorf("profile: production runtime is not open")
	}
	return p.production.Serve(ctx, workers...)
}

// Guarantees reports the guarantees and non-guarantees for the Distributed Production profile.
func (p *DistributedProduction) Guarantees() Guarantees {
	return Guarantees{
		Name:        ProfileDistributedProduction,
		Persistence: "Shared Transactional Store with Leased Fencing",
		Guarantees: []string{
			"Decoupled coordinator and worker processes with independent horizontal scaling",
			"Leased task fencing preventing stale or partitioned workers from committing results",
			"Automatic work reclamation when worker processes crash or miss heartbeat deadlines",
			"Adaptive provider routing with circuit breakers, latency rankings, and health tracking",
			"Global admission rate-limiting and quota controls preventing downstream overload",
		},
		NonGuarantees: []string{
			"Exactly-once delivery of non-idempotent external network calls (requires compensation handlers or idempotency keys)",
			"Multi-master active-active replication without an authoritative shared store or coordination backend",
		},
	}
}
