package harness

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/ai"
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
//   - ensureToolExecutionStarted (run_prompt.go) -> lifecycle.toolExecutionStart
//     (fired as soon as a tool's execution actually begins — TS #21696's
//     `publishToolExecutionStart`)
//   - telToolExecutionEnd -> lifecycle.toolExecutionEnd (fired once a
//     tool's outcome is known, closing the span toolExecutionStart opened —
//     TS #21696's `publishToolExecutionEnd`)
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

// promptText is the best-effort plain text of a turn's prompt (TS
// `promptToText`): the string itself, or the concatenated text parts of a
// structured user message.
func promptText(p Prompt) string {
	if p.Message == nil {
		return p.Text
	}
	var text string
	for _, part := range p.Message.Content {
		switch c := part.(type) {
		case types.TextContent:
			text += c.Text
		case *types.TextContent:
			if c != nil {
				text += c.Text
			}
		}
	}
	return text
}

// promptMessages is the turn's `messages` field in lifecycle callbacks and
// telemetry (TS `input.prompt == null ? [] : [{role:'user', content:
// promptToText(input.prompt)}]`): one user message with the prompt's plain
// text, or an empty list (not nil: telemetry records `[]`) for a
// continuation, which carries no fresh prompt.
func (d *turnDriver) promptMessages() []types.Message {
	if d.in.Mode == "continue" {
		return []types.Message{}
	}
	return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: promptText(d.in.Prompt)}}}}
}

// toolSpecsAsTypesTools projects harness ToolSpecs into the []types.Tool
// shape telemetry.LanguageModelCallStartEvent.Tools/gen_ai.tool.definitions
// expects: TS `languageModelTools` carries name, description and inputSchema
// (each only when set).
func toolSpecsAsTypesTools(specs []ToolSpec) []types.Tool {
	if len(specs) == 0 {
		return nil
	}
	out := make([]types.Tool, len(specs))
	for i, s := range specs {
		out[i] = types.Tool{Name: s.Name, Description: s.Description, Parameters: s.InputSchema}
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
		return map[string]interface{}{}
	}
	source, ok := contextValue.(map[string]interface{})
	if !ok || len(source) == 0 {
		return map[string]interface{}{}
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
		return map[string]interface{}{}
	}
	return filterIncludedContext(contextValue, settings.IncludeRuntimeContext)
}

func telemetryToolsContext(settings *telemetry.Options, toolsContext map[string]interface{}) map[string]interface{} {
	if settings == nil || len(settings.IncludeToolsContext) == 0 || len(toolsContext) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(toolsContext))
	for toolName, ctxValue := range toolsContext {
		// TS filterToolsContext keeps one entry per tool, empty when nothing
		// is included for it.
		out[toolName] = filterIncludedContext(ctxValue, settings.IncludeToolsContext[toolName])
	}
	return out
}

// telStart fires the turn-level ("ai.harness") telemetry start event once
// per turn and records the returned root ctx for span nesting. Called from
// ensureStarted, which already guards against firing more than once.
func (d *turnDriver) telStart() {
	// TS createTurnLifecycle: `telemetry == null` -> no dispatcher at all, so
	// an agent without Telemetry settings fires nothing, even when an
	// integration is registered globally.
	if d.in.Telemetry == nil {
		return
	}
	// TS start event: `maxRetries: 0` (a harness turn never retries).
	maxRetries := 0
	d.telCtx = telemetry.FireOnStart(d.ctx, telemetry.TelemetryStartEvent{
		MaxRetries:     &maxRetries,
		CallID:         d.telCallID,
		OperationType:  "ai.harness",
		Settings:       d.in.Telemetry,
		ModelProvider:  "harness:" + d.in.Harness.HarnessID(),
		ModelID:        d.modelID,
		System:         d.in.Instructions,
		Messages:       d.promptMessages(),
		RuntimeContext: telemetryRuntimeContext(d.in.Telemetry, d.in.RuntimeContext),
		ToolsContext:   telemetryToolsContext(d.in.Telemetry, d.in.ToolsContext),
	})
}

