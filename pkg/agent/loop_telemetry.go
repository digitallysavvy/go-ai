package agent

import (
	"context"
	"errors"
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// The legacy AgentResult loop (GenerateAgent, Execute, ExecuteWithMessages)
// calls the provider directly rather than delegating to ai.GenerateText, so it
// dispatches its own step, model-call and tool-call telemetry to the
// registered integrations (TS #22480 dispatches the same events from
// WorkflowAgent's model step). Without it a bare GenerateAgent emitted no
// spans at all.

type telemetryCallIDKey struct{}

type loopTelemetryKey struct{}

// WithTelemetryCallID marks ctx as already carrying a telemetry root span
// (opened by the caller with FireOnStart under callID). The legacy loop then
// dispatches its step, model-call and tool-call events under that call ID
// instead of opening a second root span. WorkflowAgent uses this so its
// 'ai.workflowAgent.generate' root is the parent of the loop's step spans.
func WithTelemetryCallID(ctx context.Context, callID string) context.Context {
	return context.WithValue(ctx, telemetryCallIDKey{}, callID)
}

// newTelemetryCallID returns "call-" plus 24 random alphanumerics, the TS
// callId shape (createIdGenerator({prefix: 'call', size: 24})).
func newTelemetryCallID() string {
	gen, err := ai.CreateIDGenerator(ai.CreateIDGeneratorOptions{Prefix: "call", Size: 24})
	if err != nil {
		return "call-" + ai.GenerateID()
	}
	return gen()
}

// loopTelemetry carries the per-run telemetry state of the legacy loop.
type loopTelemetry struct {
	settings *ai.TelemetrySettings
	callID   string
	// ownsRoot is true when the loop opened the root span itself.
	ownsRoot bool
	// rootCtx is the context carrying the root span when the loop opened it
	// late, for a PrepareCall telemetry override (see adoptPreparedTelemetry).
	rootCtx context.Context
}

// adoptPreparedTelemetry applies a PrepareCall telemetry override (TS
// prepareCall `telemetry`). Later spans use the new settings. When the caller
// already opened a root span under the previous settings, the new settings
// have none, so the loop opens one for them and closes it at the end.
func (a *ToolLoopAgent) adoptPreparedTelemetry(ctx context.Context, lt *loopTelemetry, settings *ai.TelemetrySettings, messages []types.Message) context.Context {
	if lt == nil || settings == nil || settings == lt.settings {
		return ctx
	}
	lt.settings = settings
	if lt.ownsRoot {
		return ctx
	}
	lt.ownsRoot = true
	ctx = telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
		CallID:         lt.callID,
		OperationType:  "ai.generateText",
		ModelProvider:  a.config.Model.Provider(),
		ModelID:        a.config.Model.ModelID(),
		Settings:       settings,
		Messages:       messages,
		RuntimeContext: telemetryRuntimeContextFor(settings, a.runtimeContext()),
		ToolsContext:   telemetryToolsContextFor(settings, a.config.ToolsContext),
	})
	lt.rootCtx = ctx
	return ctx
}

func (a *ToolLoopAgent) loopTelemetrySettings() *ai.TelemetrySettings {
	if a.config.Telemetry != nil {
		return a.config.Telemetry
	}
	return a.config.ExperimentalTelemetry //nolint:staticcheck // deprecated alias, Telemetry wins
}

func telemetryContextMap(include map[string]bool, v interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	if len(include) == 0 || v == nil {
		return out
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer && !rv.IsNil() {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return out
	}
	for key, enabled := range include {
		if !enabled {
			continue
		}
		if val := rv.MapIndex(reflect.ValueOf(key).Convert(rv.Type().Key())); val.IsValid() {
			out[key] = val.Interface()
		}
	}
	return out
}

func telemetryRuntimeContextFor(s *ai.TelemetrySettings, rc interface{}) map[string]interface{} {
	if s == nil {
		return map[string]interface{}{}
	}
	return telemetryContextMap(s.IncludeRuntimeContext, rc)
}

func telemetryToolsContextFor(s *ai.TelemetrySettings, tc map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	if s == nil || len(s.IncludeToolsContext) == 0 {
		return out
	}
	for name, v := range tc {
		// TS filterToolsContext keeps one entry per tool, empty when nothing
		// is included for it.
		out[name] = telemetryContextMap(s.IncludeToolsContext[name], v)
	}
	return out
}

func telemetryUsage(u types.Usage) telemetry.TelemetryUsage {
	out := telemetry.TelemetryUsage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, TotalTokens: u.TotalTokens}
	if u.InputDetails != nil {
		out.NoCacheInputTokens = u.InputDetails.NoCacheTokens
		out.CacheReadInputTokens = u.InputDetails.CacheReadTokens
		out.CacheCreationInputTokens = u.InputDetails.CacheWriteTokens
	}
	if u.OutputDetails != nil {
		out.OutputTextTokens = u.OutputDetails.TextTokens
		out.ReasoningTokens = u.OutputDetails.ReasoningTokens
	}
	return out
}

