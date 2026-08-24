package adgo

// WithEngineRepairPlanner installs the RepairPlanner used by the deterministic
// Runtime embedded in a production Engine.
//
// Engine keeps Runtime as its semantic kernel, so repair policy must be
// configurable at the Engine boundary as well; otherwise Engine/OpenProduction
// users cannot preserve domain-specific targeted-repair semantics that already
// work with NewRuntime(..., WithRepairPlanner(...)).
//
// The option is safe to apply during Engine construction. OpenProduction users
// may also apply it immediately after OpenProduction returns and before starting
// coordinators/workers or executions:
//
//     adgo.WithEngineRepairPlanner(planner)(production.Engine)
//
// Existing callers are unaffected; nil planners are ignored.
func WithEngineRepairPlanner(planner RepairPlanner) EngineOption {
	return func(engine *Engine) {
		if planner == nil || engine == nil || engine.runtime == nil {
			return
		}
		engine.runtime.repair = planner
	}
}
