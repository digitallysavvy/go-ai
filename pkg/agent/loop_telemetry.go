package agent

import (
	"context"
	"reflect"
	"time"

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
// 'ai.workflowAgent.stream' root is the parent of the loop's step spans.
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
// (nil means the globally registered ones; an empty, non-nil list means none).
func sameIntegrations(a, b *ai.TelemetrySettings) bool {
	var as, bs []telemetry.TelemetryIntegration
	if a != nil {
		as = a.Integrations
	}
	if b != nil {
		bs = b.Integrations
	}
	if (as == nil) != (bs == nil) || len(as) != len(bs) {
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
func (a *ToolLoopAgent) adoptPreparedTelemetry(ctx context.Context, lt *loopTelemetry, settings *ai.TelemetrySettings, c PrepareCallConfig) context.Context {
	if lt == nil || settings == nil || settings == lt.settings {
		return ctx
	}
	lt.settings = settings
	if lt.ownsRoot {
		if !telemetry.Enabled(lt.rootSettings) {
			if telemetry.Enabled(settings) {
				lt.rootSettings = settings
				ctx = telemetry.FireOnStart(ctx, a.loopStartEvent(lt.callID, settings, c))
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
		base := lt.baseCtx
		if base == nil {
			base = ctx
		}
		for i, r := range lt.otherRoots {
			if sameIntegrations(r.settings, settings) {
				lt.otherRoots[i] = current
				lt.rootSettings, lt.callID, lt.rootCtx = r.settings, r.callID, r.ctx
				return switchedRootCtx{Context: ctx, root: r.ctx, base: base}
			}
		}
		lt.otherRoots = append(lt.otherRoots, current)
		lt.rootSettings = settings
		lt.callID = newTelemetryCallID()
		root := telemetry.FireOnStart(base, a.loopStartEvent(lt.callID, settings, c))
		lt.rootCtx = root
		return switchedRootCtx{Context: ctx, root: root, base: base}
	}
	// The caller's root keeps its call id (and the call state integrations
	// keep under it) for its own end event; the late root is a call of its own.
	lt.ownsRoot = true
	lt.rootSettings = settings
	lt.callID = newTelemetryCallID()
	ctx = telemetry.FireOnStart(ctx, a.loopStartEvent(lt.callID, settings, c))
	lt.rootCtx = ctx
	return ctx
}

// switchedRootCtx is the loop's context after a PrepareCall override switched
// it to another root span. The root was opened from the loop's base context,
// so the roots are siblings, but the steps after the switch must keep the
// loop's own context: its deadline and cancellation (the total timeout), and
// the values the loop added after the base (run id, telemetry state). Values
// the root's integrations added or changed relative to the base (their span)
// come from the root; every other value comes from the loop's context.
type switchedRootCtx struct {
	context.Context
	root, base context.Context
}

func (c switchedRootCtx) Value(key any) any {
	if v := c.root.Value(key); !sameContextValue(v, c.base.Value(key)) {
		return v
	}
	return c.Context.Value(key)
}

func sameContextValue(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return sameValue(reflect.ValueOf(a), reflect.ValueOf(b))
}

// loopStartEvent is the root start event of the legacy loop: call-level
// request settings and the retry count, as GenerateText reports them on its
// start event (TS onGenerateStart). c is the call's prepared configuration
// (TS ToolLoopAgent applies prepareCall before generateText opens its root),
// so the root reports the PrepareCall model, instructions, settings and
// headers. Used for the loop's own root span and for one opened late by a
// PrepareCall telemetry override.
func (a *ToolLoopAgent) loopStartEvent(callID string, settings *ai.TelemetrySettings, c PrepareCallConfig) telemetry.TelemetryStartEvent {
	model := c.Model
	if model == nil {
		model = a.config.Model
	}
	var sys string
	var instructionMessages []types.Message
	// A nil *Options records inputs (integrations treat it the same way).
	if settings.ShouldRecordInputs() {
		sys = c.System
		// Instructions given as system messages (TS instructions:
		// SystemModelMessage[]) are the root's instructions, as GenerateText
		// reports them on its start event.
		if len(c.InstructionMessages) > 0 {
			instructionMessages = append([]types.Message(nil), c.InstructionMessages...)
		}
	}
	maxRetries := 2
	if c.MaxRetries != nil {
		maxRetries = *c.MaxRetries
	}
	return telemetry.TelemetryStartEvent{
		CallID:              callID,
		OperationType:       "ai.generateText",
		ModelProvider:       model.Provider(),
		ModelID:             model.ModelID(),
		Settings:            settings,
		System:              sys,
		Messages:            c.Messages,
		InstructionMessages: instructionMessages,
		Headers:             loopHeaders(c.Headers),
		MaxOutputTokens:     c.MaxTokens,
		Temperature:         c.Temperature,
		TopP:                c.TopP,
		TopK:                c.TopK,
		PresencePenalty:     c.PresencePenalty,
		FrequencyPenalty:    c.FrequencyPenalty,
		StopSequences:       c.StopSequences,
		Seed:                c.Seed,
		MaxRetries:          &maxRetries,
		RuntimeContext:      telemetryRuntimeContextFor(settings, c.RuntimeContext),
		ToolsContext:        telemetryToolsContextFor(settings, c.ToolsContext),
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

// beginLoopTelemetry opens the root span unless the caller already did. c is
// the first step's prepared configuration: its telemetry settings (when
// PrepareCall set them) are the root's.
func (a *ToolLoopAgent) beginLoopTelemetry(ctx context.Context, c PrepareCallConfig) (context.Context, *loopTelemetry) {
	settings := a.loopTelemetrySettings()
	if id, ok := ctx.Value(telemetryCallIDKey{}).(string); ok && id != "" {
		// The caller's root was opened under the configured settings; a
		// PrepareCall override opens a root of its own (adoptPreparedTelemetry).
		return ctx, &loopTelemetry{settings: settings, callID: id}
	}
	if c.Telemetry != nil {
		settings = c.Telemetry
	}
	lt := &loopTelemetry{settings: settings, rootSettings: settings, callID: newTelemetryCallID(), ownsRoot: true, baseCtx: ctx}
	ctx = telemetry.FireOnStart(ctx, a.loopStartEvent(lt.callID, settings, c))
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
		// The loop runs as generateText does in TS, which has no abort
		// telemetry: an abort or timeout is reported through onError.
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: root.settings, CallID: root.callID, Error: err})
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
	// TS onEndEvent: model and runtimeContext are the last step's, and
	// toolsContext is the call's latest (a prepareStep override included).
	provider, modelID := a.config.Model.Provider(), a.config.Model.ModelID()
	runtimeContext, toolsContext := a.runtimeContext(), a.config.ToolsContext
	if len(result.Steps) > 0 {
		if last.Model.Provider != "" || last.Model.ModelID != "" {
			provider, modelID = last.Model.Provider, last.Model.ModelID
		}
		runtimeContext, toolsContext = last.RuntimeContext, last.ToolsContext
	}
	telemetry.FireOnEnd(ctx, telemetry.TelemetryFinishEvent{
		CallID:         root.callID,
		OperationType:  "ai.generateText",
		FinishReason:   string(result.FinishReason),
		Usage:          telemetryUsage(result.Usage),
		ModelProvider:  provider,
		ModelID:        modelID,
		Text:           result.Text,
		Reasoning:      telemetry.JoinReasoning(last.Reasoning),
		ToolCalls:      allToolCalls,
		ToolResults:    allToolResults,
		ReasoningParts: last.Reasoning,
		ReasoningFiles: loopReasoningFiles(last.Content),
		// TS onEndEvent.files spans every step.
		Files:            allFiles,
		ProviderMetadata: last.ProviderMetadata,
		Settings:         root.settings,
		RuntimeContext:   telemetryRuntimeContextFor(root.settings, runtimeContext),
		ToolsContext:     telemetryToolsContextFor(root.settings, toolsContext),
	})
}

// loopReasoningFiles returns the reasoning file parts of a step's content
// (TS StepResult.reasoning holds them next to the reasoning parts).
func loopReasoningFiles(content []types.ContentPart) []types.ReasoningFileContent {
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

// loopStepTelemetry tracks the open step (and model-call) span of one step.
type loopStepTelemetry struct {
	lt      *loopTelemetry
	stepCtx context.Context
	// errCtx is the context failures are reported under: the model-call ctx
	// while a provider call is in flight (so the nested "chat" span closes
	// too), the step ctx otherwise.
	errCtx context.Context
	closed bool
	// startedAt is the step's TS stepStartTimestampMs: taken just before the
	// language model call start, it times both the model call and the step.
	startedAt time.Time
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
		PromptMessages: loopPromptMessages(c),
		StepTools:      c.Tools,
		RuntimeContext: telemetryRuntimeContextFor(lt.settings, c.RuntimeContext),
		ToolsContext:   telemetryToolsContextFor(lt.settings, c.ToolsContext),
	})
	return &loopStepTelemetry{lt: lt, stepCtx: stepCtx, errCtx: stepCtx}
}

// loopPromptMessages is the step's prompt messages for the step span (TS
// promptMessages): instructions given as system messages come first, as the
// step's prompt places them (see executeStep).
func loopPromptMessages(c PrepareCallConfig) []types.Message {
	if len(c.InstructionMessages) == 0 {
		return c.Messages
	}
	out := make([]types.Message, 0, len(c.InstructionMessages)+len(c.Messages))
	for _, m := range c.InstructionMessages {
		out = append(out, types.Message{Role: types.RoleSystem, Content: m.Content, ProviderOptions: m.ProviderOptions})
	}
	return append(out, c.Messages...)
}

// fail closes the step span after an error, an abort included (TS
// generateText reports aborts through onStepError).
func (s *loopStepTelemetry) fail(err error) {
	if s == nil || s.closed {
		return
	}
	s.closed = true
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
		ReasoningParts:   step.Reasoning,
		ReasoningFiles:   loopReasoningFiles(step.Content),
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
		Settings:      s.lt.settings,
		CallID:        s.lt.callID,
		ModelProvider: model.Provider(),
		ModelID:       model.ModelID(),
		Prompt:        prompt,
		Messages:      c.Messages,
		Tools:         genTools,
		System:        c.System,
		// TS languageModelCallStart instructions: the step's instruction
		// messages when they were given as messages.
		InstructionMessages: c.InstructionMessages,
		Temperature:         c.Temperature,
		MaxOutputTokens:     c.MaxTokens,
		TopP:                c.TopP,
		TopK:                c.TopK,
		PresencePenalty:     c.PresencePenalty,
		FrequencyPenalty:    c.FrequencyPenalty,
		StopSequences:       c.StopSequences,
		Seed:                c.Seed,
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
