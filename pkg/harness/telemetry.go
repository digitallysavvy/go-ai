package harness

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// This file wires turnDriver (run_prompt.go) into pkg/telemetry's dispatch
// pattern — core (here) fires lifecycle events, and each registered
// telemetry.TelemetryIntegration (e.g. telemetry.OpenTelemetry) owns turning
// them into spans/attributes. pkg/harness never imports OpenTelemetry
// directly, mirroring how pkg/ai/generate.go and stream.go call
// telemetry.Fire* instead of touching go.opentelemetry.io/otel themselves.
//
// Mirrors TS `agent/internal/turn-telemetry.ts`'s createTurnLifecycle:
//   - telStart          -> lifecycle.start
//   - telStepStart      -> lifecycle.ensureStepOpen (stepStart + modelStart)
//   - telLanguageModelCallEnd -> lifecycle.languageModelCallEnd
//   - telToolExecution  -> lifecycle.toolExecutionStart + toolExecutionEnd
//     (fired back-to-back once a tool's outcome is known — TS's
//     publishToolExecutions does the same at each step boundary/pause)
//   - telStepEnd        -> lifecycle.stepEnd (TS b7aa06a: includes StepNumber)
//   - telEnd            -> lifecycle.end (TS a9a22e1: reports the *final*
//     step's text/reasoning, not a fresh accumulation, for multi-step turns)
//   - telError          -> lifecycle.error
//
// Span *nesting* (turn -> step -> model-call, and turn/step -> tool) is
// achieved purely by ctx threading: d.telCtx (from telStart) parents every
// telStepStart call, and d.telStepCtx (from telStepStart, itself already
// nested under d.telCtx) parents every model-call/tool-execution event for
// that step. telemetry.OpenTelemetry embeds each span it creates back into
// the ctx it returns (or into a CallID-keyed lookup for the model-call
// span, which is why FireOnLanguageModelCallEnd is called with the *step*
// ctx rather than a separate model-call ctx — see pkg/ai/generate.go's
// identical pattern, which this mirrors exactly).

// promptAsMessages converts a harness Prompt into the single-message slice
// TS's turn-telemetry `messages` field uses
// (`input.prompt == null ? [] : [{role:'user', content: promptToText(...)}]`).
// A structured Prompt.Message is used as-is; a plain-text Prompt becomes one
// user message; an empty Prompt (e.g. a "continue" turn, which carries no
// fresh prompt) yields no messages.
func promptAsMessages(p Prompt) []types.Message {
	if p.Message != nil {
		return []types.Message{*p.Message}
	}
	if p.Text != "" {
		return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: p.Text}}}}
	}
	return nil
}

// toolSpecsAsTypesTools projects harness ToolSpecs into the []types.Tool
// shape telemetry.LanguageModelCallStartEvent.Tools/gen_ai.tool.definitions
// expects (name/description only — the wire-format InputSchema isn't part
// of that projection).
func toolSpecsAsTypesTools(specs []ToolSpec) []types.Tool {
	if len(specs) == 0 {
		return nil
	}
	out := make([]types.Tool, len(specs))
	for i, s := range specs {
		out[i] = types.Tool{Name: s.Name, Description: s.Description}
	}
	return out
}

// telemetryUsageFromTypesUsage converts the harness's types.Usage
// representation (see harnessUsageToTypesUsageValue) into
// telemetry.TelemetryUsage.
func telemetryUsageFromTypesUsage(u types.Usage) telemetry.TelemetryUsage {
	out := telemetry.TelemetryUsage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		TotalTokens:  u.TotalTokens,
	}
	if u.InputDetails != nil {
		out.CacheReadInputTokens = u.InputDetails.CacheReadTokens
		out.CacheCreationInputTokens = u.InputDetails.CacheWriteTokens
		out.NoCacheInputTokens = u.InputDetails.NoCacheTokens
	}
	if u.OutputDetails != nil {
		out.ReasoningTokens = u.OutputDetails.ReasoningTokens
		out.OutputTextTokens = u.OutputDetails.TextTokens
	}
	return out
}

