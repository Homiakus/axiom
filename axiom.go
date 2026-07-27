// Package axiom provides an embeddable deterministic workflow, rules and
// state-transition engine for Go.
//
// The package intentionally exposes a compact facade while the parser,
// compiler, runtime, persistence, replay and diagnostics implementation stays
// isolated from application code.
package axiom

import legacy "axiom/pkg/axiom"

// Stable public model and runtime types.
type (
	Module            = legacy.Module
	Diagnostic        = legacy.Diagnostic
	Diagnostics       = legacy.Diagnostics
	Error             = legacy.Error
	Errors            = legacy.Errors
	Engine            = legacy.Engine
	Store             = legacy.Store
	Execution         = legacy.Execution
	ActivityTask      = legacy.ActivityTask
	HistoryEntry      = legacy.HistoryEntry
	WorkerOptions     = legacy.WorkerOptions
	TraceLevel        = legacy.TraceLevel
	FieldID           = legacy.FieldID
	AtomID            = legacy.AtomID
	RuleID            = legacy.RuleID
	SignalID          = legacy.SignalID
	ActivityID        = legacy.ActivityID
	ValueKind         = legacy.ValueKind
	Value             = legacy.Value
	ExecutionState    = legacy.ExecutionState
	SourceMapEntry    = legacy.SourceMapEntry
	TRIZNormalization = legacy.TRIZNormalization
	Input             = legacy.Input
	Output            = legacy.Output
	Patch             = legacy.Patch
	Activity          = legacy.Activity
	ActivityRegistry  = legacy.ActivityRegistry
	App               = legacy.App
	Option            = legacy.Option
	CompileOption     = legacy.CompileOption
	PebbleStore       = legacy.PebbleStore
	PebbleOption      = legacy.PebbleOption
	ModuleBundle      = legacy.ModuleBundle
	BundleDiff        = legacy.BundleDiff
	ImpactReport      = legacy.ImpactReport
)

const (
	TraceAggregate = legacy.TraceAggregate
	TraceFull      = legacy.TraceFull
	TraceMinimal   = legacy.TraceMinimal

	ValueInvalid = legacy.ValueInvalid
	ValueNull    = legacy.ValueNull
	ValueBool    = legacy.ValueBool
	ValueInt     = legacy.ValueInt
	ValueFloat   = legacy.ValueFloat
	ValueString  = legacy.ValueString
	ValueAny     = legacy.ValueAny

	DSLVersion = legacy.DSLVersion
)

// Store configuration.
var (
	PebbleNoSync    = legacy.PebbleNoSync
	PebbleSyncEvery = legacy.PebbleSyncEvery
	PebbleJSONCodec = legacy.PebbleJSONCodec
	PebbleGobCodec  = legacy.PebbleGobCodec
	OpenPebble      = legacy.OpenPebble
	NewMemoryStore  = legacy.NewMemoryStore
	WithStore       = legacy.WithStore
)

// Activity registration and runtime configuration.
var (
	Act                   = legacy.Act
	Acts                  = legacy.Acts
	WithActivity          = legacy.WithActivity
	WithActivities        = legacy.WithActivities
	Register              = legacy.Register
	WithStrictFastRuntime = legacy.WithStrictFastRuntime
	WithProductionMode    = legacy.WithProductionMode
	WithTraceLevel        = legacy.WithTraceLevel
)

// Compilation, loading and engine construction.
var (
	Load              = legacy.Load
	MustLoad          = legacy.MustLoad
	Compile           = legacy.Compile
	CompileAny        = legacy.CompileAny
	NormalizeTRIZ     = legacy.NormalizeTRIZ
	MustCompile       = legacy.MustCompile
	CompileAndNew     = legacy.CompileAndNew
	MustCompileAndNew = legacy.MustCompileAndNew
	WithSourceName    = legacy.WithSourceName
	New               = legacy.New
	MustNew           = legacy.MustNew
	LoadModule        = legacy.LoadModule
	NewEngine         = legacy.NewEngine
)

// Analysis and recovery.
var (
	ReplayFromHistory = legacy.ReplayFromHistory
	CompileBundle     = legacy.CompileBundle
)
