// Package axiom is the public API for loading .axm modules, wiring Go
// activities, and running Axiom executions.
//
// # Quick Start
//
//	// One-liner for simple cases:
//	engine, err := axiom.CompileAndNew(source, axiom.Act("SendEmail", sendEmail))
//
//	// From file with full control:
//	app, err := axiom.Load("module.axm")
//	engine := app.MustNew(
//	    axiom.Act("SendEmail", sendEmail),
//	    axiom.WithProductionMode(),
//	)
//	engine.Start(ctx, "exec-1", nil)
//
// # Stores
//
// Memory store is the default (no option needed). For durability use Pebble:
//
//	store, err := axiom.OpenPebble("data/axiom")
//	engine := app.MustNew(axiom.WithStore(store))
//
// # Activity Registration
//
// Use ActTyped for application code when inputs and outputs have stable Go
// shapes. Use Act/Acts for dynamic integration boundaries that naturally use
// map payloads. The engine validates that every .axm activity with
// effect!=none has a Go handler.
package axiom

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/Homiakus/axiom/internal/compiler"
	"github.com/Homiakus/axiom/internal/diag"
	runtimepkg "github.com/Homiakus/axiom/internal/runtime"
	"github.com/Homiakus/axiom/internal/store/memory"
	pebblestore "github.com/Homiakus/axiom/internal/store/pebble"
	"github.com/Homiakus/axiom/internal/triz"
)

// ── Type aliases (stable public surface) ──────────────────────────────────

type Module = compiler.Module
type Diagnostic = compiler.Diagnostic
type Diagnostics = compiler.Diagnostics
type Error = diag.Error
type Errors = diag.Errors

type Engine = runtimepkg.Engine
type Store = runtimepkg.Store
type Execution = runtimepkg.Execution
type ActivityTask = runtimepkg.ActivityTask
type HistoryEntry = runtimepkg.HistoryEntry
type WorkerOptions = runtimepkg.WorkerOptions
type TraceLevel = runtimepkg.TraceLevel
type FieldID = runtimepkg.FieldID
type AtomID = runtimepkg.AtomID
type RuleID = runtimepkg.RuleID
type SignalID = runtimepkg.SignalID
type ActivityID = runtimepkg.ActivityID
type ValueKind = runtimepkg.ValueKind
type Value = runtimepkg.Value
type ExecutionState = runtimepkg.ExecutionState

type SourceMapEntry struct {
	TRIZKind string
	TRIZName string
	TRIZLine int
	V0Kind   string
	V0Name   string
	V0Line   int
}

type TRIZNormalization struct {
	Source           []byte
	NormalizedSource []byte
	Module           *Module
	Diagnostics      Diagnostics
	SourceMap        []SourceMapEntry
}

// ── Constants ─────────────────────────────────────────────────────────────

const (
	TraceAggregate TraceLevel = runtimepkg.TraceAggregate
	TraceFull      TraceLevel = runtimepkg.TraceFull
	TraceMinimal   TraceLevel = runtimepkg.TraceMinimal

	ValueInvalid ValueKind = runtimepkg.ValueInvalid
	ValueNull    ValueKind = runtimepkg.ValueNull
	ValueBool    ValueKind = runtimepkg.ValueBool
	ValueInt     ValueKind = runtimepkg.ValueInt
	ValueFloat   ValueKind = runtimepkg.ValueFloat
	ValueString  ValueKind = runtimepkg.ValueString
	ValueAny     ValueKind = runtimepkg.ValueAny
)

// ── Domain types ──────────────────────────────────────────────────────────

// Input is the payload passed into a signal or an activity.
type Input = map[string]any

// Output is the result returned by an activity.
type Output = map[string]any

// Patch is a set of context field changes.
type Patch = map[string]any

// Activity is a Go function that implements a .axm activity block.
// ctx is canceled when the execution is canceled or times out.
type Activity func(ctx context.Context, input Input) (Output, error)

// ActivityRegistry maps .axm activity names to their Go implementations.
type ActivityRegistry map[string]Activity

// ── App: parsed module ready for engine construction ──────────────────────

