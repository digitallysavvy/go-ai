package telemetry

import "context"

// Deprecated pre-rename entry points, kept so callers of the exported
// "evaluate" telemetry API keep compiling (TS #21935 keeps the deprecated
// experimental_onEvaluate* aliases).

// FireOnEvaluateStart is the pre-rename form of FireOnDecideStart.
//
// Deprecated: Use FireOnDecideStart instead.
func FireOnEvaluateStart(ctx context.Context, e EvaluateStartEvent) context.Context {
	return FireOnDecideStart(ctx, e)
}

// FireOnEvaluateEnd is the pre-rename form of FireOnDecideEnd.
//
// Deprecated: Use FireOnDecideEnd instead.
func FireOnEvaluateEnd(ctx context.Context, e EvaluateEndEvent) {
	FireOnDecideEnd(ctx, e)
}

// FireOnEvaluationModelCallStart is the pre-rename form of
// FireOnDecisionModelCallStart.
//
// Deprecated: Use FireOnDecisionModelCallStart instead.
func FireOnEvaluationModelCallStart(ctx context.Context, e EvaluationModelCallStartEvent) {
	FireOnDecisionModelCallStart(ctx, e)
}

// FireOnEvaluationModelCallEnd is the pre-rename form of
// FireOnDecisionModelCallEnd.
//
// Deprecated: Use FireOnDecisionModelCallEnd instead.
func FireOnEvaluationModelCallEnd(ctx context.Context, e EvaluationModelCallEndEvent) {
	FireOnDecisionModelCallEnd(ctx, e)
}

// OnEvaluateStart is the pre-rename form of OnDecideStart.
//
// Deprecated: Use OnDecideStart instead.
func (i LegacyOpenTelemetry) OnEvaluateStart(ctx context.Context, e EvaluateStartEvent) context.Context {
	return i.OnDecideStart(ctx, e)
}

// OnEvaluateEnd is the pre-rename form of OnDecideEnd.
//
// Deprecated: Use OnDecideEnd instead.
func (i LegacyOpenTelemetry) OnEvaluateEnd(ctx context.Context, e EvaluateEndEvent) {
	i.OnDecideEnd(ctx, e)
}

// OnEvaluationModelCallStart is the pre-rename form of OnDecisionModelCallStart.
//
// Deprecated: Use OnDecisionModelCallStart instead.
func (i LegacyOpenTelemetry) OnEvaluationModelCallStart(ctx context.Context, e EvaluationModelCallStartEvent) {
	i.OnDecisionModelCallStart(ctx, e)
}

// OnEvaluationModelCallEnd is the pre-rename form of OnDecisionModelCallEnd.
//
// Deprecated: Use OnDecisionModelCallEnd instead.
func (i LegacyOpenTelemetry) OnEvaluationModelCallEnd(ctx context.Context, e EvaluationModelCallEndEvent) {
	i.OnDecisionModelCallEnd(ctx, e)
}

// OnEvaluateStart is the pre-rename form of OnDecideStart.
//
// Deprecated: Use OnDecideStart instead.
func (i OpenTelemetry) OnEvaluateStart(ctx context.Context, e EvaluateStartEvent) context.Context {
	return i.OnDecideStart(ctx, e)
}

// OnEvaluateEnd is the pre-rename form of OnDecideEnd.
//
// Deprecated: Use OnDecideEnd instead.
func (i OpenTelemetry) OnEvaluateEnd(ctx context.Context, e EvaluateEndEvent) {
	i.OnDecideEnd(ctx, e)
}

// OnEvaluationModelCallStart is the pre-rename form of OnDecisionModelCallStart.
//
// Deprecated: Use OnDecisionModelCallStart instead.
func (i OpenTelemetry) OnEvaluationModelCallStart(ctx context.Context, e EvaluationModelCallStartEvent) {
	i.OnDecisionModelCallStart(ctx, e)
}

// OnEvaluationModelCallEnd is the pre-rename form of OnDecisionModelCallEnd.
//
// Deprecated: Use OnDecisionModelCallEnd instead.
func (i OpenTelemetry) OnEvaluationModelCallEnd(ctx context.Context, e EvaluationModelCallEndEvent) {
	i.OnDecisionModelCallEnd(ctx, e)
}