func isAbort(ctx context.Context, err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || (ctx != nil && ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled)))
}

// beginLoopTelemetry opens the root span unless the caller already did.
func (a *ToolLoopAgent) beginLoopTelemetry(ctx context.Context, messages []types.Message) (context.Context, *loopTelemetry) {
	settings := a.loopTelemetrySettings()
	if id, ok := ctx.Value(telemetryCallIDKey{}).(string); ok && id != "" {
		return ctx, &loopTelemetry{settings: settings, callID: id}
	}
	lt := &loopTelemetry{settings: settings, callID: newTelemetryCallID(), ownsRoot: true}
	var sys string
	if settings != nil && settings.RecordInputs {
		sys = a.config.System
	}
	maxRetries := 2
	if a.config.MaxRetries != nil {
		maxRetries = *a.config.MaxRetries
	}
	ctx = telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
		CallID:        lt.callID,
		OperationType: "ai.generateText",
		ModelProvider: a.config.Model.Provider(),
		ModelID:       a.config.Model.ModelID(),
		Settings:      settings,
		System:        sys,
		Messages:      messages,
		Headers:       a.config.Headers,
		// Call-level request settings and the retry count, as GenerateText
		// reports them on its start event (TS onGenerateStart).
		MaxOutputTokens:  a.config.MaxTokens,
		Temperature:      a.config.Temperature,
		TopP:             a.config.TopP,
		TopK:             a.config.TopK,
		PresencePenalty:  a.config.PresencePenalty,
		FrequencyPenalty: a.config.FrequencyPenalty,
		StopSequences:    a.config.StopSequences,
		Seed:             a.config.Seed,
		MaxRetries:       &maxRetries,
		RuntimeContext:   telemetryRuntimeContextFor(settings, a.runtimeContext()),
		ToolsContext:     telemetryToolsContextFor(settings, a.config.ToolsContext),
	})
	return ctx, lt
}

// endLoopTelemetry closes the root span opened by beginLoopTelemetry.
func (a *ToolLoopAgent) endLoopTelemetry(ctx context.Context, lt *loopTelemetry, result *AgentResult, err error) {
	if lt == nil || !lt.ownsRoot {
		return
	}
	if lt.rootCtx != nil {
		ctx = lt.rootCtx
	}
	if err != nil {
		if isAbort(ctx, err) {
			var steps []types.StepResult
			if result != nil {
				steps = result.Steps
			}
			telemetry.FireOnAbort(ctx, telemetry.TelemetryAbortEvent{Settings: lt.settings, CallID: lt.callID, Reason: err, Steps: steps})
		} else {
			telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: lt.settings, CallID: lt.callID, Error: err})
		}
		return
	}
	if result == nil {
		return
	}
	var last types.StepResult
	if n := len(result.Steps); n > 0 {
		last = result.Steps[n-1]
	}
	// TS onEndEvent.toolCalls/toolResults span every step.
	var allToolCalls []types.ToolCall
	var allToolResults []types.ToolResult
	for _, step := range result.Steps {
		allToolCalls = append(allToolCalls, step.ToolCalls...)
		allToolResults = append(allToolResults, step.ToolResults...)
	}
	telemetry.FireOnEnd(ctx, telemetry.TelemetryFinishEvent{
		CallID:           lt.callID,
		OperationType:    "ai.generateText",
		FinishReason:     string(result.FinishReason),
		Usage:            telemetryUsage(result.Usage),
		ModelProvider:    a.config.Model.Provider(),
		ModelID:          a.config.Model.ModelID(),
		Text:             result.Text,
		Reasoning:        telemetry.JoinReasoning(last.Reasoning),
		ToolCalls:        allToolCalls,
		ToolResults:      allToolResults,
		ReasoningParts:   last.Reasoning,
		ProviderMetadata: last.ProviderMetadata,
		Settings:         lt.settings,
		RuntimeContext:   telemetryRuntimeContextFor(lt.settings, a.runtimeContext()),
		ToolsContext:     telemetryToolsContextFor(lt.settings, a.config.ToolsContext),
	})
}

// loopStepTelemetry tracks the open step (and model-call) span of one step.
type loopStepTelemetry struct {
	lt      *loopTelemetry
	stepCtx context.Context
	// errCtx is the context failures are reported under: the model-call ctx
	// while a provider call is in flight (so the nested "chat" span closes
	// too), the step ctx otherwise.
	errCtx context.Context
	closed bool
}

