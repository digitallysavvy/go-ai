package ai

import (
	"context"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// objectTelemetryStep carries the step and language-model-call OTel contexts
// for a single generateObject/streamObject provider call. TS's
// generateObject/streamObject fire telemetryDispatcher.onObjectStepStart /
// onObjectStepEnd around the model call (generate-object.ts /
// stream-object.ts), which LegacyOpenTelemetry/OpenTelemetry turn into a
// nested "chat"/doGenerate span; before this (H3 item 1) GenerateObject fired
// only start/end telemetry (no step/language-model-call spans at all) and
// StreamObject fired no telemetry whatsoever. Go models this with the same
// step + language-model-call event pair generateText/streamText already use,
// rather than TS's own single-span-per-step shape, since LegacyOpenTelemetry
// already reproduces the TS attribute shape for object operations via
// OperationType dispatch (see registry.go's isLegacyObjectOperation).
type objectTelemetryStep struct {
	stepCtx      context.Context
	modelCallCtx context.Context
	callID       string
	start        time.Time
}

// fireObjectStepStart fires step-start and language-model-call-start
// telemetry immediately before a generateObject/streamObject provider call.
// The returned modelCallCtx is what the provider call should run inside, so
// provider-level HTTP spans nest under the "chat" span when one integration
// creates it (mirrors generateText/streamText's FireOnLanguageModelCallStart
// usage).
func fireObjectStepStart(
	ctx context.Context,
	operationType, callID string,
	model provider.LanguageModel,
	genOpts *provider.GenerateOptions,
	settings *telemetry.Options,
) objectTelemetryStep {
	stepCtx := telemetry.FireOnStepStart(ctx, telemetry.TelemetryStepStartEvent{
		CallID:         callID,
		OperationType:  operationType,
		Settings:       settings,
		StepNumber:     0,
		ModelProvider:  model.Provider(),
		ModelID:        model.ModelID(),
		PromptMessages: genOpts.Prompt.Messages,
	})
	modelCallCtx := telemetry.FireOnLanguageModelCallStart(stepCtx, telemetry.LanguageModelCallStartEvent{
		Settings:         settings,
		CallID:           callID,
		ModelProvider:    model.Provider(),
		ModelID:          model.ModelID(),
		Prompt:           genOpts.Prompt,
		System:           genOpts.Prompt.System,
		Temperature:      genOpts.Temperature,
		MaxOutputTokens:  genOpts.MaxTokens,
		TopP:             genOpts.TopP,
		TopK:             genOpts.TopK,
		PresencePenalty:  genOpts.PresencePenalty,
		FrequencyPenalty: genOpts.FrequencyPenalty,
		Seed:             genOpts.Seed,
	})
	return objectTelemetryStep{stepCtx: stepCtx, modelCallCtx: modelCallCtx, callID: callID, start: time.Now()}
}

// fireObjectLanguageModelCallEnd fires the language-model-call-end event
// after a provider call returns, ending the "chat" span. Shared by both the
// non-streaming (doGenerate) and streaming (doStream, once the terminal
// chunk arrives) object code paths.
func fireObjectLanguageModelCallEnd(
	step objectTelemetryStep,
	model provider.LanguageModel,
	settings *telemetry.Options,
	finishReason types.FinishReason,
	usage types.Usage,
	content []types.ContentPart,
	responseID string,
	providerMetadata map[string]interface{},
) {
	perf := stepPerformance(step.start, usage, nil, nil)
	telemetry.FireOnLanguageModelCallEnd(step.modelCallCtx, telemetry.LanguageModelCallEndEvent{
		Settings:         settings,
		CallID:           step.callID,
		ModelProvider:    model.Provider(),
		ModelID:          model.ModelID(),
		FinishReason:     string(finishReason),
		Usage:            telemetryUsageFromUsage(usage),
		Content:          content,
		ResponseID:       responseID,
		ProviderMetadata: providerMetadata,
		Performance:      languageModelCallPerformance(perf),
	})
}

// fireObjectStepError closes the step span (and, for the GenAI integration,
// the nested "chat" span) opened by fireObjectStepStart when the provider
// call itself failed, so neither fireObjectStepEnd nor
// fireObjectLanguageModelCallEnd will ever run for this step (H4 item 2:
// "span leak on provider error"). Pass a non-nil err to record an error
// status on the closed span; nil just closes it (abort).
func fireObjectStepError(step objectTelemetryStep, settings *telemetry.Options, err error) {
	telemetry.FireOnStepError(step.modelCallCtx, telemetry.TelemetryErrorEvent{
		Settings: settings,
		CallID:   step.callID,
		Error:    err,
	})
}

// fireObjectStepEnd fires the step-end event, ending the step span created
// by fireObjectStepStart. objectText is the raw (possibly partial, for a
// streaming error path) JSON text of the model's response, used by
// LegacyOpenTelemetry's isLegacyObjectOperation branch to build
// ai.response.object. firstChunkAt is the time the first stream chunk was
// received (zero value for non-streaming generateObject, which — like TS's
// `msToFirstChunk: undefined` in generate-object.ts — never reports a
// first-chunk time); when set, it becomes
// Performance.TimeToFirstOutputMs, matching TS stream-object.ts's
// `msToFirstChunk = now() - startTimestampMs`.
func fireObjectStepEnd(
	step objectTelemetryStep,
	operationType string,
	settings *telemetry.Options,
	finishReason types.FinishReason,
	usage types.Usage,
	objectText string,
	responseID, responseModelID string,
	responseTimestamp time.Time,
	providerMetadata map[string]interface{},
	firstChunkAt time.Time,
) {
	var perf telemetry.LanguageModelCallPerformance
	if !firstChunkAt.IsZero() {
		ms := firstChunkAt.Sub(step.start).Milliseconds()
		perf.TimeToFirstOutputMs = &ms
	}
	telemetry.FireOnStepEnd(step.stepCtx, telemetry.TelemetryStepEndEvent{
		CallID:            step.callID,
		OperationType:     operationType,
		StepNumber:        0,
		FinishReason:      string(finishReason),
		Usage:             telemetryUsageFromUsage(usage),
		Text:              objectText,
		ProviderMetadata:  providerMetadata,
		ResponseID:        responseID,
		ResponseModelID:   responseModelID,
		ResponseTimestamp: responseTimestamp,
		Performance:       perf,
		Settings:          settings,
	})
}