// telStepStart fires the step-start and (immediately, as this step's single
// "model call") language-model-call-start telemetry events, mirroring TS
// `ensureStepOpen`. Called from ensureStepOpen, which already guards against
// firing more than once per step.
func (d *turnDriver) telStepStart() {
	d.telModelCallStartedAt = nowMs()
	if d.in.Telemetry == nil {
		return
	}
	provider := "harness:" + d.in.Harness.HarnessID()
	d.telStepCtx = telemetry.FireOnStepStart(d.telCtx, telemetry.TelemetryStepStartEvent{
		CallID:         d.telCallID,
		OperationType:  "ai.harness",
		Settings:       d.in.Telemetry,
		StepNumber:     d.stepNumber,
		ModelProvider:  provider,
		ModelID:        d.modelID,
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
		ModelID:       d.modelID,
		System:        d.in.Instructions,
		Prompt:        types.Prompt{Messages: d.promptMessages()},
		Messages:      d.promptMessages(),
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
			ProviderExecuted: tc.ProviderExecuted,
		})
	}
	for _, ar := range d.stepApprovalRequests {
		content = append(content, ar)
	}
	// TS buildModelCallContent also carries the provider-executed tool
	// results of the step. Host tool results are not part of the model call:
	// TS ends the model call before publishing them.
	for _, tr := range d.stepProviderToolResults {
		part := types.ToolResultContent{ToolCallID: tr.ToolCallID, ToolName: tr.ToolName, Input: tr.Input, Result: tr.Result}
		if tr.Error != nil {
			part.Error = tr.Error.Error()
		}
		content = append(content, part)
	}
	return content
}

// languageModelCallEnd fires the language-model-call-end callback and
// telemetry event for the step about to close, ending the "chat" span
// telStepStart opened. Called from completeStep, before the step's
// tool-result chunks are flushed - mirrors TS `completeStep`'s
// `lifecycle.languageModelCallEnd` call, which likewise precedes
// `publishToolExecutions`. The event carries the step's content, the step's
// harness metadata as provider metadata, and TS's `${callId}-${stepNumber}`
// response id.
func (d *turnDriver) languageModelCallEnd(finishReason FinishReason, usage Usage, metadata Metadata) {
	// TS lifecycle.languageModelCallEnd opens the step first, so a step that
	// closes without any content still reports its start events.
	d.ensureStepOpen()
	responseTimeMs := int64(0)
	if d.telModelCallStartedAt > 0 {
		if elapsed := nowMs() - d.telModelCallStartedAt; elapsed > 0 {
			responseTimeMs = elapsed
		}
	}
	provider := "harness:" + d.in.Harness.HarnessID()
	typesUsage := harnessUsageToTypesUsageValue(usage)
	content := d.telStepContent()
	responseID := fmt.Sprintf("%s-%d", d.telCallID, d.stepNumber)
	var providerMetadata map[string]interface{}
	if len(metadata) > 0 {
		providerMetadata = make(map[string]interface{}, len(metadata))
		for k, v := range metadata {
			providerMetadata[k] = v
		}
	}
	if d.in.Callbacks.OnLanguageModelCallEnd != nil {
		d.in.Callbacks.OnLanguageModelCallEnd(d.ctx, ai.LanguageModelCallEndEvent{
			CallID: d.telCallID, Provider: provider, ModelID: d.modelID,
			FinishReason: unifiedFinishReason(finishReason), Usage: typesUsage,
			Content: content, ResponseID: responseID, ProviderMetadata: providerMetadata,
			Performance: ai.LanguageModelCallPerformance{ResponseTimeMs: responseTimeMs},
		})
	}
	if d.telStepCtx == nil {
		return
	}
	telemetry.FireOnLanguageModelCallEnd(d.telStepCtx, telemetry.LanguageModelCallEndEvent{
		Settings:         d.in.Telemetry,
		CallID:           d.telCallID,
		ModelProvider:    provider,
		ModelID:          d.modelID,
		FinishReason:     string(unifiedFinishReason(finishReason)),
		Usage:            telemetryUsageFromTypesUsage(typesUsage),
		Content:          content,
		ResponseID:       responseID,
		ProviderMetadata: providerMetadata,
		Performance:      telemetry.LanguageModelCallPerformance{ResponseTimeMs: responseTimeMs},
	})
}

