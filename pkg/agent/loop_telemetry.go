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

// newTextResponseID is the default step response id of TS generateText
// (generateId: createIdGenerator({prefix: 'aitxt', size: 24})), which the TS
// ToolLoopAgent runs on.
func newTextResponseID() string {
	gen, err := ai.CreateIDGenerator(ai.CreateIDGeneratorOptions{Prefix: "aitxt", Size: 24})
	if err != nil {
		return "aitxt-" + ai.GenerateID()
	}
	return gen()
}

// loopTelemetry carries the per-run telemetry state of the legacy loop.
type loopTelemetry struct {
	settings *ai.TelemetrySettings
	// rootSettings are the settings the loop's own root span was started
	// with. Its end, error and abort events use them, so a PrepareCall
	// override that disables telemetry or changes its integrations cannot
	// leave that root open.
	rootSettings *ai.TelemetrySettings
	callID       string
	// ownsRoot is true when the loop opened the root span itself.
	ownsRoot bool
	// rootCtx is the context carrying the root span when the loop opened it
	// late, for a PrepareCall telemetry override (see adoptPreparedTelemetry).
	rootCtx context.Context
	// baseCtx is the context the loop's own root was opened from; a root
	// opened for a different integration set starts from it too, so the
	// roots are siblings rather than nested.
	baseCtx context.Context
	// otherRoots are roots the loop opened for an earlier integration set
	// that a PrepareCall override switched away from. Each is ended with its
	// own settings and call id.
	otherRoots []loopRoot
}

// loopRoot is a root span the legacy loop opened: the settings it was
// started with, its call id and the context carrying it.
type loopRoot struct {
	settings *ai.TelemetrySettings
	callID   string
	ctx      context.Context
}

// sameIntegrations reports whether a and b dispatch to the same integrations
// (an empty list means the globally registered ones).
func sameIntegrations(a, b *ai.TelemetrySettings) bool {
	var as, bs []telemetry.TelemetryIntegration
	if a != nil {
		as = a.Integrations
	}
	if b != nil {
		bs = b.Integrations
	}
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if !sameIntegration(as[i], bs[i]) {
			return false
		}
	}
	return true
}

// sameIntegration reports whether a and b are the same integration. The
// SDK's integrations are struct values with func fields, which == cannot
// compare, so values of one type are compared field by field, with
// reference-like fields (pointers, maps, funcs, slices, channels) compared by
// identity: two copies of one constructed integration are the same, two
// constructions are not (each holds its own state pointer).
func sameIntegration(a, b telemetry.TelemetryIntegration) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return sameValue(reflect.ValueOf(a), reflect.ValueOf(b))
}