// filterIncludedContext returns the subset of contextValue's top-level keys
// listed (and true) in include. Duplicated from pkg/ai's identically-named
// unexported helper (telemetry_context.go) rather than imported — it isn't
// exported, and pkg/harness's RuntimeContext/ToolsContext values are always
// plain map[string]interface{} (agent.AgentGenerateOptions.RuntimeContext/
// ToolsContext), so the simpler map-only form is sufficient here.
func filterIncludedContext(contextValue interface{}, include map[string]bool) map[string]interface{} {
	if len(include) == 0 || contextValue == nil {
		return nil
	}
	source, ok := contextValue.(map[string]interface{})
	if !ok || len(source) == 0 {
		return nil
	}
	out := make(map[string]interface{})
	for key, enabled := range include {
		if !enabled {
			continue
		}
		if value, ok := source[key]; ok {
			out[key] = value
		}
	}
	return out
}

func telemetryRuntimeContext(settings *telemetry.Options, contextValue interface{}) map[string]interface{} {
	if settings == nil {
		return nil
	}
	return filterIncludedContext(contextValue, settings.IncludeRuntimeContext)
}

func telemetryToolsContext(settings *telemetry.Options, toolsContext map[string]interface{}) map[string]interface{} {
	if settings == nil || len(settings.IncludeToolsContext) == 0 || len(toolsContext) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(toolsContext))
	for toolName, ctxValue := range toolsContext {
		if filtered := filterIncludedContext(ctxValue, settings.IncludeToolsContext[toolName]); len(filtered) > 0 {
			out[toolName] = filtered
		}
	}
	return out
}

func telemetryToolContext(settings *telemetry.Options, toolName string, toolContext interface{}) map[string]interface{} {
	if settings == nil {
		return nil
	}
	return filterIncludedContext(toolContext, settings.IncludeToolsContext[toolName])
}

// telStart fires the turn-level ("ai.harness") telemetry start event once
// per turn and records the returned root ctx for span nesting. Called from
// ensureStarted, which already guards against firing more than once.
func (d *turnDriver) telStart() {
	d.telCtx = telemetry.FireOnStart(d.ctx, telemetry.TelemetryStartEvent{
		CallID:         d.telCallID,
		OperationType:  "ai.harness",
		Settings:       d.in.Telemetry,
		ModelProvider:  "harness:" + d.in.Harness.HarnessID(),
		ModelID:        d.in.Model,
		System:         d.in.Instructions,
		Messages:       promptAsMessages(d.in.Prompt),
		RuntimeContext: telemetryRuntimeContext(d.in.Telemetry, d.in.RuntimeContext),
		ToolsContext:   telemetryToolsContext(d.in.Telemetry, d.in.ToolsContext),
	})
}

// telStepStart fires the step-start and (immediately, as this step's single
// "model call") language-model-call-start telemetry events, mirroring TS
// `ensureStepOpen`. Called from ensureStepOpen, which already guards against
// firing more than once per step.
func (d *turnDriver) telStepStart() {
	provider := "harness:" + d.in.Harness.HarnessID()
	d.telStepCtx = telemetry.FireOnStepStart(d.telCtx, telemetry.TelemetryStepStartEvent{
		CallID:         d.telCallID,
		OperationType:  "ai.harness",
		Settings:       d.in.Telemetry,
		StepNumber:     d.stepNumber,
		ModelProvider:  provider,
		ModelID:        d.in.Model,
		RuntimeContext: telemetryRuntimeContext(d.in.Telemetry, d.in.RuntimeContext),
		ToolsContext:   telemetryToolsContext(d.in.Telemetry, d.in.ToolsContext),
	})
	d.telModelCallStartedAt = nowMs()
	// The returned ctx (which would nest further under the new "chat" span)
	// is intentionally discarded, not assigned back to d.telStepCtx: mirrors
	// pkg/ai/generate.go, which captures OnLanguageModelCallStart's return
	// in its own separate `modelCallCtx` variable (used only to scope the
	// actual provider HTTP call, which harness has none of) and keeps
	// `stepCtx` itself pointing at the step span for every later step-scoped
	// event — FireOnLanguageModelCallEnd, tool execution/telemetry — exactly
	// as d.telStepCtx must here. Overwriting d.telStepCtx would incorrectly
	// nest tool-execution spans under the (by-then-ended) chat span instead
	// of the step span.
	_ = telemetry.FireOnLanguageModelCallStart(d.telStepCtx, telemetry.LanguageModelCallStartEvent{
		Settings:      d.in.Telemetry,
		CallID:        d.telCallID,
		ModelProvider: provider,
		ModelID:       d.in.Model,
		System:        d.in.Instructions,
		Prompt:        types.Prompt{Messages: promptAsMessages(d.in.Prompt)},
		Tools:         toolSpecsAsTypesTools(d.in.ToolSpecs),
	})
}