// App holds a parsed and compiled .axm module, ready to create engines.
type App struct {
	Path   string
	Module *Module
}

// ── Engine options ────────────────────────────────────────────────────────

type engineConfig struct {
	store      Store
	activities ActivityRegistry
	strictFast bool
	production bool
	traceLevel TraceLevel
}

// Option configures an Engine before it is built.
// Options are validated at Build/New time (fail-fast).
type Option func(*engineConfig) error

// ── Store options ─────────────────────────────────────────────────────────

// WithStore sets an explicit store. Use this when you need Pebble durability
// or a custom Store implementation. For simple cases the default memory store
// is used automatically.
func WithStore(store Store) Option {
	return func(c *engineConfig) error {
		if store == nil {
			return Errors{{Code: "AX500", Kind: "config", Message: "store must not be nil", Hint: "Omit WithStore to use the default memory store, or pass a valid Store."}}
		}
		c.store = store
		return nil
	}
}

// ── Pebble integration ────────────────────────────────────────────────────

// PebbleStore is a durable on-disk Store backed by CockroachDB Pebble.
type PebbleStore = pebblestore.Store

// PebbleOption configures a Pebble store.
type PebbleOption = pebblestore.Option

// PebbleNoSync disables WAL sync (fast, less durable).
var PebbleNoSync = pebblestore.WithNoSync

// PebbleSyncEvery batches syncs at the given interval.
var PebbleSyncEvery = pebblestore.WithSyncEvery

// PebbleJSONCodec uses JSON instead of Gob for encoding.
var PebbleJSONCodec = pebblestore.WithJSONCodec

// PebbleGobCodec uses Gob encoding (default).
var PebbleGobCodec = pebblestore.WithGobCodec

// OpenPebble opens a Pebble-backed durable store. Fail-fast: returns error
// immediately if the store cannot be opened.
//
//	store, err := axiom.OpenPebble("data/axiom", axiom.PebbleNoSync())
//	engine := app.MustNew(axiom.WithStore(store))
func OpenPebble(path string, opts ...PebbleOption) (*PebbleStore, error) {
	return pebblestore.Open(path, opts...)
}

// ── Activity registration ─────────────────────────────────────────────────

// Act registers a single activity. Shorthand for WithActivity.
//
//	axiom.Act("SendWelcomeEmail", func(ctx context.Context, in axiom.Input) (axiom.Output, error) {
//	    return axiom.Output{"sent": true}, nil
//	})
func Act(name string, fn Activity) Option {
	return func(c *engineConfig) error {
		if name == "" {
			return Errors{{Code: "AX500", Kind: "config", Message: "activity name must not be empty"}}
		}
		if fn == nil {
			return Errors{{Code: "AX500", Kind: "config", Entity: name, Message: "activity function must not be nil", Hint: "Pass a non-nil Activity func."}}
		}
		if c.activities == nil {
			c.activities = ActivityRegistry{}
		}
		c.activities[name] = fn
		return nil
	}
}

// ActTyped registers an activity with typed Go inputs and outputs.
// Input and output types must be structs (or pointers to structs) or maps with
// string keys. Unsupported shapes fail during Engine construction instead of
// producing a late decode error or a silently empty output.
//
//	axiom.ActTyped("SendWelcomeEmail", func(ctx context.Context, in WelcomeInput) (WelcomeOutput, error) {
//	    return WelcomeOutput{Sent: true}, nil
//	})
func ActTyped[In any, Out any](name string, fn func(ctx context.Context, input In) (Out, error)) Option {
	if fn == nil {
		return optionError(Errors{{
			Code:    "AX507",
			Kind:    "config",
			Entity:  name,
			Message: "typed activity function must not be nil",
			Hint:    "Pass a non-nil typed activity function.",
		}})
	}
	if err := validateTypedActivityShape[In](name, "input"); err != nil {
		return optionError(err)
	}
	if err := validateTypedActivityShape[Out](name, "output"); err != nil {
		return optionError(err)
	}

	return Act(name, func(ctx context.Context, input Input) (Output, error) {
		data, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("axiom: marshal activity input for %s: %w", name, err)
		}
		var typedIn In
		if err := json.Unmarshal(data, &typedIn); err != nil {
			return nil, fmt.Errorf("axiom: decode activity input for %s: %w", name, err)
		}
		typedOut, err := fn(ctx, typedIn)
		if err != nil {
			return nil, err
		}
		return structToOutput(typedOut), nil
	})
}