// telToolExecutionEnd fires the toolExecutionEnd telemetry event for one
// completed tool call, from recordToolResult (the main goroutine), once the
// tool's outcome is known. Its matching toolExecutionStart has already
// fired — either early, from the tool's own exec goroutine or from
// consumeLoop for a provider-executed call (see ensureToolExecutionStarted
// and the telToolCtx field doc, TS #21696's `publishToolExecutionStart`) —
// or, as a fallback here via recordToolResult's own
// ensureToolExecutionStarted call for a tool-result that never got an
// earlier explicit start (e.g. a client-submitted result), matching TS's
// `publishToolExecutionEnd` always calling `publishToolExecutionStart`
// first. Threads the exact ctx that start returned (d.telToolCtx[call.ID])
// so the end event closes the same span, falling back to d.telStepCtx (the
// pre-H5 behavior) if no stored ctx exists — e.g. telemetry was disabled
// when start ran. Mirrors TS's `toolExecutions` map / `publishToolExecutionEnd`.
func (d *turnDriver) telToolExecutionEnd(call types.ToolCall, result types.ToolResult, durationMs int64) {
	d.execMu.Lock()
	toolCtx, started := d.telToolCtx[call.ID]
	if started {
		delete(d.telToolCtx, call.ID)
	}
	d.execMu.Unlock()
	if !started {
		if d.telStepCtx == nil {
			return
		}
		toolCtx = d.telStepCtx
	}
	telemetry.FireOnToolCallFinish(toolCtx, telemetry.TelemetryToolCallFinishEvent{
		Settings:   d.in.Telemetry,
		CallID:     d.telCallID,
		ToolCallID: call.ID,
		ToolName:   call.ToolName,
		Args:       call.Arguments,
		Result:     result.Result,
		Error:      result.Error,
		DurationMs: durationMs,
		// TS publishToolExecutionEnd passes `toolContext: undefined`.
		ToolContext: nil,
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
		Settings:      d.in.Telemetry,
		CallID:        d.telCallID,
		OperationType: "ai.harness",
		StepNumber:    stepNumber,
		// TS stepEnd passes step.response ({callId}-{n}, timestamp, model),
		// which the legacy step span records as ai.response.*.
		ResponseID:        step.Response.ID,
		ResponseModelID:   step.Response.ModelID,
		ResponseTimestamp: step.Response.Timestamp,
		FinishReason:      string(step.FinishReason),
		Usage:             telemetryUsageFromTypesUsage(step.Usage),
		Text:              step.Text,
		// TS legacy ai.response.reasoning joins the parts with "\n".
		Reasoning:        telemetry.JoinReasoning(step.Reasoning),
		ReasoningParts:   step.Reasoning,
		ReasoningFiles:   harnessReasoningFiles(step.Content),
		ToolCalls:        step.ToolCalls,
		ToolResults:      step.ToolResults,
		Files:            step.Files,
		ProviderMetadata: step.ProviderMetadata,
		RuntimeContext:   telemetryRuntimeContext(d.in.Telemetry, d.in.RuntimeContext),
		ToolsContext:     telemetryToolsContext(d.in.Telemetry, d.in.ToolsContext),
	})
	d.telStepCtx = nil
}