// telStepContent builds the []types.ContentPart the in-progress step has
// accumulated so far, for telLanguageModelCallEnd's Content field. Mirrors
// TS `buildModelCallContent` (text, reasoning, then every tool call/
// approval-request/provider-tool-result part collected this step — the
// approval-request/provider-tool-result parts aren't tracked separately by
// turnDriver's own step accumulation, so this covers text/reasoning/tool
// calls, the parts every harness turn actually produces).
func (d *turnDriver) telStepContent() []types.ContentPart {
	var content []types.ContentPart
	if d.stepText != "" {
		content = append(content, types.TextContent{Text: d.stepText})
	}
	if d.stepReasoning != "" {
		content = append(content, types.ReasoningContent{Text: d.stepReasoning})
	}
	for _, tc := range d.stepToolCalls {
		content = append(content, types.ToolCallContent{
			ToolCallID: tc.ID, ToolName: tc.ToolName, Input: tc.RawArguments, Arguments: tc.Arguments,
		})
	}
	return content
}

// telLanguageModelCallEnd fires the language-model-call-end telemetry event
// for the step about to close, ending the "chat" span telStepStart opened.
// Called from completeStep, before the step's tool-result chunks are
// flushed — mirrors TS `completeStep`'s `lifecycle.languageModelCallEnd`
// call, which likewise precedes `publishToolExecutions`.
func (d *turnDriver) telLanguageModelCallEnd(finishReason FinishReason, usage Usage) {
	if d.telStepCtx == nil {
		return
	}
	responseTimeMs := int64(0)
	if d.telModelCallStartedAt > 0 {
		if elapsed := nowMs() - d.telModelCallStartedAt; elapsed > 0 {
			responseTimeMs = elapsed
		}
	}
	telemetry.FireOnLanguageModelCallEnd(d.telStepCtx, telemetry.LanguageModelCallEndEvent{
		Settings:      d.in.Telemetry,
		CallID:        d.telCallID,
		ModelProvider: "harness:" + d.in.Harness.HarnessID(),
		ModelID:       d.in.Model,
		FinishReason:  string(unifiedFinishReason(finishReason)),
		Usage:         telemetryUsageFromTypesUsage(harnessUsageToTypesUsageValue(usage)),
		Content:       d.telStepContent(),
		Performance:   telemetry.LanguageModelCallPerformance{ResponseTimeMs: responseTimeMs},
	})
}

// telToolExecution fires a toolExecutionStart+toolExecutionEnd telemetry
// event pair for one completed tool call, mirroring TS's
// `publishToolExecutions` (which likewise fires both back-to-back once a
// tool's outcome — success or error — is already known, rather than
// bracketing the execution live). Called from recordToolResult, for every
// tool result regardless of who executed it (host tool, harness builtin, or
// provider-executed), matching TS's `toolExecutions` map, which is
// populated the same way. The actual host-tool Execute call is separately
// wrapped for nested-span chaining by executeHostToolAsync via
// telemetry.FireExecuteToolWithSettings (TS 59a2306's `wrappedExecuteTool`).
func (d *turnDriver) telToolExecution(call types.ToolCall, result types.ToolResult, durationMs int64) {
	if d.telStepCtx == nil {
		return
	}
	toolCtx := telemetry.FireOnToolCallStart(d.telStepCtx, telemetry.TelemetryToolCallStartEvent{
		Settings:    d.in.Telemetry,
		CallID:      d.telCallID,
		ToolCallID:  call.ID,
		ToolName:    call.ToolName,
		Args:        call.Arguments,
		ToolContext: telemetryToolContext(d.in.Telemetry, call.ToolName, d.in.ToolsContext[call.ToolName]),
	})
	telemetry.FireOnToolCallFinish(toolCtx, telemetry.TelemetryToolCallFinishEvent{
		Settings:    d.in.Telemetry,
		CallID:      d.telCallID,
		ToolCallID:  call.ID,
		ToolName:    call.ToolName,
		Args:        call.Arguments,
		Result:      result.Result,
		Error:       result.Error,
		DurationMs:  durationMs,
		ToolContext: telemetryToolContext(d.in.Telemetry, call.ToolName, d.in.ToolsContext[call.ToolName]),
	})
}