func optionError(err error) Option {
	return func(*engineConfig) error { return err }
}

func validateTypedActivityShape[T any](name, role string) error {
	typ := reflect.TypeFor[T]()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	supported := typ.Kind() == reflect.Struct || (typ.Kind() == reflect.Map && typ.Key().Kind() == reflect.String)
	if supported {
		return nil
	}

	return Errors{{
		Code:    "AX507",
		Kind:    "config",
		Entity:  name,
		Message: fmt.Sprintf("typed activity %s must be a struct or a map with string keys; got %s", role, typ),
		Hint:    "Use a struct (recommended) or a map whose key type is string. Use axiom.Act for dynamic scalar integration boundaries.",
	}}
}

func structToOutput(v any) Output {
	val := reflect.ValueOf(v)
	for val.Kind() == reflect.Pointer {
		val = val.Elem()
	}
	if !val.IsValid() {
		return nil
	}
	if val.Kind() == reflect.Map && val.Type().Key().Kind() == reflect.String {
		out := Output{}
		iter := val.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = iter.Value().Interface()
		}
		return out
	}
	if val.Kind() != reflect.Struct {
		return nil
	}
	out := Output{}
	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		sf := typ.Field(i)
		if !sf.IsExported() {
			continue
		}
		name := sf.Tag.Get("axiom")
		if name == "" {
			name = strings.Split(sf.Tag.Get("json"), ",")[0]
		}
		if name == "" {
			if len(sf.Name) > 0 {
				name = strings.ToLower(sf.Name[:1]) + sf.Name[1:]
			}
		}
		if name == "-" {
			continue
		}
		out[name] = val.Field(i).Interface()
	}
	return out
}

// Acts registers multiple activities from a registry.
//
//	engine := app.MustNew(axiom.Acts(axiom.ActivityRegistry{
//	    "CheckInventory": checkInventory,
//	    "ChargeCard":     chargeCard,
//	}))
func Acts(registry ActivityRegistry) Option {
	return func(c *engineConfig) error {
		if len(registry) == 0 {
			return nil
		}
		if c.activities == nil {
			c.activities = ActivityRegistry{}
		}
		for name, fn := range registry {
			if name == "" {
				return Errors{{Code: "AX500", Kind: "config", Message: "activity name must not be empty in registry"}}
			}
			if fn == nil {
				return Errors{{Code: "AX500", Kind: "config", Entity: name, Message: "activity function must not be nil", Hint: "Pass a non-nil Activity func for " + name + "."}}
			}
			c.activities[name] = fn
		}
		return nil
	}
}

// WithActivity registers a single activity (alias for Act).
func WithActivity(name string, fn Activity) Option { return Act(name, fn) }

// Register is a deprecated alias for WithActivity. Use Act.
//
// Deprecated: Use Act(name, fn) instead.
func Register(name string, fn Activity) Option { return Act(name, fn) }

// WithActivities registers multiple activities (alias for Acts).
func WithActivities(registry ActivityRegistry) Option { return Acts(registry) }

// ── Runtime behavior ─────────────────────────────────────────────────────

// WithStrictFastRuntime enables strict mode: refuses to fall back to the
// slow path. Use in tests to catch unsupported .axm patterns early.
func WithStrictFastRuntime() Option {
	return func(c *engineConfig) error {
		c.strictFast = true
		return nil
	}
}

// WithProductionMode enables production safeguards:
//   - Strict fast runtime (no slow-path fallback)
//   - Transactional store required (Pebble or custom durable store)
//   - durable retry/backoff and per-attempt timeout
//   - concurrency: once is serialized per activity within an Engine
//   - concurrency: parallel remains unrestricted
//   - concurrency: first/latest use transactional pending-task supersession
func WithProductionMode() Option {
	return func(c *engineConfig) error {
		c.production = true
		c.strictFast = true
		return nil
	}
}

