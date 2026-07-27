package axiom

import "fmt"

// AnalysisLevel describes how much static analysis a Plan supports.
type AnalysisLevel string

const (
	AnalysisOpaque AnalysisLevel = "opaque"
	AnalysisStatic AnalysisLevel = "static"
)

// Plan is the canonical executable representation consumed by Axiom.
// Frontends such as Go builders, AXM and TOML all compile into this type.
type Plan struct {
	Name     string
	Version  string
	Digest   string
	Format   string
	Analysis AnalysisLevel
	module   *Module
}

// NewPlan wraps a validated compiled module as a canonical Plan.
func NewPlan(module *Module, format, version string, analysis AnalysisLevel) (*Plan, error) {
	if module == nil {
		return nil, fmt.Errorf("axiom: module is required")
	}
	if format == "" {
		format = "unknown"
	}
	if version == "" {
		version = module.DSLVersion
	}
	if analysis == "" {
		analysis = AnalysisStatic
	}
	return &Plan{
		Name:     module.Domain,
		Version:  version,
		Digest:   module.CompiledHash,
		Format:   format,
		Analysis: analysis,
		module:   module,
	}, nil
}

// CompilePlan compiles AXM or TRIZ source into a canonical Plan.
func CompilePlan(source []byte, opts ...CompileOption) (*Plan, error) {
	module, err := CompileAny(source, opts...)
	if err != nil {
		return nil, err
	}
	return NewPlan(module, "axm", module.DSLVersion, AnalysisStatic)
}

// Module returns the validated runtime module backing the Plan.
func (p *Plan) Module() *Module {
	if p == nil {
		return nil
	}
	return p.module
}

// CompilePlan lets a Plan satisfy PlanSource.
func (p *Plan) CompilePlan() (*Plan, error) {
	if p == nil || p.module == nil {
		return nil, fmt.Errorf("axiom: plan is required")
	}
	return p, nil
}

// New creates an Engine from the Plan.
func (p *Plan) New(opts ...Option) (*Engine, error) {
	if p == nil || p.module == nil {
		return nil, fmt.Errorf("axiom: plan is required")
	}
	return New(p.module, opts...)
}

// PlanSource is implemented by AXM, TOML and Go model frontends.
type PlanSource interface {
	CompilePlan() (*Plan, error)
}

// Open compiles a source and creates an Engine in one operation.
func Open(source PlanSource, opts ...Option) (*Engine, error) {
	if source == nil {
		return nil, fmt.Errorf("axiom: plan source is required")
	}
	plan, err := source.CompilePlan()
	if err != nil {
		return nil, err
	}
	return plan.New(opts...)
}