// telEnd ends the turn: it fires the Callbacks.OnEnd callback and the
// turn-end telemetry event (ending the root span telStart opened). Mirrors TS
// `lifecycle.end`: a no-op if the turn already ended (telError/telAbort
// already fired) or if no step ever completed (TS: `steps.length === 0`).
// Like TS, the event reports the *final* completed step's text/reasoning
// (TS a9a22e1) rather than a turn-wide re-accumulation, and the tool calls,
// tool results and files of every step.
func (d *turnDriver) telEnd(steps []types.StepResult, totalUsage types.Usage) {
	// TS lifecycle.end starts the lifecycle first, then returns without
	// ending when the turn has no steps.
	d.ensureStarted()
	if d.telEnded || len(steps) == 0 {
		return
	}
	d.telEnded = true
	finalStep := steps[len(steps)-1]
	var toolCalls []types.ToolCall
	var toolResults []types.ToolResult
	var files []types.GeneratedFileContent
	for _, st := range steps {
		toolCalls = append(toolCalls, st.ToolCalls...)
		toolResults = append(toolResults, st.ToolResults...)
		files = append(files, st.Files...)
	}
	if d.in.Callbacks.OnEnd != nil {
		event := ai.OnFinishEvent{
			CallID: d.telCallID, StepNumber: finalStep.StepNumber, Model: finalStep.Model,
			ModelProvider: finalStep.Model.Provider, ModelID: finalStep.Model.ModelID,
			Text: finalStep.Text, Reasoning: finalStep.Reasoning, ReasoningText: finalStep.ReasoningText,
			FinishReason: finalStep.FinishReason, RawFinishReason: finalStep.RawFinishReason,
			Usage: totalUsage, TotalUsage: totalUsage, Steps: steps,
			ProviderMetadata: finalStep.ProviderMetadata,
			ResponseHeaders:  finalStep.Response.Headers,
			Request:          ai.GenerateStepRequest{Body: finalStep.Request.Body},
			Response: ai.GenerateStepResponse{
				ID: finalStep.Response.ID, Timestamp: finalStep.Response.Timestamp, ModelID: finalStep.Response.ModelID,
				Headers: finalStep.Response.Headers, Messages: finalStep.Response.Messages, Body: finalStep.Response.Body,
			},
			RuntimeContext: d.in.RuntimeContext, ToolsContext: d.in.ToolsContext,
		}
		var responseMessages []types.Message
		for _, st := range steps {
			responseMessages = append(responseMessages, st.Response.Messages...)
		}
		event.SetAggregatesFromSteps(steps, responseMessages)
		event.Usage = totalUsage
		d.in.Callbacks.OnEnd(d.ctx, event)
	}
	if d.telCtx == nil {
		return
	}
	telemetry.FireOnEnd(d.telCtx, telemetry.TelemetryFinishEvent{
		CallID:        d.telCallID,
		OperationType: "ai.harness",
		FinishReason:  string(finalStep.FinishReason),
		Usage:         telemetryUsageFromTypesUsage(totalUsage),
		ModelProvider: "harness:" + d.in.Harness.HarnessID(),
		ModelID:       d.modelID,
		Text:          finalStep.Text,
		Reasoning:     telemetry.JoinReasoning(finalStep.Reasoning),
		ToolCalls:     toolCalls,
		// TS passes toolResults of every step and finalStep.reasoning; the
		// GenAI integration formats them into gen_ai.output.messages.
		ToolResults:      toolResults,
		ReasoningParts:   finalStep.Reasoning,
		ReasoningFiles:   harnessReasoningFiles(finalStep.Content),
		Files:            files,
		ProviderMetadata: finalStep.ProviderMetadata,
		Settings:         d.in.Telemetry,
		RuntimeContext:   telemetryRuntimeContext(d.in.Telemetry, d.in.RuntimeContext),
		ToolsContext:     telemetryToolsContext(d.in.Telemetry, d.in.ToolsContext),
	})
}

// telError fires the turn-error telemetry event and marks the turn ended
// (so a later telEnd, if any code path still calls one, is a no-op).
// Mirrors TS `lifecycle.error`, which likewise starts the turn first if it
// never got the chance to (e.g. DoPromptTurn itself failed before any
// content existed), so the root span this reports the error on always
// exists.
func (d *turnDriver) telError(err error) {
	// TS `lifecycle.error` starts the lifecycle (firing the onStart callback)
	// and marks the turn ended whether or not telemetry is configured.
	if d.telEnded {
		return
	}
	d.ensureStarted()
	d.telEnded = true
	if d.in.Telemetry == nil {
		return
	}
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
	if d.in.Telemetry == nil {
		return
	}
	telemetry.FireOnAbort(d.telCtx, telemetry.TelemetryAbortEvent{
		Settings: d.in.Telemetry, CallID: d.telCallID, Reason: err, Steps: d.completedSteps,
	})
}

// harnessReasoningFiles returns the reasoning file parts of a step's content
// (TS StepResult.reasoning holds them next to the reasoning parts).
func harnessReasoningFiles(content []types.ContentPart) []types.ReasoningFileContent {
	var out []types.ReasoningFileContent
	for _, p := range content {
		switch v := p.(type) {
		case types.ReasoningFileContent:
			out = append(out, v)
		case *types.ReasoningFileContent:
			if v != nil {
				out = append(out, *v)
			}
		}
	}
	return out
}