// WithTraceLevel controls how much execution detail is recorded in history.
//
//   - TraceMinimal:   only errors and lifecycle events
//   - TraceAggregate: summary of rule evaluation per turn (default)
//   - TraceFull:      every rule attempt, every activity scheduling
func WithTraceLevel(level TraceLevel) Option {
	return func(c *engineConfig) error {
		switch level {
		case TraceMinimal, TraceAggregate, TraceFull, "":
			c.traceLevel = level
			return nil
		default:
			return Errors{{Code: "AX500", Kind: "config", Entity: string(level), Message: fmt.Sprintf("unknown trace level: %s", level), Hint: "Use TraceMinimal, TraceAggregate, or TraceFull."}}
		}
	}
}

// ── Module loading ────────────────────────────────────────────────────────

// Load reads a .axm file from disk, compiles it, and returns an App.
//
//	app, err := axiom.Load("module.axm")
//	engine := app.MustNew(axiom.Act("MyActivity", myFunc))
func Load(path string) (*App, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, Errors{{Code: "AX600", Kind: "config", File: path, Message: fmt.Sprintf("read module file: %v", err), Cause: err}}
	}
	module, err := Compile(source, WithSourceName(path))
	if err != nil {
		return nil, err
	}
	return &App{Path: path, Module: module}, nil
}

// MustLoad is like Load but panics on error. Use in tests and init scripts.
func MustLoad(path string) *App {
	app, err := Load(path)
	if err != nil {
		panic(fmt.Sprintf("axiom.MustLoad(%q): %v", path, err))
	}
	return app
}

// Compile parses and compiles raw .axm source into a compiled Module.
// Use WithSourceName to set the source name for error messages.
func Compile(source []byte, opts ...CompileOption) (*Module, error) {
	cfg := compileConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	module, err := compiler.Compile(source)
	if err != nil {
		return nil, attachSourceName(err, cfg.sourceName)
	}
	return module, nil
}

// CompileAny compiles either the stable Axiom v0 syntax (`domain ...`) or the
// user-facing TRIZ syntax (`system ...`). Existing Compile remains v0-only for
// compatibility with older callers.
func CompileAny(source []byte, opts ...CompileOption) (*Module, error) {
	if triz.LooksLike(source) {
		result, err := NormalizeTRIZ(source, opts...)
		if err != nil {
			return nil, err
		}
		return result.Module, nil
	}
	return Compile(source, opts...)
}

// NormalizeTRIZ parses TRIZ DSL, emits equivalent Axiom v0 source, compiles it,
// and returns source-map and diagnostic data for tools such as Axiom Studio.
func NormalizeTRIZ(source []byte, opts ...CompileOption) (*TRIZNormalization, error) {
	cfg := compileConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	normalized, err := triz.Normalize(source)
	if err != nil {
		return nil, attachSourceName(err, cfg.sourceName)
	}
	result := &TRIZNormalization{
		Source:           append([]byte(nil), source...),
		NormalizedSource: append([]byte(nil), normalized.Source...),
		Diagnostics:      normalized.Diagnostics,
		SourceMap:        make([]SourceMapEntry, 0, len(normalized.SourceMap)),
	}
	for _, entry := range normalized.SourceMap {
		result.SourceMap = append(result.SourceMap, SourceMapEntry{
			TRIZKind: entry.TRIZKind,
			TRIZName: entry.TRIZName,
			TRIZLine: entry.TRIZLine,
			V0Kind:   entry.V0Kind,
			V0Name:   entry.V0Name,
			V0Line:   entry.V0Line,
		})
	}
	module, err := Compile(normalized.Source, opts...)
	if err != nil {
		return result, err
	}
	result.Module = module
	return result, nil
}

// MustCompile is like Compile but panics on error.
func MustCompile(source []byte, opts ...CompileOption) *Module {
	module, err := Compile(source, opts...)
	if err != nil {
		panic(fmt.Sprintf("axiom.MustCompile: %v", err))
	}
	return module
}