func (a *ToolLoopAgent) startStepTelemetry(ctx context.Context, lt *loopTelemetry, stepIndex int, c PrepareCallConfig) *loopStepTelemetry {
	stepCtx := telemetry.FireOnStepStart(ctx, telemetry.TelemetryStepStartEvent{
		CallID:         lt.callID,
		OperationType:  "ai.generateText",
		Settings:       lt.settings,
		StepNumber:     stepIndex,
		ModelProvider:  c.Model.Provider(),
		ModelID:        c.Model.ModelID(),
		ToolChoice:     c.ToolChoice,
		PromptMessages: c.Messages,
		StepTools:      c.Tools,
		RuntimeContext: telemetryRuntimeContextFor(lt.settings, c.RuntimeContext),
		ToolsContext:   telemetryToolsContextFor(lt.settings, c.ToolsContext),
	})
	return &loopStepTelemetry{lt: lt, stepCtx: stepCtx, errCtx: stepCtx}
}

// fail closes the step span after an error (no error status for an abort).
func (s *loopStepTelemetry) fail(err error) {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	if isAbort(s.stepCtx, err) {
		err = nil
	}
	telemetry.FireOnStepError(s.errCtx, telemetry.TelemetryErrorEvent{Settings: s.lt.settings, CallID: s.lt.callID, Error: err})
}

func (s *loopStepTelemetry) finish(step *types.StepResult, rc interface{}, tc map[string]interface{}) {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	telemetry.FireOnStepEnd(s.stepCtx, telemetry.TelemetryStepEndEvent{
		CallID:           s.lt.callID,
		OperationType:    "ai.generateText",
		StepNumber:       step.StepNumber,
		FinishReason:     string(step.FinishReason),
		Usage:            telemetryUsage(step.Usage),
		Text:             step.Text,
		Reasoning:        telemetry.JoinReasoning(step.Reasoning),
		ToolCalls:        step.ToolCalls,
		ToolResults:      step.ToolResults,
		ProviderMetadata: step.ProviderMetadata,
		Settings:         s.lt.settings,
		RuntimeContext:   telemetryRuntimeContextFor(s.lt.settings, rc),
		ToolsContext:     telemetryToolsContextFor(s.lt.settings, tc),
	})
}

func (s *loopStepTelemetry) modelCallStart(c PrepareCallConfig, model interface {
	Provider() string
	ModelID() string
}, prompt types.Prompt, genTools []types.Tool) context.Context {
	if s == nil {
		return nil
	}
	ctx := telemetry.FireOnLanguageModelCallStart(s.stepCtx, telemetry.LanguageModelCallStartEvent{
		Settings:         s.lt.settings,
		CallID:           s.lt.callID,
		ModelProvider:    model.Provider(),
		ModelID:          model.ModelID(),
		Prompt:           prompt,
		Tools:            genTools,
		System:           c.System,
		Temperature:      c.Temperature,
		MaxOutputTokens:  c.MaxTokens,
		TopP:             c.TopP,
		TopK:             c.TopK,
		PresencePenalty:  c.PresencePenalty,
		FrequencyPenalty: c.FrequencyPenalty,
		StopSequences:    c.StopSequences,
		Seed:             c.Seed,
	})
	s.errCtx = ctx
	return ctx
}

func (s *loopStepTelemetry) modelCallEnd(model interface {
	Provider() string
	ModelID() string
}, res *types.GenerateResult, content []types.ContentPart, response types.StepResponse, elapsedMs int64) {
	if s == nil {
		return
	}
	telemetry.FireOnLanguageModelCallEnd(s.stepCtx, telemetry.LanguageModelCallEndEvent{
		Settings:      s.lt.settings,
		CallID:        s.lt.callID,
		ModelProvider: model.Provider(),
		// The response model (TS event.modelId), not the request model.
		ModelID:          response.ModelID,
		FinishReason:     string(res.FinishReason),
		Usage:            telemetryUsage(res.Usage),
		Content:          content,
		ResponseID:       response.ID,
		ProviderMetadata: res.ProviderMetadata,
		Performance:      ai.NonStreamingModelCallPerformance(elapsedMs, res.Usage),
	})
	s.errCtx = s.stepCtx
}

// callID is the telemetry call id the step's events are dispatched under.
func (s *loopStepTelemetry) callID() string {
	if s == nil || s.lt == nil {
		return ""
	}
	return s.lt.callID
}

func telemetryToolContextFor(s *ai.TelemetrySettings, toolName string, v interface{}) map[string]interface{} {
	if s == nil {
		return map[string]interface{}{}
	}
	return telemetryContextMap(s.IncludeToolsContext[toolName], v)
}
