package axiom

import "context"

type flowDurableFailpointStage string

const (
	flowFailpointBeforeStateIntentCommit flowDurableFailpointStage = "before-state-intent-commit"
	flowFailpointAfterStateIntentCommit  flowDurableFailpointStage = "after-state-intent-commit"
	flowFailpointBeforeEffectDelivery    flowDurableFailpointStage = "before-effect-delivery"
	flowFailpointAfterEffectDelivery     flowDurableFailpointStage = "after-effect-delivery"
	flowFailpointBeforeAcknowledgeCommit flowDurableFailpointStage = "before-acknowledge-commit"
	flowFailpointAfterAcknowledgeCommit  flowDurableFailpointStage = "after-acknowledge-commit"
)

type flowDurableFailpointEvent struct {
	Stage           flowDurableFailpointStage
	Flow            string
	ExecutionID     string
	HistorySequence int
	EffectID        string
	EffectName      string
}

type flowDurableFailpoint func(flowDurableFailpointEvent) error

type flowDurableFailpointContextKey struct{}

// withDurableFlowFailpoint is an internal deterministic crash-boundary seam.
// It is intentionally unexported: production callers cannot inject failpoints,
// and tests scope a hook to exactly one Dispatch or DrainEffects context rather
// than mutating package-global state.
func withDurableFlowFailpoint(ctx context.Context, failpoint flowDurableFailpoint) context.Context {
	if failpoint == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, flowDurableFailpointContextKey{}, failpoint)
}

func hitDurableFlowFailpoint(ctx context.Context, event flowDurableFailpointEvent) error {
	if ctx == nil {
		return nil
	}
	failpoint, _ := ctx.Value(flowDurableFailpointContextKey{}).(flowDurableFailpoint)
	if failpoint == nil {
		return nil
	}
	return failpoint(event)
}

func (e *FlowExecution[S]) hitDurableFlowFailpoint(
	ctx context.Context,
	stage flowDurableFailpointStage,
	historySequence int,
	intent *FlowEffectIntent,
) error {
	event := flowDurableFailpointEvent{
		Stage:           stage,
		Flow:            e.engine.flow.name,
		ExecutionID:     e.id,
		HistorySequence: historySequence,
	}
	if intent != nil {
		event.EffectID = intent.ID
		event.EffectName = intent.Name
	}
	return hitDurableFlowFailpoint(ctx, event)
}