// CompileAndNew compiles source and builds an Engine in one call.
// Memory store is used by default. Use WithStore for Pebble.
//
//	engine, err := axiom.CompileAndNew(source, axiom.Act("MyActivity", myFunc))
func CompileAndNew(source []byte, opts ...Option) (*Engine, error) {
	module, err := Compile(source)
	if err != nil {
		return nil, err
	}
	return New(module, opts...)
}

// MustCompileAndNew is like CompileAndNew but panics on error.
func MustCompileAndNew(source []byte, opts ...Option) *Engine {
	engine, err := CompileAndNew(source, opts...)
	if err != nil {
		panic(fmt.Sprintf("axiom.MustCompileAndNew: %v", err))
	}
	return engine
}

// ── Compile options ──────────────────────────────────────────────────────

type compileConfig struct {
	sourceName string
}

// CompileOption configures the compiler.
type CompileOption func(*compileConfig)

// WithSourceName sets the filename used in error messages.
func WithSourceName(name string) CompileOption {
	return func(cfg *compileConfig) {
		cfg.sourceName = name
	}
}

// ── Engine construction ──────────────────────────────────────────────────

// New builds an Engine from a compiled Module.
//
// Memory store is used by default. Pass WithStore for Pebble durability.
// Every .axm activity with effect != "none" must have a Go handler registered
// via Act/Acts/WithActivity.
//
//	engine, err := axiom.New(module,
//	    axiom.Act("SendEmail", sendEmail),
//	    axiom.WithTraceLevel(axiom.TraceFull),
//	)
func New(module *Module, opts ...Option) (*Engine, error) {
	if module == nil {
		return nil, Errors{{Code: "AX500", Kind: "config", Message: "module is required", Hint: "Pass the module returned by Load or Compile."}}
	}
	if err := ValidateRuntimeQueryProjections(module); err != nil {
		return nil, err
	}

	cfg := engineConfig{
		store:      memory.NewStore(),
		activities: ActivityRegistry{},
		traceLevel: TraceAggregate,
	}

	// Collect options, fail-fast on any error.
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}

	// Production mode requires transactional storage so retry checkpoints and
	// pending-task supersession decisions are committed atomically.
	if cfg.production {
		if _, ok := cfg.store.(runtimepkg.TransactionalStore); !ok {
			return nil, Errors{{Code: "AX506", Kind: "config", Message: "production mode requires a transactional store", Hint: "Use OpenPebble or provide a Store that implements TransactionalStore."}}
		}
		if err := validateProductionPolicyConfig(module); err != nil {
			return nil, err
		}
	}

	// Durability and expression mode are independent. A transactional store
	// such as Pebble must not silently enable strict expression validation.
	// Strict mode is enabled only by WithStrictFastRuntime or WithProductionMode.

	// Validate activities match the module.
	if err := validateActivityConfig(module, cfg.activities); err != nil {
		return nil, err
	}

	engine := runtimepkg.NewEngine(module, cfg.store, toRuntimeActivities(cfg.activities))

	if cfg.strictFast {
		if err := engine.EnableStrictFastRuntime(); err != nil {
			return nil, err
		}
	}
	engine.SetTraceLevel(cfg.traceLevel)
	return engine, nil
}

// MustNew is like New but panics on error.
func MustNew(module *Module, opts ...Option) *Engine {
	engine, err := New(module, opts...)
	if err != nil {
		panic(fmt.Sprintf("axiom.MustNew: %v", err))
	}
	return engine
}

// ── App methods ──────────────────────────────────────────────────────────

// New builds an Engine from this App.
func (a *App) New(opts ...Option) (*Engine, error) {
	if a == nil {
		return nil, Errors{{Code: "AX500", Kind: "config", Message: "app is required"}}
	}
	return New(a.Module, opts...)
}

// MustNew builds an Engine from this App, panicking on error.
func (a *App) MustNew(opts ...Option) *Engine {
	return MustNew(a.Module, opts...)
}

// ── Replay ───────────────────────────────────────────────────────────────