func sameValue(a, b reflect.Value) bool {
	if a.Type() != b.Type() {
		return false
	}
	switch a.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return a.Pointer() == b.Pointer()
	case reflect.Slice:
		return a.Pointer() == b.Pointer() && a.Len() == b.Len()
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() && b.IsNil()
		}
		return sameValue(a.Elem(), b.Elem())
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if !sameValue(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Array:
		for i := 0; i < a.Len(); i++ {
			if !sameValue(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	default:
		return a.Equal(b)
	}
}

// adoptPreparedTelemetry applies a PrepareCall telemetry override (TS
// prepareCall `telemetry`). Later spans use the new settings. When the caller
// already opened a root span under the previous settings, the new settings
// have none, so the loop opens one for them, under a new call id, and closes
// it at the end. When the loop's own root was not opened because telemetry
// was disabled, it is opened now under the new settings. When the override
// routes the steps to a different integration set than the loop's root, that
// set gets a root of its own (see below); every root is ended at the end.
func (a *ToolLoopAgent) adoptPreparedTelemetry(ctx context.Context, lt *loopTelemetry, settings *ai.TelemetrySettings, messages []types.Message) context.Context {
	if lt == nil || settings == nil || settings == lt.settings {
		return ctx
	}
	lt.settings = settings
	if lt.ownsRoot {
		if !telemetry.Enabled(lt.rootSettings) {
			if telemetry.Enabled(settings) {
				lt.rootSettings = settings
				ctx = telemetry.FireOnStart(ctx, a.loopStartEvent(lt.callID, settings, messages))
				lt.rootCtx = ctx
			}
			return ctx
		}
		if !telemetry.Enabled(settings) || sameIntegrations(lt.rootSettings, settings) {
			return ctx
		}
		// The override routes the steps to a different integration set. TS
		// applies prepareCall before generateText opens its root, so the
		// integrations that receive the steps always have a root for the
		// call: switch back to a root already open for this set, or open one
		// under a call id of its own. The previous root stays open until the
		// loop ends.
		current := loopRoot{settings: lt.rootSettings, callID: lt.callID, ctx: lt.rootCtx}
		if current.ctx == nil {
			current.ctx = lt.baseCtx
		}
		for i, r := range lt.otherRoots {
			if sameIntegrations(r.settings, settings) {
				lt.otherRoots[i] = current
				lt.rootSettings, lt.callID, lt.rootCtx = r.settings, r.callID, r.ctx
				return r.ctx
			}
		}
		lt.otherRoots = append(lt.otherRoots, current)
		lt.rootSettings = settings
		lt.callID = newTelemetryCallID()
		base := lt.baseCtx
		if base == nil {
			base = ctx
		}
		ctx = telemetry.FireOnStart(base, a.loopStartEvent(lt.callID, settings, messages))
		lt.rootCtx = ctx
		return ctx
	}
	// The caller's root keeps its call id (and the call state integrations
	// keep under it) for its own end event; the late root is a call of its own.
	lt.ownsRoot = true
	lt.rootSettings = settings
	lt.callID = newTelemetryCallID()
	ctx = telemetry.FireOnStart(ctx, a.loopStartEvent(lt.callID, settings, messages))
	lt.rootCtx = ctx
	return ctx
}

// loopStartEvent is the root start event of the legacy loop: call-level
// request settings and the retry count, as GenerateText reports them on its
// start event (TS onGenerateStart). Used for the loop's own root span and for
// one opened late by a PrepareCall telemetry override.
func (a *ToolLoopAgent) loopStartEvent(callID string, settings *ai.TelemetrySettings, messages []types.Message) telemetry.TelemetryStartEvent {
	var sys string
	// A nil *Options records inputs (integrations treat it the same way).
	if settings.ShouldRecordInputs() {
		sys = a.config.System
	}
	maxRetries := 2
	if a.config.MaxRetries != nil {
		maxRetries = *a.config.MaxRetries
	}
	return telemetry.TelemetryStartEvent{
		CallID:           callID,
		OperationType:    "ai.generateText",
		ModelProvider:    a.config.Model.Provider(),
		ModelID:          a.config.Model.ModelID(),
		Settings:         settings,
		System:           sys,
		Messages:         messages,
		Headers:          loopHeaders(a.config.Headers),
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
	}
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

// isAbort reports whether err ends the run as an abort: a cancellation or a
// timeout, as GenerateText classifies them (TS timeouts are abort signals).
func isAbort(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return ctx != nil && ctx.Err() != nil
}

// beginLoopTelemetry opens the root span unless the caller already did.
func (a *ToolLoopAgent) beginLoopTelemetry(ctx context.Context, messages []types.Message) (context.Context, *loopTelemetry) {
	settings := a.loopTelemetrySettings()
	if id, ok := ctx.Value(telemetryCallIDKey{}).(string); ok && id != "" {
		return ctx, &loopTelemetry{settings: settings, callID: id}
	}
	lt := &loopTelemetry{settings: settings, rootSettings: settings, callID: newTelemetryCallID(), ownsRoot: true, baseCtx: ctx}
	ctx = telemetry.FireOnStart(ctx, a.loopStartEvent(lt.callID, settings, messages))
	if telemetry.Enabled(settings) {
		lt.rootCtx = ctx
	}
	return ctx, lt
}

// endLoopTelemetry closes the root span opened by beginLoopTelemetry.
func (a *ToolLoopAgent) endLoopTelemetry(ctx context.Context, lt *loopTelemetry, result *AgentResult, err error) {
	if lt == nil || !lt.ownsRoot {
		return
	}
	rootCtx := ctx
	if lt.rootCtx != nil {
		rootCtx = lt.rootCtx
	}
	a.endLoopRoot(rootCtx, loopRoot{settings: lt.rootSettings, callID: lt.callID}, result, err)
	for i := len(lt.otherRoots) - 1; i >= 0; i-- {
		r := lt.otherRoots[i]
		if r.ctx == nil {
			r.ctx = ctx
		}
		a.endLoopRoot(r.ctx, r, result, err)
	}
}

// endLoopRoot ends one root span the loop opened with its own settings and
// call id.
func (a *ToolLoopAgent) endLoopRoot(ctx context.Context, root loopRoot, result *AgentResult, err error) {
	if err != nil {
		if isAbort(ctx, err) {
			var steps []types.StepResult
			if result != nil {
				steps = result.Steps
			}
			telemetry.FireOnAbort(ctx, telemetry.TelemetryAbortEvent{Settings: root.settings, CallID: root.callID, Reason: err, Steps: steps})
		} else {
			telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: root.settings, CallID: root.callID, Error: err})
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
	var allFiles []types.GeneratedFileContent
	for _, step := range result.Steps {
		allToolCalls = append(allToolCalls, step.ToolCalls...)
		allToolResults = append(allToolResults, step.ToolResults...)
		allFiles = append(allFiles, step.Files...)
	}
	telemetry.FireOnEnd(ctx, telemetry.TelemetryFinishEvent{
		CallID:         root.callID,
		OperationType:  "ai.generateText",
		FinishReason:   string(result.FinishReason),
		Usage:          telemetryUsage(result.Usage),
		ModelProvider:  a.config.Model.Provider(),
		ModelID:        a.config.Model.ModelID(),
		Text:           result.Text,
		Reasoning:      telemetry.JoinReasoning(last.Reasoning),
		ToolCalls:      allToolCalls,
		ToolResults:    allToolResults,
		ReasoningParts: last.Reasoning,
		// TS onEndEvent.files spans every step.
		Files:            allFiles,
		ProviderMetadata: last.ProviderMetadata,
		Settings:         root.settings,
		RuntimeContext:   telemetryRuntimeContextFor(root.settings, a.runtimeContext()),
		ToolsContext:     telemetryToolsContextFor(root.settings, a.config.ToolsContext),
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
		Files:            step.Files,
		ProviderMetadata: step.ProviderMetadata,
		// TS onStepEnd records event.response.id/modelId/timestamp.
		ResponseID:        step.Response.ID,
		ResponseModelID:   step.Response.ModelID,
		ResponseTimestamp: step.Response.Timestamp,
		Settings:          s.lt.settings,
		RuntimeContext:    telemetryRuntimeContextFor(s.lt.settings, rc),
		ToolsContext:      telemetryToolsContextFor(s.lt.settings, tc),
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