// telStepEnd fires the step-end telemetry event for a just-closed step,
// ending the step span telStepStart opened. Mirrors TS `lifecycle.stepEnd`
// (TS b7aa06a: StepNumber is included so multi-step turns don't collapse to
// one span in a trace view). stepNumber must be the number of the step being
// closed — pass it explicitly rather than reading d.stepNumber, since
// completeStep's caller increments that field only after this fires.
func (d *turnDriver) telStepEnd(stepNumber int, step types.StepResult) {
	if d.telStepCtx == nil {
		return
	}
	telemetry.FireOnStepEnd(d.telStepCtx, telemetry.TelemetryStepEndEvent{
		Settings:       d.in.Telemetry,
		CallID:         d.telCallID,
		StepNumber:     stepNumber,
		FinishReason:   string(step.FinishReason),
		Usage:          telemetryUsageFromTypesUsage(step.Usage),
		Text:           step.Text,
		Reasoning:      step.ReasoningText,
		ToolCalls:      step.ToolCalls,
		RuntimeContext: telemetryRuntimeContext(d.in.Telemetry, d.in.RuntimeContext),
		ToolsContext:   telemetryToolsContext(d.in.Telemetry, d.in.ToolsContext),
	})
	d.telStepCtx = nil
}

// telEnd fires the turn-end telemetry event, ending the root span telStart
// opened. Mirrors TS `lifecycle.end`: a no-op if the turn never started or
// already ended (telError already fired), or if no step ever completed (TS:
// `steps.length === 0`) — and reports the *final* completed step's
// text/reasoning (TS a9a22e1), not a turn-wide re-accumulation, since a
// multi-step turn's earlier steps' text isn't part of "the answer" the same
// way the final one is.
func (d *turnDriver) telEnd(steps []types.StepResult, totalUsage types.Usage) {
	if d.telCtx == nil || d.telEnded || len(steps) == 0 {
		return
	}
	d.telEnded = true
	finalStep := steps[len(steps)-1]
	telemetry.FireOnEnd(d.telCtx, telemetry.TelemetryFinishEvent{
		CallID:         d.telCallID,
		FinishReason:   string(finalStep.FinishReason),
		Usage:          telemetryUsageFromTypesUsage(totalUsage),
		ModelProvider:  "harness:" + d.in.Harness.HarnessID(),
		ModelID:        d.in.Model,
		Text:           finalStep.Text,
		Files:          finalStep.Files,
		Settings:       d.in.Telemetry,
		RuntimeContext: telemetryRuntimeContext(d.in.Telemetry, d.in.RuntimeContext),
		ToolsContext:   telemetryToolsContext(d.in.Telemetry, d.in.ToolsContext),
	})
}

// telError fires the turn-error telemetry event and marks the turn ended
// (so a later telEnd, if any code path still calls one, is a no-op).
// Mirrors TS `lifecycle.error`, which likewise starts the turn first if it
// never got the chance to (e.g. DoPromptTurn itself failed before any
// content existed), so the root span this reports the error on always
// exists.
func (d *turnDriver) telError(err error) {
	if d.telEnded {
		return
	}
	d.ensureStarted()
	d.telEnded = true
	// H5: CallID is now required so each registered integration can resolve
	// ITS OWN root span by CallID instead of trace.SpanFromContext(ctx) —
	// pkg/ai/generate.go's matching FireOnError call was updated the same way.
	telemetry.FireOnError(d.telCtx, telemetry.TelemetryErrorEvent{Settings: d.in.Telemetry, CallID: d.telCallID, Error: err})
}

// telAbort fires the turn-abort telemetry event instead of telError's
// turn-error one, for a caller-initiated stop (ctx cancelled or timed out).
// Mirrors pkg/ai/generate.go's own abort-vs-error telemetry split
// (telemetry.FireOnAbort vs FireOnError): telemetry.OpenTelemetry's OnAbort
// ends the span cleanly, without recording an error status, matching how
// the stream itself settles through an `abort` chunk (turnDriver.abort)
// rather than an `error` one. Guarded by d.telEnded like telError.
func (d *turnDriver) telAbort(err error) {
	if d.telEnded {
		return
	}
	d.ensureStarted()
	d.telEnded = true
	telemetry.FireOnAbort(d.telCtx, telemetry.TelemetryAbortEvent{
		Settings: d.in.Telemetry, CallID: d.telCallID, Reason: err, Steps: d.completedSteps,
	})
}