// ReplayFromHistory reconstructs an Execution state from its history entries.
func ReplayFromHistory(module *Module, history []HistoryEntry) (*Execution, error) {
	return runtimepkg.ReplayFromHistory(module, history)
}

// ── Deprecated wrappers ──────────────────────────────────────────────────

// LoadModule is a compatibility wrapper. Use Compile instead.
//
// Deprecated: Use Compile(source).
func LoadModule(source []byte) (*Module, error) {
	return Compile(source)
}

// NewEngine is a compatibility wrapper. Use New with options.
//
// Deprecated: Use New(module, WithStore(store), WithActivities(activities)).
func NewEngine(module *Module, store Store, activities ActivityRegistry) *Engine {
	if store == nil {
		store = memory.NewStore()
	}
	engine, err := New(module, WithStore(store), WithActivities(activities))
	if err != nil {
		// Fall back to pre-validation behavior for backwards compat.
		return runtimepkg.NewEngine(module, store, toRuntimeActivities(activities))
	}
	return engine
}

// NewMemoryStore creates an in-memory store. Use when you need explicit store
// ownership (e.g. passing the same store to multiple engines).
func NewMemoryStore() Store {
	return memory.NewStore()
}

// ── Helpers ──────────────────────────────────────────────────────────────

func attachSourceName(err error, sourceName string) error {
	if sourceName == "" {
		return err
	}
	if diagnostics, ok := err.(Diagnostics); ok {
		out := make(Errors, len(diagnostics))
		for i, diagnostic := range diagnostics {
			diagnostic.File = sourceName
			out[i] = diagnostic
		}
		return out
	}
	if diagnostic, ok := err.(Error); ok {
		diagnostic.File = sourceName
		return diagnostic
	}
	return Errors{{Code: "AX000", Kind: "compile", File: sourceName, Message: err.Error(), Cause: err}}
}

func validateActivityConfig(module *Module, activities ActivityRegistry) error {
	var errs Errors
	for _, activity := range module.Activities {
		if activity.Effect == "none" {
			continue
		}
		if activities == nil || activities[activity.Name] == nil {
			errs = append(errs, Error{
				Code:    "AX501",
				Kind:    "config",
				Entity:  activity.Name,
				Line:    activity.Line,
				Message: fmt.Sprintf("activity %s is declared in .axm but not registered in Go", activity.Name),
				Hint:    fmt.Sprintf("Register it with axiom.Act(%q, fn).", activity.Name),
			})
		}
	}
	for name := range activities {
		if _, ok := module.Activities[name]; !ok {
			errs = append(errs, Error{
				Code:    "AX502",
				Kind:    "config",
				Entity:  name,
				Message: fmt.Sprintf("activity %s is registered in Go but not declared in .axm", name),
				Hint:    "Remove the registration or add the matching activity block to the .axm module.",
			})
		}
	}
	sort.Slice(errs, func(i, j int) bool {
		return errs[i].Entity < errs[j].Entity
	})
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func validateProductionPolicyConfig(module *Module) error {
	var errs Errors
	for name, policy := range module.Policies {
		expr := policy.Entries["concurrency"]
		if expr == nil {
			continue
		}
		value := fmt.Sprint(expr.Value)
		switch value {
		case "", "parallel", "once", "latest", "first":
			continue
		default:
			errs = append(errs, Error{
				Code:    "AX508",
				Kind:    "config",
				Entity:  name + ".concurrency",
				Line:    policy.Line,
				Message: fmt.Sprintf("unsupported concurrency mode %q", value),
				Hint:    "Use concurrency: parallel, once, latest, or first.",
			})
		}
	}

	sort.Slice(errs, func(i, j int) bool {
		return errs[i].Entity < errs[j].Entity
	})
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func toRuntimeActivities(activities ActivityRegistry) runtimepkg.ActivityRegistry {
	out := runtimepkg.ActivityRegistry{}
	for name, fn := range activities {
		if fn == nil {
			continue
		}
		activity := fn
		out[name] = func(ctx context.Context, input map[string]any) (map[string]any, error) {
			return activity(ctx, input)
		}
	}
	return out
}
