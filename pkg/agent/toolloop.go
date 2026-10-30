package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/internal/aibridge"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	toolutil "github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/version"
	"github.com/google/uuid"
)

// ========================================================================
// Callback merging (CB-T22)
// ========================================================================

// agentCallbacks groups the structured event callbacks used in a single
// agent execution. It is built by mergeCallbacks combining settings-level
// and per-call callbacks.
type agentCallbacks struct {
	onStart          func(ctx context.Context, e ai.OnStartEvent)
	onStepStart      func(ctx context.Context, e ai.OnStepStartEvent)
	onToolCallStart  func(ctx context.Context, e ai.OnToolCallStartEvent)
	onToolCallFinish func(ctx context.Context, e ai.OnToolCallFinishEvent)
	onStepFinish     func(ctx context.Context, e ai.OnStepFinishEvent)
	onFinish         func(ctx context.Context, e ai.OnFinishEvent)
}

type toolResultModelOutputError struct {
	err error
}

func (e *toolResultModelOutputError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *toolResultModelOutputError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// mergeCallbacks combines settings-level and per-call structured callbacks.
// When both are provided, both fire in order: settings first, then call-level.
// Either (or both) may be nil.
func mergeCallbacks(settings AgentConfig, callOpts agentCallbacks) agentCallbacks {
	settingsToolStart := settings.OnToolExecutionStart
	if settingsToolStart == nil {
		settingsToolStart = settings.OnToolCallStart
	}
	settingsToolFinish := settings.OnToolExecutionEnd
	if settingsToolFinish == nil {
		settingsToolFinish = settings.OnToolCallFinish
	}
	settingsStepEnd := settings.OnStepEndEvent
	if settingsStepEnd == nil {
		settingsStepEnd = settings.OnStepFinishEvent
	}
	settingsEnd := settings.OnEndEvent
	if settingsEnd == nil {
		settingsEnd = settings.OnFinishEvent
	}
	return agentCallbacks{
		onStart:          mergeListener(settings.OnStart, callOpts.onStart),
		onStepStart:      mergeListener(settings.OnStepStartEvent, callOpts.onStepStart),
		onToolCallStart:  mergeListener(settingsToolStart, callOpts.onToolCallStart),
		onToolCallFinish: mergeListener(settingsToolFinish, callOpts.onToolCallFinish),
		onStepFinish:     mergeListener(settingsStepEnd, callOpts.onStepFinish),
		onFinish:         mergeListener(settingsEnd, callOpts.onFinish),
	}
}

// resolveAgentOnEnd returns opts.OnEnd if set, else its deprecated alias
// opts.OnFinish.
func resolveAgentOnEnd(opts AgentGenerateOptions) func(context.Context, ai.OnFinishEvent) {
	if opts.OnEnd != nil {
		return opts.OnEnd
	}
	return opts.OnFinish
}

func resolveAgentOnStepEnd(opts AgentGenerateOptions) func(context.Context, ai.OnStepFinishEvent) {
	if opts.OnStepEnd != nil {
		return opts.OnStepEnd
	}
	return opts.OnStepFinish
}

func resolveAgentBasicOnStepEnd(config AgentConfig) func(types.StepResult) {
	if config.OnStepEnd != nil {
		return config.OnStepEnd
	}
	return config.OnStepFinish
}

func resolveAgentToolExecutionStart(opts AgentGenerateOptions) func(context.Context, ai.OnToolCallStartEvent) {
	if opts.OnToolExecutionStart != nil {
		return opts.OnToolExecutionStart
	}
	return opts.OnToolCallStart
}

func resolveAgentToolExecutionEnd(opts AgentGenerateOptions) func(context.Context, ai.OnToolCallFinishEvent) {
	if opts.OnToolExecutionEnd != nil {
		return opts.OnToolExecutionEnd
	}
	return opts.OnToolCallFinish
}

// mergeListener returns a single listener that calls both a and b in order.
// If either is nil, returns the other. If both are nil, returns nil.
// mergeErrorHandlers runs the config-level handler first, then the per-call one.
func mergeErrorHandlers(a, b func(ctx context.Context, err error)) func(ctx context.Context, err error) {
	return mergeListener(a, b)
}

// mergeAbortHandlers runs the config-level handler first, then the per-call one.
func mergeAbortHandlers(a, b func(ctx context.Context, steps []types.StepResult)) func(ctx context.Context, steps []types.StepResult) {
	return mergeListener(a, b)
}

func mergeListener[E any](a, b func(ctx context.Context, e E)) func(ctx context.Context, e E) {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return func(ctx context.Context, e E) {
		a(ctx, e)
		b(ctx, e)
	}
}

// Context keys for run tracking
type contextKey string

const (
	runIDKey       contextKey = "agent_run_id"
	parentRunIDKey contextKey = "agent_parent_run_id"
	tagsKey        contextKey = "agent_tags"
)

// ToolLoopAgent is an agent that loops through tool calls until task completion
type ToolLoopAgent struct {
	config   AgentConfig
	warnings []types.Warning
}

// NewToolLoopAgent creates a new ToolLoopAgent with the given configuration
func NewToolLoopAgent(config AgentConfig) *ToolLoopAgent {
	var warnings []types.Warning
	if config.System == "" && config.Prompt != "" {
		config.System = config.Prompt
	}
	if config.Instructions != nil {
		config.System = *config.Instructions
	}
	if config.RuntimeContext == nil && config.ExperimentalContext != nil {
		config.RuntimeContext = config.ExperimentalContext
	}
	if config.ExperimentalContext == nil && config.RuntimeContext != nil {
		config.ExperimentalContext = config.RuntimeContext
	}
	// Resolve stop conditions (Vercel AI SDK v5 approach):
	// MaxSteps is sugar for StopWhen{StepCountIs(N)}.
	// All termination flows through stop conditions.
	if len(config.StopWhen) > 0 {
		// StopWhen takes precedence; MaxSteps is not used as a second cap.
		config.MaxSteps = 0
	} else if config.MaxSteps > 0 {
		config.StopWhen = []ai.StopCondition{ai.IsStepCount(config.MaxSteps)}
		warnings = append(warnings, types.Warning{
			Type:    "deprecated-setting",
			Message: fmt.Sprintf("AgentConfig.MaxSteps is deprecated; use StopWhen with ai.IsStepCount(%d) instead.", config.MaxSteps),
		})
		config.MaxSteps = 0
	} else {
		config.StopWhen = []ai.StopCondition{createDefaultStopCondition(20)}
		config.MaxSteps = 0
	}

	// Initialize skills registry if not provided
	if config.Skills == nil {
		config.Skills = NewSkillRegistry()
	}

	// Initialize subagents registry if not provided
	if config.Subagents == nil {
		config.Subagents = NewSubagentRegistry()
	}

	return &ToolLoopAgent{
		config:   config,
		warnings: warnings,
	}
}

// ID returns the agent identifier, matching the TypeScript Agent.id accessor.
func (a *ToolLoopAgent) ID() string {
	return a.config.ID
}

// Version returns the agent version. TypeScript ToolLoopAgent exposes "agent-v1".
func (a *ToolLoopAgent) Version() string {
	if a.config.Version != "" {
		return a.config.Version
	}
	return "agent-v1"
}

// Tools returns a copy of the configured tools, matching the TypeScript tools accessor.
func (a *ToolLoopAgent) Tools() []types.Tool {
	if len(a.config.Tools) == 0 {
		return nil
	}
	tools := make([]types.Tool, len(a.config.Tools))
	copy(tools, a.config.Tools)
	return tools
}

// agentHeaders tags outgoing requests so usage can be attributed to
// ToolLoopAgent. Chains with the "ai/<version>" and
// "ai-sdk-<provider>/<version>" suffixes added downstream by
// GenerateText/StreamText and the provider (TS ToolLoopAgent.agentHeaders).
//
// If the headers already carry an "ai-sdk-agent/*" segment (for example,
// WorkflowAgent tags "ai-sdk-agent/workflow" before delegating to an
// internal ToolLoopAgent), that segment is left as the sole agent
// attribution instead of appending a second, conflicting one.
func agentHeaders(headers map[string]string) map[string]string {
	if hasAgentUserAgentSegment(headers) {
		return headers
	}
	return version.WithUserAgentSuffix(headers, "ai-sdk-agent/tool-loop")
}

// loopHeaders is the header set the legacy loop's model calls send: for a
// direct call, agentHeaders plus the ai/<version> suffix GenerateText adds.
// A caller that already tagged the request with its own agent segment
// (WorkflowAgent's ai-sdk-agent/workflow; TS streamLanguageModelCall adds no
// ai/<version>) keeps its headers as they are.
func loopHeaders(headers map[string]string) map[string]string {
	if hasAgentUserAgentSegment(headers) {
		return headers
	}
	return version.WithUserAgentSuffix(agentHeaders(headers), version.UserAgent())
}

// loopStepHeaders is loopHeaders for a step's model call, unless the caller
// asked for its headers to be sent as given (InternalOptions.HeadersAsGiven).
func loopStepHeaders(internal *ai.InternalOptions, headers map[string]string) map[string]string {
	if internal != nil && internal.HeadersAsGiven {
		return headers
	}
	return loopHeaders(headers)
}

func hasAgentUserAgentSegment(headers map[string]string) bool {
	for k, v := range headers {
		if strings.EqualFold(k, "user-agent") {
			return strings.Contains(v, "ai-sdk-agent/")
		}
	}
	return false
}

// Generate runs the agent with per-call options, mirroring TypeScript
// ToolLoopAgent.generate with an idiomatic Go options struct.
func (a *ToolLoopAgent) Generate(ctx context.Context, opts AgentGenerateOptions) (*ai.GenerateTextResult, error) {
	if err := validateAgentPromptOptions(opts.Prompt, opts.Messages); err != nil {
		return nil, err
	}
	config := a.config.withGenerateOptions(opts)
	if config.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	defaultedCallOptions, err := validateAgentCallOptions(config.CallOptionsSchema, config.CallOptions)
	if err != nil {
		return nil, err
	}
	config.CallOptions = defaultedCallOptions

	messages := opts.Messages
	prompt := opts.Prompt
	callConfig := (&ToolLoopAgent{config: config}).prepareGenerateCallConfig(ctx, prompt, messages, opts.Timeout)
	if len(callConfig.Messages) > 0 {
		messages = callConfig.Messages
		prompt = ""
	} else {
		prompt = callConfig.Prompt
	}
	cbs := mergeCallbacks(config, agentCallbacks{
		onStart:          opts.OnStart,
		onStepStart:      opts.OnStepStart,
		onToolCallStart:  resolveAgentToolExecutionStart(opts),
		onToolCallFinish: resolveAgentToolExecutionEnd(opts),
		onStepFinish:     resolveAgentOnStepEnd(opts),
		onFinish:         resolveAgentOnEnd(opts),
	})
	result, genErr := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:                          callConfig.Model,
		Prompt:                         prompt,
		Messages:                       messages,
		System:                         callConfig.System,
		Instructions:                   &callConfig.System,
		AllowSystemInMessages:          callConfig.AllowSystemInMessages,
		Temperature:                    callConfig.Temperature,
		MaxTokens:                      callConfig.MaxTokens,
		TopP:                           callConfig.TopP,
		TopK:                           callConfig.TopK,
		FrequencyPenalty:               callConfig.FrequencyPenalty,
		PresencePenalty:                callConfig.PresencePenalty,
		StopSequences:                  callConfig.StopSequences,
		Seed:                           callConfig.Seed,
		Headers:                        agentHeaders(callConfig.Headers),
		Tools:                          callConfig.Tools,
		ActiveTools:                    callConfig.ActiveTools,
		ToolChoice:                     callConfig.ToolChoice,
		ToolOrder:                      callConfig.ToolOrder,
		ToolCallConcurrency:            callConfig.ToolCallConcurrency,
		ExperimentalToolCallers:        callConfig.ExperimentalToolCallers,
		ToolApproval:                   callConfig.ToolApproval,
		ExperimentalToolApprovalSecret: callConfig.ExperimentalToolApprovalSecret,
		StopWhen:                       callConfig.StopWhen,
		Timeout:                        callConfig.Timeout,
		MaxRetries:                     callConfig.MaxRetries,
		Reasoning:                      callConfig.Reasoning,
		SendReasoning:                  callConfig.SendReasoning,
		ProviderOptions:                callConfig.ProviderOptions,
		Include:                        callConfig.Include,
		ExperimentalSandbox:            callConfig.ExperimentalSandbox,
		ExperimentalRefineToolInput:    callConfig.ExperimentalRefineToolInput,
		ExperimentalDownload:           callConfig.ExperimentalDownload,
		InstructionMessages:            callConfig.InstructionMessages,
		PrepareStep:                    callConfig.PrepareStep,
		RepairToolCall:                 callConfig.RepairToolCall,
		OnLanguageModelCallStart:       callConfig.OnLanguageModelCallStart,
		OnLanguageModelCallEnd:         callConfig.OnLanguageModelCallEnd,
		RuntimeContext:                 callConfig.RuntimeContext,
		ToolsContext:                   callConfig.ToolsContext,
		SensitiveRuntimeContext:        callConfig.SensitiveRuntimeContext,
		ExperimentalContext:            config.ExperimentalContext,
		Output:                         callConfig.Output,
		Telemetry:                      callConfig.Telemetry,
		ExperimentalTelemetry:          callConfig.ExperimentalTelemetry, //nolint:staticcheck // forwarded for callers still setting the deprecated field; Telemetry (set above) wins when both are present
		Internal:                       callConfig.Internal,
		OnStart:                        cbs.onStart,
		OnStepStart:                    cbs.onStepStart,
		OnStepError:                    config.OnStepError,
		ExperimentalSteering:           opts.ExperimentalSteering,
		OnToolExecutionStart:           cbs.onToolCallStart,
		OnToolExecutionEnd:             cbs.onToolCallFinish,
		OnStepEndEvent:                 cbs.onStepFinish,
		OnEndEvent:                     cbs.onFinish,
	})
	// GenerateText has no error callback, so report the error before it is
	// returned. Aborts and timeouts are not reported, like in Stream.
	if genErr != nil && config.ErrorHandler != nil && ctx.Err() == nil &&
		!errors.Is(genErr, context.Canceled) && !errors.Is(genErr, context.DeadlineExceeded) {
		config.ErrorHandler(ctx, genErr)
	}
	return result, genErr
}

// GenerateAgent preserves the Go-specific AgentResult loop shape. Prefer
// Generate when matching TypeScript ToolLoopAgent.generate.
func (a *ToolLoopAgent) GenerateAgent(ctx context.Context, opts AgentGenerateOptions) (*AgentResult, error) {
	if err := validateAgentPromptOptions(opts.Prompt, opts.Messages); err != nil {
		return nil, err
	}
	callAgent := &ToolLoopAgent{
		config:   a.config.withGenerateOptions(opts),
		warnings: append([]types.Warning{}, a.warnings...),
	}
	messages := opts.Messages
	if len(messages) == 0 && opts.Prompt != "" {
		messages = []types.Message{{
			Role:    types.RoleUser,
			Content: []types.ContentPart{types.TextContent{Text: opts.Prompt}},
		}}
	}
	cbs := agentCallbacks{
		onStart:          opts.OnStart,
		onStepStart:      opts.OnStepStart,
		onToolCallStart:  resolveAgentToolExecutionStart(opts),
		onToolCallFinish: resolveAgentToolExecutionEnd(opts),
		onStepFinish:     resolveAgentOnStepEnd(opts),
		onFinish:         resolveAgentOnEnd(opts),
	}
	return callAgent.executeWithMessages(ctx, messages, cbs)
}

// Stream streams an agent response using the SDK's StreamTextResult. This is
// the Go equivalent of TypeScript ToolLoopAgent.stream.
func (a *ToolLoopAgent) Stream(ctx context.Context, opts AgentStreamOptions) (*ai.StreamTextResult, error) {
	if err := validateAgentPromptOptions(opts.Prompt, opts.Messages); err != nil {
		return nil, err
	}
	config := a.config.withGenerateOptions(opts.AgentGenerateOptions)
	if config.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	defaultedCallOptions, err := validateAgentCallOptions(config.CallOptionsSchema, config.CallOptions)
	if err != nil {
		return nil, err
	}
	config.CallOptions = defaultedCallOptions
	messages := opts.Messages
	prompt := opts.Prompt
	callConfig := (&ToolLoopAgent{config: config}).prepareGenerateCallConfig(ctx, prompt, messages, opts.Timeout)
	if len(callConfig.Messages) > 0 {
		messages = callConfig.Messages
		prompt = ""
	} else {
		prompt = callConfig.Prompt
	}
	cbs := mergeCallbacks(config, agentCallbacks{
		onStart:          opts.OnStart,
		onStepStart:      opts.OnStepStart,
		onToolCallStart:  resolveAgentToolExecutionStart(opts.AgentGenerateOptions),
		onToolCallFinish: resolveAgentToolExecutionEnd(opts.AgentGenerateOptions),
		onStepFinish:     resolveAgentOnStepEnd(opts.AgentGenerateOptions),
		onFinish:         resolveAgentOnEnd(opts.AgentGenerateOptions),
	})
	streamOpts := ai.StreamTextOptions{
		Model:                          callConfig.Model,
		Prompt:                         prompt,
		Messages:                       messages,
		System:                         callConfig.System,
		Instructions:                   &callConfig.System,
		AllowSystemInMessages:          callConfig.AllowSystemInMessages,
		Temperature:                    callConfig.Temperature,
		MaxTokens:                      callConfig.MaxTokens,
		TopP:                           callConfig.TopP,
		TopK:                           callConfig.TopK,
		FrequencyPenalty:               callConfig.FrequencyPenalty,
		PresencePenalty:                callConfig.PresencePenalty,
		StopSequences:                  callConfig.StopSequences,
		Seed:                           callConfig.Seed,
		Headers:                        agentHeaders(callConfig.Headers),
		Tools:                          callConfig.Tools,
		ActiveTools:                    callConfig.ActiveTools,
		ToolChoice:                     callConfig.ToolChoice,
		ToolOrder:                      callConfig.ToolOrder,
		ToolCallConcurrency:            callConfig.ToolCallConcurrency,
		ExperimentalToolCallers:        callConfig.ExperimentalToolCallers,
		ToolApproval:                   callConfig.ToolApproval,
		ExperimentalToolApprovalSecret: callConfig.ExperimentalToolApprovalSecret,
		StopWhen:                       callConfig.StopWhen,
		Timeout:                        callConfig.Timeout,
		MaxRetries:                     callConfig.MaxRetries,
		Reasoning:                      callConfig.Reasoning,
		SendReasoning:                  callConfig.SendReasoning,
		ProviderOptions:                callConfig.ProviderOptions,
		Include:                        callConfig.Include,
		ExperimentalSandbox:            callConfig.ExperimentalSandbox,
		ExperimentalRefineToolInput:    callConfig.ExperimentalRefineToolInput,
		ExperimentalDownload:           callConfig.ExperimentalDownload,
		InstructionMessages:            callConfig.InstructionMessages,
		PrepareStep:                    callConfig.PrepareStep,
		RepairToolCall:                 callConfig.RepairToolCall,
		OnLanguageModelCallStart:       callConfig.OnLanguageModelCallStart,
		OnLanguageModelCallEnd:         callConfig.OnLanguageModelCallEnd,
		RuntimeContext:                 callConfig.RuntimeContext,
		ToolsContext:                   callConfig.ToolsContext,
		SensitiveRuntimeContext:        callConfig.SensitiveRuntimeContext,
		ExperimentalContext:            config.ExperimentalContext,
		Output:                         callConfig.Output,
		Telemetry:                      callConfig.Telemetry,
		ExperimentalTelemetry:          callConfig.ExperimentalTelemetry, //nolint:staticcheck // forwarded for callers still setting the deprecated field; Telemetry (set above) wins when both are present
		Internal:                       callConfig.Internal,
		OnChunk:                        opts.OnChunk,
		InitialStreamChunks:            opts.InitialStreamChunks,
		ExperimentalTransform:          opts.ExperimentalTransform,
		OnError:                        mergeErrorHandlers(config.ErrorHandler, opts.OnError),
		OnAbortEvent:                   agentAbortEvent(mergeAbortHandlers(config.AbortHandler, opts.OnAbort)),
		OnStart:                        cbs.onStart,
		OnStepStart:                    cbs.onStepStart,
		OnStepError:                    config.OnStepError,
		ExperimentalSteering:           opts.ExperimentalSteering,
		OnToolExecutionStart:           cbs.onToolCallStart,
		OnToolExecutionEnd:             cbs.onToolCallFinish,
		OnStepEndEvent:                 cbs.onStepFinish,
		OnEndEvent:                     cbs.onFinish,
	}
	return ai.StreamText(ctx, streamOpts)
}

// Execute runs the agent with a simple text prompt
func (a *ToolLoopAgent) Execute(ctx context.Context, prompt string) (*AgentResult, error) {
	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: prompt},
			},
		},
	}

	return a.ExecuteWithMessages(ctx, messages)
}

// ExecuteWithMessages runs the agent with a message history
func (a *ToolLoopAgent) ExecuteWithMessages(ctx context.Context, messages []types.Message) (*AgentResult, error) {
	return a.executeWithMessages(ctx, messages, agentCallbacks{})
}

func (a *ToolLoopAgent) executeWithMessages(ctx context.Context, messages []types.Message, callCallbacks agentCallbacks) (*AgentResult, error) {
	// Validate configuration
	if a.config.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	// runLoop opens the telemetry root span (unless the caller already did)
	// once the first step's PrepareCall has run, so the step, model-call and
	// tool-call spans have a parent; see loop_telemetry.go.
	baseCtx := ctx
	var lt *loopTelemetry
	result, err := a.runLoop(ctx, messages, callCallbacks, &lt)
	if lt != nil {
		// Telemetry gets the original error (TS telemetry onError).
		a.endLoopTelemetry(baseCtx, lt, result, err)
	}
	if err != nil {
		// TS ToolLoopAgent runs generateText, which rethrows every failure
		// through wrapGatewayError.
		return result, aibridge.WrapGatewayError(err)
	}
	return result, nil
}

func (a *ToolLoopAgent) runLoop(ctx context.Context, messages []types.Message, callCallbacks agentCallbacks, ltOut **loopTelemetry) (*AgentResult, error) {
	if a.config.Model.SpecificationVersion() == "" || a.config.Model.Provider() == "" || a.config.Model.ModelID() == "" {
		return nil, fmt.Errorf("model must implement provider.LanguageModel metadata methods")
	}
	defaultedCallOptions, err := validateAgentCallOptions(a.config.CallOptionsSchema, a.config.CallOptions)
	if err != nil {
		return nil, err
	}
	if defaultedCallOptions != nil {
		// Reassign the local receiver to a shallow copy carrying the
		// defaulted call options, rather than mutating a.config in place:
		// a is a shared *ToolLoopAgent that callers may reuse across
		// concurrent calls, so writing through it here would be a data
		// race. This only shadows the local variable for the rest of this
		// call; the caller's agent is untouched.
		localAgent := *a
		localAgent.config.CallOptions = defaultedCallOptions
		a = &localAgent
	}

	// Initialize run tracking in context if not already present
	// Generate a new run ID if one doesn't exist
	if ctx.Value(runIDKey) == nil {
		runID := uuid.New().String()
		ctx = context.WithValue(ctx, runIDKey, runID)
	}

	// The first step's PrepareCall runs before the root span opens, as TS
	// ToolLoopAgent applies prepareCall before generateText reports its
	// start: the root, the start event and the approval resumption use the
	// prepared configuration, and step 0 runs with it (PrepareCall is not
	// called again for it).
	firstCallConfig := a.prepareCallConfig(ctx, 0, "", messages, nil, types.Usage{}, nil)
	// TS generateText validates the prepared call's toolCallConcurrency
	// first, before anything starts.
	if err := validateToolCallConcurrency(firstCallConfig.ToolCallConcurrency); err != nil {
		return nil, err
	}
	var lt *loopTelemetry
	ctx, lt = a.beginLoopTelemetry(ctx, firstCallConfig)
	ctx = context.WithValue(ctx, loopTelemetryKey{}, lt)
	*ltOut = lt

	// CB-T23: Merge settings-level callbacks with no per-call overrides.
	// Per-call callback merging is used when ToolLoopAgent is called via
	// dedicated generate/stream wrappers that accept per-call callbacks.
	cbs := mergeCallbacks(a.config, callCallbacks)

	// Extract input for OnChainStart callback
	input := ""
	if len(messages) > 0 {
		for _, part := range messages[0].Content {
			if textPart, ok := part.(types.TextContent); ok {
				input = textPart.Text
				break
			}
		}
	}

	// Call OnChainStart callback
	if a.config.OnChainStart != nil {
		a.config.OnChainStart(input, messages)
	}

	// CB-T23: Emit OnStartEvent with the prepared call (TS generateText's
	// start event reports the prepareCall result).
	fc := firstCallConfig
	loopCallID := ""
	if lt != nil {
		loopCallID = lt.callID
	}
	ai.Notify(ctx, ai.OnStartEvent{
		// TS ToolLoopAgent runs generateText, whose start event carries the
		// call id, the operation and the call settings.
		CallID:      loopCallID,
		OperationID: "ai.generateText",
		Provider:    fc.Model.Provider(),
		// ModelProvider/System are deprecated aliases of Provider/Instructions;
		// set for callbacks still reading the old fields (mirrors
		// generate.go's OnStartEvent population).
		ModelProvider:       fc.Model.Provider(), //nolint:staticcheck
		ModelID:             fc.Model.ModelID(),
		Instructions:        fc.System,
		InstructionMessages: fc.InstructionMessages,
		System:              fc.System, //nolint:staticcheck
		Messages:            fc.Messages,
		Tools:               fc.Tools,
		ActiveTools:         fc.ActiveTools,
		ToolOrder:           fc.ToolOrder,
		ToolChoice:          fc.ToolChoice,
		Timeout:             fc.Timeout,
		Reasoning:           fc.Reasoning,
		MaxRetries:          ai.GatewayMaxRetries(fc.Model, fc.MaxRetries),
		Headers:             loopStepHeaders(fc.Internal, fc.Headers),
		Output:              fc.Output,
		Temperature:         fc.Temperature,
		MaxTokens:           fc.MaxTokens,
		TopP:                fc.TopP,
		TopK:                fc.TopK,
		FrequencyPenalty:    fc.FrequencyPenalty,
		PresencePenalty:     fc.PresencePenalty,
		StopSequences:       fc.StopSequences,
		Seed:                fc.Seed,
		ProviderOptions:     fc.ProviderOptions,
		ExperimentalContext: a.config.ExperimentalContext,
		RuntimeContext:      fc.RuntimeContext,
		ToolsContext:        fc.ToolsContext,
	}, cbs.onStart)

	// Apply total timeout if configured (TS `timeout ?? preparedCall.timeout`)
	var cancel context.CancelFunc
	if fc.Timeout != nil && fc.Timeout.HasTotal() {
		ctx, cancel = fc.Timeout.CreateTimeoutContext(ctx, "total")
		defer cancel()
	}

	// Initialize result
	result := &AgentResult{
		Steps:       []types.StepResult{},
		ToolResults: []types.ToolResult{},
		Delegations: []SubagentDelegation{},
		Warnings:    append([]types.Warning{}, a.warnings...),
	}

	// Current conversation state: the prepared call's messages.
	currentMessages := make([]types.Message, len(fc.Messages))
	copy(currentMessages, fc.Messages)

	// Resume tool approvals from the input messages before the first step
	// (review finding F5): this loop previously signed approval requests
	// (see agentToolResultsToContentParts) but never resumed them, so a
	// history ending in an approved tool-approval-response silently
	// continued without executing the tool or verifying its signature.
	// TS generateText resumes them with the prepared call's tools, approval
	// configuration and contexts.
	// A caller that resumed the approvals itself (WorkflowAgent,
	// InternalOptions.ApprovalsResumed) leaves nothing to resume here.
	var resumed ai.ResumeToolApprovalsResult
	var resumeErr error
	if fc.Internal == nil || !fc.Internal.ApprovalsResumed {
		resumed, resumeErr = ai.ResumeToolApprovals(ctx, ai.ResumeToolApprovalsOptions{
			Messages:             currentMessages,
			Tools:                fc.Tools,
			ToolApproval:         fc.ToolApproval,
			ToolsContext:         fc.ToolsContext,
			RuntimeContext:       fc.RuntimeContext,
			Secret:               fc.ExperimentalToolApprovalSecret,
			RefineToolInput:      fc.ExperimentalRefineToolInput,
			ToolCallConcurrency:  fc.ToolCallConcurrency,
			ModelProvider:        fc.Model.Provider(),
			ModelID:              fc.Model.ModelID(),
			OnToolExecutionStart: cbs.onToolCallStart,
			OnToolExecutionEnd:   cbs.onToolCallFinish,
		})
	}
	if resumeErr != nil {
		if a.config.OnChainError != nil {
			a.config.OnChainError(resumeErr)
		}
		return nil, resumeErr
	}
	if len(resumed.ResponseMessages) > 0 {
		currentMessages = append(currentMessages, resumed.ResponseMessages...)
	}

	// Custom data for PrepareCall (persists across steps)
	var customData interface{}

	// Deferred tool discovery (ai.ToolSearch / types.Tool.DeferLoading):
	// created once per run from the full configured tool set, then applied
	// per step below so tools marked DeferLoading stay hidden from the
	// model until a toolSearch call surfaces them. This native step loop
	// (executeWithMessages) calls the provider model directly instead of
	// delegating to ai.GenerateText, so it needs its own copy of the same
	// per-step application ai.GenerateText/StreamText perform internally.
	// Mirrors TS workflow/stream-text-iterator.ts's createToolSearchState +
	// prepareToolSearch(filterActiveTools(...)).
	// TS generateText resolves them from the prepared call's tools and
	// tool callers (ToolLoopAgent passes the prepareCall result).
	resolvedToolCallers, err := ai.ResolveToolCallerConfiguration(fc.Tools, fc.ExperimentalToolCallers)
	if err != nil {
		if a.config.OnChainError != nil {
			a.config.OnChainError(err)
		}
		return nil, err
	}
	toolSearchState, err := ai.NewToolSearchState(fc.Tools, resolvedToolCallers)
	if err != nil {
		if a.config.OnChainError != nil {
			a.config.OnChainError(err)
		}
		return nil, err
	}

	// pendingDeferredToolCalls tracks provider-executed calls of tools with
	// SupportsDeferredResults whose result has not arrived yet (TS
	// pendingDeferredToolCalls): the loop keeps going until they resolve.
	pendingDeferredToolCalls := map[string]string{}

	// Execute agent loop
	for stepNum := 1; a.config.MaxSteps <= 0 || stepNum <= a.config.MaxSteps; stepNum++ {
		stepIndex := stepNum - 1
		var callConfig PrepareCallConfig
		// callTools is the step's prepared tool set before ActiveTools, tool
		// search and tool-caller routing narrow it: TS generateText checks a
		// response's provider results against the call's `tools`.
		var callTools []types.Tool
		if stepIndex == 0 {
			// Step 0 runs with the configuration prepared before the root
			// opened, plus any messages the approval resumption added.
			callTools = fc.Tools
			callConfig = a.finishStepCallConfig(ctx, stepIndex, fc)
			callConfig.Messages = currentMessages
		} else {
			prepared := a.prepareCallConfig(ctx, stepIndex, "", currentMessages, result.Steps, result.Usage, customData)
			callTools = prepared.Tools
			callConfig = a.finishStepCallConfig(ctx, stepIndex, prepared)
		}
		// A PrepareCall override of the telemetry settings (TS prepareCall
		// `telemetry`) applies to this and the following steps' spans.
		if callConfig.Telemetry != nil && callConfig.Telemetry != a.config.Telemetry {
			ctx = a.adoptPreparedTelemetry(ctx, lt, callConfig.Telemetry, callConfig)
		}

		// Apply deferred tool discovery (ai.ToolSearch / types.Tool.DeferLoading)
		// and tool-caller routing (ExperimentalToolCallers) before the
		// step-start notification and the model call, mirroring
		// ai.GenerateText's per-step pipeline
		// (pkg/ai/generate.go: toolSearchState.Apply ->
		// PrepareToolsForToolCallers -> AppendToolCallerMessages ->
		// resolve/order -> Notify(OnStepStartEvent)): undiscovered
		// DeferLoading tools are hidden and any toolSearch tool is rebound
		// with an Execute scoped to its deferred candidates, then tools
		// routed exclusively through a local/provider caller are hidden
		// from the model (executionTools keeps the caller-bound Execute
		// used below to actually run the call; callConfig.Tools becomes
		// the model-visible set sent to the model, while OnStepStartEvent
		// reports the call's tools, as TS stepStartEvent.tools). A local caller's catalog-announcement message, if
		// any, is appended to callConfig.Messages below, and persists into
		// currentMessages once this step's response messages are recorded
		// (see the currentMessages assignment after executeTools/
		// ConvertToResponseMessages below), matching TS generate-text.ts's
		// `messagesForNextStep = [...stepMessages, ...stepResponseMessages]`
		// where stepMessages already carries that step's
		// appendToolCallerMessages announcement.
		callConfig.Tools = toolSearchState.Apply(callConfig.Tools, callConfig.ToolsContext, callConfig.ExperimentalSandbox)
		executionTools, modelTools, toolCallerMessages := ai.PrepareToolsForToolCallers(callConfig.Tools, resolvedToolCallers)
		callConfig.Tools = modelTools
		if len(toolCallerMessages) > 0 {
			callConfig.Messages = ai.AppendToolCallerMessages(callConfig.Messages, toolCallerMessages)
		}

		// CB-T23: Emit OnStepStartEvent. It precedes the step's prompt
		// conversion, as TS WorkflowAgent's onStepStart does (this loop runs
		// WorkflowAgent.Generate); a step that fails after it ends with
		// OnStepError (TS generateText onStepError).
		stepStartEvent := ai.OnStepStartEvent{
			CallID:     loopCallID,
			StepNumber: stepIndex,
			Provider:   callConfig.Model.Provider(),
			// ModelProvider/System are deprecated aliases of Provider/
			// Instructions; set for callbacks still reading the old fields
			// (mirrors generate.go's OnStepStartEvent population).
			ModelProvider:       callConfig.Model.Provider(), //nolint:staticcheck
			ModelID:             callConfig.Model.ModelID(),
			Instructions:        callConfig.System,
			InstructionMessages: callConfig.InstructionMessages,
			System:              callConfig.System, //nolint:staticcheck
			Messages:            callConfig.Messages,
			// TS stepStartEvent.tools: the call's tools, before ActiveTools,
			// tool search and tool-caller routing narrow them.
			Tools:               callTools,
			ToolChoice:          callConfig.ToolChoice,
			ActiveTools:         callConfig.ActiveTools,
			ToolOrder:           callConfig.ToolOrder,
			ProviderOptions:     callConfig.ProviderOptions,
			Output:              callConfig.Output,
			Steps:               result.Steps,
			PreviousSteps:       result.Steps,
			ExperimentalContext: a.config.ExperimentalContext,
			RuntimeContext:      callConfig.RuntimeContext,
			ToolsContext:        callConfig.ToolsContext,
		}
		ai.Notify(ctx, stepStartEvent, cbs.onStepStart)
		stepStartNotified := true
		// failStep reports a failure of a step that started to OnStepError
		// (TS notifies onStepError with wrapGatewayError(error)).
		failStep := func(err error) {
			if !stepStartNotified {
				return
			}
			stepStartNotified = false
			ai.Notify(ctx, ai.OnStepErrorEvent{OnStepStartEvent: stepStartEvent, Error: aibridge.WrapGatewayError(err)}, a.config.OnStepError)
		}

		// Call step start callback (legacy)
		if a.config.OnStepStart != nil {
			a.config.OnStepStart(stepIndex)
		}

		// Open the step telemetry span; tools and the model call run under it.
		stepTel := a.startStepTelemetry(ctx, lt, stepIndex, callConfig)
		// The per-step timeout (a PrepareCall override included) spans the
		// model call and the step's tool execution, as TS generateText's
		// step timeout aborts mergedAbortSignal, which executeTools uses.
		stepCtx, cancelStepTimeout := a.stepTimeoutContext(stepTel.stepCtx, callConfig)

		// Execute one step with custom data
		stepStarted := time.Now()
		stepResult, shouldContinue, newCustomData, _, err := a.executeStep(stepCtx, callConfig, callTools, stepTel)
		customData = newCustomData
		if err == nil {
			// TS generateText continues only for client (locally executed)
			// tool calls or while a deferred-result provider tool is still
			// pending; a step whose provider-executed calls all carry their
			// results ends the run.
			shouldContinue = agentStepContinues(stepResult, callConfig.Tools, pendingDeferredToolCalls)
		}
		if err != nil {
			cancelStepTimeout()
			stepTel.fail(err)
			failStep(err)
			// Call OnChainError callback
			if a.config.OnChainError != nil {
				a.config.OnChainError(err)
			}
			return nil, fmt.Errorf("step %d failed: %w", stepNum, err)
		}

		responseContent := append([]types.ContentPart(nil), stepResult.Content...)
		// The model's own content, before any tool output is appended.
		modelContent := responseContent
		toolResultsBefore := len(result.ToolResults)

		// If there are tool calls, execute them
		var stepToolResults []types.ToolResult
		// WorkflowAgent: a step that finished with a reason other than stop or
		// tool-calls ends the run; its tool calls are neither executed nor
		// answered (TS modelCallIterator shouldProcessTools).
		workflowBlocked := aibridge.WorkflowBlockedToolStep(callConfig.Internal != nil && callConfig.Internal.ContinueOnMissingProviderResult, stepResult.FinishReason, stepResult.Content)
		if workflowBlocked {
			shouldContinue = false
		}
		// Notify the tools that their calls are available and resolve each
		// call's approval in the same pass, before any tool runs (TS
		// generateText: per valid call, OnInputStart, OnInputAvailable, then
		// resolveToolApproval; an error fails the run).
		preApprovals, inputErr, approvalErr := a.notifyToolCallsAndResolveApprovals(stepCtx, stepResult.ToolCalls, executionTools, callConfig.Messages, callConfig.RuntimeContext, callConfig.ToolsContext, callConfig.ToolApproval, len(stepResult.ToolCalls) > 0 && !workflowBlocked)
		if inputErr != nil || approvalErr != nil {
			cancelStepTimeout()
			failErr := approvalErr
			if inputErr != nil {
				failErr = inputErr
			}
			stepTel.fail(failErr)
			failStep(failErr)
			if a.config.OnChainError != nil {
				a.config.OnChainError(failErr)
			}
			if inputErr != nil {
				return nil, fmt.Errorf("step %d failed: %w", stepNum, inputErr)
			}
			return nil, approvalErr
		}
		if len(stepResult.ToolCalls) > 0 && !workflowBlocked {
			// Call OnAgentAction callback for each tool call
			if a.config.OnAgentAction != nil {
				// Extract run tracking from context
				runID, _ := ctx.Value(runIDKey).(string)
				parentRunID, _ := ctx.Value(parentRunIDKey).(string)
				tags, _ := ctx.Value(tagsKey).([]string)

				for _, toolCall := range stepResult.ToolCalls {
					action := AgentAction{
						ToolCall:    toolCall,
						StepNumber:  stepIndex,
						Reasoning:   stepResult.Text, // Include any reasoning text from the step
						RunID:       runID,
						ParentRunID: parentRunID,
						Tags:        tags,
					}
					a.config.OnAgentAction(action)
				}
			}
			// callConfig.Messages (this step's messages, including its own
			// AppendToolCallerMessages announcement), not currentMessages,
			// matches TS generate-text.ts's executeToolCall({ messages: stepMessages, ... }).
			timings := &toolTimings{}
			callTools := executionTools
			if reason := stepResult.FinishReason; reason != "" && reason != types.FinishReasonStop && reason != types.FinishReasonToolCalls {
				// A truncated, filtered or errored step never runs local
				// tools (TS isToolExecutionAllowedFinishReason); approvals and
				// error results for invalid calls still resolve.
				callTools = make([]types.Tool, len(executionTools))
				for i, tool := range executionTools {
					if !tool.ProviderExecuted {
						tool.Execute = nil
					}
					callTools[i] = tool
				}
			}
			// A PrepareCall override of the step's tool-call concurrency (TS
			// prepareCall toolCallConcurrency) bounds this step's calls.
			stepAgent := a
			if callConfig.ToolCallConcurrency != a.config.ToolCallConcurrency {
				if err := validateToolCallConcurrency(callConfig.ToolCallConcurrency); err != nil {
					cancelStepTimeout()
					stepTel.fail(err)
					failStep(err)
					return nil, err
				}
				local := *a
				local.config.ToolCallConcurrency = callConfig.ToolCallConcurrency
				stepAgent = &local
			}
			// The tool timeouts come from the step's timeout (a PrepareCall
			// override included), as the step timeout does (TS
			// `timeout ?? preparedCall.timeout` reaches executeToolCall).
			if callConfig.Timeout != nil && callConfig.Timeout != stepAgent.config.Timeout {
				local := *stepAgent
				local.config.Timeout = callConfig.Timeout
				stepAgent = &local
			}
			// The tool-call events report the step's model (a PrepareCall model
			// override), not the agent's configured one.
			if callConfig.Model != nil && callConfig.Model != stepAgent.config.Model {
				local := *stepAgent
				local.config.Model = callConfig.Model
				stepAgent = &local
			}
			toolResults, err := stepAgent.executeTools(stepCtx, stepResult.ToolCalls, callTools, callConfig.Messages, stepIndex, callConfig.RuntimeContext, callConfig.ToolsContext, callConfig.ExperimentalSandbox, callConfig.ToolApproval, preApprovals, cbs, result.Usage.Add(stepResult.Usage), timings)
			if err != nil {
				cancelStepTimeout()
				stepTel.fail(err)
				var conversionErr *toolResultModelOutputError
				if errors.As(err, &conversionErr) {
					failStep(conversionErr.Unwrap())
				} else {
					failStep(err)
				}
				// Call OnChainError callback
				if a.config.OnChainError != nil {
					a.config.OnChainError(err)
				}
				if conversionErr != nil {
					return nil, conversionErr.Unwrap()
				}
				return nil, err
			}

			// An approved local call that did not run (no Execute, or a finish
			// reason that blocks execution) has no result, but TS still names
			// its approval (in call order) and records the approval request
			// and response.
			approvalOnly := agentApprovedUnexecutedCalls(stepResult.ToolCalls, callTools, preApprovals, toolResults)
			toolResults, approvalOnly = splitAgentApprovalOnly(mergeAgentApprovalOnly(toolResults, stepResult.ToolCalls, approvalOnly), callConfig.Internal)

			// WorkflowAgent: a provider-executed call with no result gets an
			// empty text result (TS resolveProviderToolResult) instead of the
			// pending marker, so the loop continues with a text output.
			if callConfig.Internal != nil && callConfig.Internal.ContinueOnMissingProviderResult {
				for _, synthetic := range aibridge.EmptyProviderResults(stepResult.ToolCalls, callConfig.Tools, stepResult.Content, stepResult.FinishReason, stepResult.Model.Provider, stepResult.Model.ModelID) {
					for i := range toolResults {
						if toolResults[i].ToolCallID == synthetic.ToolCallID && toolResults[i].ProviderExecuted && toolResults[i].Result == nil && toolResults[i].Error == nil {
							toolResults[i].Result = synthetic.Result
							toolResults[i].ModelOutput = synthetic.ModelOutput
							// The answered call keeps the loop going (as
							// generate.go's injectedProviderResult).
							shouldContinue = true
						}
					}
				}
			}
			stepToolResults = toolResults
			stepResult.Performance.ToolExecutionMs = timings.snapshot()
			// TS toolResults holds only tool-result content parts; approval
			// bookkeeping entries stay in the content parts and response
			// messages (built from the full list below).
			publicResults := types.ResultsForPublicView(toolResults)
			stepResult.ToolResults = publicResults
			stepResult.StaticToolResults = filterAgentStaticToolResults(publicResults)
			stepResult.DynamicToolResults = filterAgentDynamicToolResults(publicResults)
			stepResult.Content = append(stepResult.Content, agentToolResultsToContentParts(mergeAgentApprovalOnly(toolResults, stepResult.ToolCalls, approvalOnly), callConfig.ExperimentalToolApprovalSecret)...)
			responseContent = stepResult.Content
			result.ToolResults = append(result.ToolResults, publicResults...)
			if len(toolResults) == 0 {
				shouldContinue = false
			}
			// A tool call awaiting user approval pauses the loop, as in
			// ai.GenerateText and TS WorkflowAgent: the next call resumes it
			// with the approval response. Continuing would send the model a
			// tool call that has no tool result.
			for _, tr := range toolResults {
				if tr.ApprovalStatus == types.ToolApprovalStatusUserApproval {
					shouldContinue = false
					break
				}
			}
			// Continue only after every local tool call has a result (TS:
			// clientToolOutputs + denied === clientToolCalls); a call left
			// without one (a tool with no Execute) pauses the loop instead of
			// sending the model a dangling tool call.
			if hasUnresolvedLocalAgentToolCalls(stepResult.ToolCalls, executionTools, toolResults) {
				shouldContinue = false
			}
		}
		// Provider-executed results the model returned in this response are
		// tool-result content parts, so TS step.toolResults has them (in
		// content order, before the locally executed results), as
		// GenerateText's step does.
		if providerResults := aibridge.ProviderToolResultsFromContent(modelContent, stepToolResults); len(providerResults) > 0 {
			stepResult.ToolResults = append(providerResults, stepResult.ToolResults...)
			stepResult.StaticToolResults = filterAgentStaticToolResults(stepResult.ToolResults)
			stepResult.DynamicToolResults = filterAgentDynamicToolResults(stepResult.ToolResults)
			result.ToolResults = append(result.ToolResults[:toolResultsBefore:toolResultsBefore], stepResult.ToolResults...)
		}
		// A provider-executed call the provider gated behind its own
		// tool-approval-request (MCP) pauses the loop until the caller
		// answers; TS WorkflowAgent returns on providerApprovalRequests.
		if agentHasProviderApprovalRequest(stepResult.Content) {
			shouldContinue = false
		}
		// An interrupted step (provider stream ended without a completion
		// signal) never continues the loop (TS #23778).
		if stepResult.FinishReason == types.FinishReasonInterrupted {
			shouldContinue = false
		}
		// A step timeout that expired while the tools ran aborts the rest of
		// the run (checked below, once the step is recorded).
		var stepTimeoutErr error
		if errors.Is(stepCtx.Err(), context.DeadlineExceeded) {
			stepTimeoutErr = stepCtx.Err()
		}
		cancelStepTimeout()
		// TS stepTimeMs spans the model call and the tool execution.
		if !stepTel.startedAt.IsZero() {
			stepStarted = stepTel.startedAt
		}
		stepResult.Performance.StepTimeMs = time.Since(stepStarted).Milliseconds()
		responseCalls := stepResult.ToolCalls
		if workflowBlocked {
			responseCalls, responseContent = aibridge.WorkflowBlockedResponse(responseCalls, responseContent)
		}
		responseMessages := providerutils.ConvertToResponseMessages(responseCalls, responseContent, stepToolResults)
		stepResult.ResponseMessages = responseMessages
		stepResult.Response.Messages = responseMessages
		// Use callConfig.Messages (this step's messages after
		// AppendToolCallerMessages above), not the stale pre-announcement
		// currentMessages, so a local caller's catalog announcement
		// persists into subsequent steps' history instead of being dropped
		// after one step (TS generate-text.ts: messagesForNextStep =
		// [...stepMessages, ...stepResponseMessages]).
		currentMessages = append(append([]types.Message(nil), callConfig.Messages...), responseMessages...)

		// Add step to results after tool execution so step data mirrors TS step objects.
		result.Steps = append(result.Steps, *stepResult)
		result.Usage = result.Usage.Add(stepResult.Usage)
		result.Warnings = append(result.Warnings, stepResult.Warnings...)

		stepTel.finish(stepResult, callConfig.RuntimeContext, callConfig.ToolsContext)

		// Call step end callback (legacy/basic).
		if onStepEnd := resolveAgentBasicOnStepEnd(a.config); onStepEnd != nil {
			onStepEnd(*stepResult)
		}

		// CB-T23: Emit OnStepFinishEvent (after tool execution so ToolResults is populated)
		ai.Notify(ctx, ai.OnStepFinishEvent{
			CallID:           stepTel.callID(),
			StepNumber:       stepResult.StepNumber,
			Model:            stepResult.Model,
			ModelProvider:    stepResult.Model.Provider,
			ModelID:          stepResult.Model.ModelID,
			Text:             stepResult.Text,
			Reasoning:        stepResult.Reasoning,
			ReasoningText:    stepResult.ReasoningText,
			ToolCalls:        stepResult.ToolCalls,
			StaticToolCalls:  stepResult.StaticToolCalls,
			DynamicToolCalls: stepResult.DynamicToolCalls,
			// The public results, like StaticToolResults/DynamicToolResults
			// and GenerateText's step event: approval requests and pending
			// provider markers are content parts, not tool results.
			ToolResults:        stepResult.ToolResults,
			StaticToolResults:  stepResult.StaticToolResults,
			DynamicToolResults: stepResult.DynamicToolResults,
			FinishReason:       stepResult.FinishReason,
			RawFinishReason:    stepResult.RawFinishReason,
			Usage:              stepResult.Usage,
			Warnings:           stepResult.Warnings,
			Sources:            stepResult.Sources,
			Files:              stepResult.Files,
			ProviderMetadata:   stepResult.ProviderMetadata,
			ResponseHeaders:    stepResult.Response.Headers,
			Request:            ai.GenerateStepRequest{Body: stepResult.Request.Body},
			Response: ai.GenerateStepResponse{
				ID:        stepResult.Response.ID,
				Timestamp: stepResult.Response.Timestamp,
				ModelID:   stepResult.Response.ModelID,
				Headers:   stepResult.Response.Headers,
				Messages:  stepResult.ResponseMessages,
				Body:      stepResult.Response.Body,
			},
			ExperimentalContext: a.config.ExperimentalContext,
			RuntimeContext:      callConfig.RuntimeContext,
			ToolsContext:        callConfig.ToolsContext,
		}, cbs.onStepFinish)

		// Check if we should continue
		if !shouldContinue {
			result.Text = stepResult.Text
			result.FinishReason = stepResult.FinishReason

			// Call OnAgentFinish callback when agent reaches final answer
			if a.config.OnAgentFinish != nil {
				// Extract run tracking from context
				runID, _ := ctx.Value(runIDKey).(string)
				parentRunID, _ := ctx.Value(parentRunIDKey).(string)
				tags, _ := ctx.Value(tagsKey).([]string)

				finish := AgentFinish{
					Output:       stepResult.Text,
					StepNumber:   stepIndex,
					FinishReason: stepResult.FinishReason,
					Metadata: map[string]interface{}{
						"total_steps": stepNum,
						"usage":       result.Usage,
					},
					RunID:       runID,
					ParentRunID: parentRunID,
					Tags:        tags,
				}
				a.config.OnAgentFinish(finish)
			}
			break
		}

		// Evaluate stop conditions
		if len(callConfig.StopWhen) > 0 {
			state := ai.StopConditionState{
				Steps:    result.Steps,
				Messages: currentMessages,
				Usage:    result.Usage,
			}
			if reason := ai.EvaluateStopConditions(callConfig.StopWhen, state); reason != "" {
				result.StopReason = reason
				result.Text = stepResult.Text
				result.FinishReason = stepResult.FinishReason

				// Call OnAgentFinish callback
				if a.config.OnAgentFinish != nil {
					runID, _ := ctx.Value(runIDKey).(string)
					parentRunID, _ := ctx.Value(parentRunIDKey).(string)
					tags, _ := ctx.Value(tagsKey).([]string)

					finish := AgentFinish{
						Output:       stepResult.Text,
						StepNumber:   stepIndex,
						FinishReason: stepResult.FinishReason,
						Metadata: map[string]interface{}{
							"total_steps": stepNum,
							"usage":       result.Usage,
							"stop_reason": reason,
						},
						RunID:       runID,
						ParentRunID: parentRunID,
						Tags:        tags,
					}
					a.config.OnAgentFinish(finish)
				}
				break
			}
		}

		// Check if we've hit max steps
		if stepNum == a.config.MaxSteps {
			result.Text = stepResult.Text
			result.FinishReason = types.FinishReasonLength
			result.Warnings = append(result.Warnings, types.Warning{
				Type:    "max_steps_reached",
				Message: fmt.Sprintf("Agent reached maximum steps (%d)", a.config.MaxSteps),
			})

			// Call OnAgentFinish callback when hitting max steps
			if a.config.OnAgentFinish != nil {
				// Extract run tracking from context
				runID, _ := ctx.Value(runIDKey).(string)
				parentRunID, _ := ctx.Value(parentRunIDKey).(string)
				tags, _ := ctx.Value(tagsKey).([]string)

				finish := AgentFinish{
					Output:       stepResult.Text,
					StepNumber:   stepIndex,
					FinishReason: types.FinishReasonLength,
					Metadata: map[string]interface{}{
						"total_steps":   stepNum,
						"usage":         result.Usage,
						"max_steps_hit": true,
					},
					RunID:       runID,
					ParentRunID: parentRunID,
					Tags:        tags,
				}
				a.config.OnAgentFinish(finish)
			}
			break
		}

		// TS generateText: the step timeout aborts the call's shared signal
		// for good, so the next step rejects (a tool that swallowed the
		// abort does not prevent it).
		if stepTimeoutErr != nil && ctx.Err() == nil {
			if a.config.OnChainError != nil {
				a.config.OnChainError(stepTimeoutErr)
			}
			return nil, fmt.Errorf("step %d failed: %w", stepNum+1, stepTimeoutErr)
		}
		// TS generateText `if (steps.length > 0) mergedAbortSignal?.throwIfAborted()`:
		// a run aborted after a step ended (e.g. while a tool that ignores
		// ctx ran) fails before the next step's PrepareCall and step events.
		if ctx.Err() != nil {
			abortErr := context.Cause(ctx)
			if a.config.OnChainError != nil {
				a.config.OnChainError(abortErr)
			}
			return nil, fmt.Errorf("step %d failed: %w", stepNum+1, abortErr)
		}
	}

	// Call OnChainEnd callback (successful completion)
	if a.config.OnChainEnd != nil {
		a.config.OnChainEnd(result)
	}
	// CB-T23: the OnFinishEvent. Like the TS onEnd event, it aggregates
	// content, tool calls, results, sources, files and warnings across steps.
	emitFinishEvent := func() {
		finishEvent := ai.OnFinishEvent{
			Text:                result.Text,
			FinishReason:        result.FinishReason,
			Steps:               result.Steps,
			TotalUsage:          result.Usage,
			ExperimentalContext: a.config.ExperimentalContext,
			RuntimeContext:      a.runtimeContext(),
			ToolsContext:        a.config.ToolsContext,
		}
		if lt != nil {
			finishEvent.CallID = lt.callID
		}
		// TS onEndEvent: the final step's number, model, reasoning, raw finish
		// reason, provider metadata, request and response, and its runtime
		// and tools context.
		if n := len(result.Steps); n > 0 {
			last := result.Steps[n-1]
			finishEvent.StepNumber = last.StepNumber
			finishEvent.Model = last.Model
			finishEvent.ModelProvider = last.Model.Provider
			finishEvent.ModelID = last.Model.ModelID
			finishEvent.Reasoning = last.Reasoning
			finishEvent.ReasoningText = last.ReasoningText
			finishEvent.RawFinishReason = last.RawFinishReason
			finishEvent.ProviderMetadata = last.ProviderMetadata
			finishEvent.ResponseHeaders = last.Response.Headers
			finishEvent.Request = ai.GenerateStepRequest{Body: last.Request.Body}
			finishEvent.Response = ai.GenerateStepResponse{
				ID:        last.Response.ID,
				Timestamp: last.Response.Timestamp,
				ModelID:   last.Response.ModelID,
				Headers:   last.Response.Headers,
				Messages:  last.ResponseMessages,
				Body:      last.Response.Body,
			}
			finishEvent.RuntimeContext = last.RuntimeContext
			finishEvent.ToolsContext = last.ToolsContext
		}
		finishEvent.Output = result.Output
		finishEvent.SetAggregatesFromSteps(result.Steps, append(append([]types.Message(nil), resumed.ResponseMessages...), responseMessagesOf(result.Steps)...))
		if len(finishEvent.ToolResults) == 0 {
			finishEvent.ToolResults = result.ToolResults
		}
		if len(finishEvent.Warnings) == 0 {
			finishEvent.Warnings = result.Warnings
		}
		ai.Notify(ctx, finishEvent, cbs.onFinish)
	}
	// TS parses the output only for a stop finish, or a non-tool-calls finish
	// that produced text, and only after onEnd has run: a parse failure
	// rejects after the end event, and a tool-calls finish leaves the output
	// unset instead of failing the call.
	// The output is the prepared call's (TS ToolLoopAgent passes the
	// prepareCall result to generateText); with no Output it is the text
	// (TS `output ?? text()`), under the same finish-reason rule.
	if result.Output == nil && shouldParseAgentOutput(result.FinishReason, result.Text) {
		output, err := parseAgentOutput(ctx, fc.Output, result)
		if err != nil {
			emitFinishEvent()
			if a.config.OnChainError != nil {
				a.config.OnChainError(err)
			}
			return nil, err
		}
		result.Output = output
	}

	// Call finish callback (legacy)
	if a.config.OnFinish != nil {
		a.config.OnFinish(result)
	}

	emitFinishEvent()

	return result, nil
}

// prepareCallConfig builds the call configuration from the agent config and
// runs PrepareCall on it (TS ToolLoopAgent.prepareCall).
func (a *ToolLoopAgent) prepareCallConfig(ctx context.Context, stepNum int, prompt string, messages []types.Message, previousSteps []types.StepResult, accumulatedUsage types.Usage, customData interface{}) PrepareCallConfig {
	callConfig := PrepareCallConfig{
		StepNumber:                     stepNum,
		Model:                          a.config.Model,
		System:                         a.config.System,
		AllowSystemInMessages:          a.config.AllowSystemInMessages,
		Prompt:                         prompt,
		Messages:                       messages,
		Tools:                          a.config.Tools,
		ActiveTools:                    a.config.ActiveTools,
		ToolChoice:                     a.config.ToolChoice,
		ToolOrder:                      a.config.ToolOrder,
		ToolCallConcurrency:            a.config.ToolCallConcurrency,
		ExperimentalToolCallers:        a.config.ExperimentalToolCallers,
		ToolApproval:                   a.config.ToolApproval,
		ExperimentalToolApprovalSecret: a.config.ExperimentalToolApprovalSecret,
		SensitiveRuntimeContext:        a.config.SensitiveRuntimeContext,
		Temperature:                    a.config.Temperature,
		MaxTokens:                      a.config.MaxTokens,
		TopP:                           a.config.TopP,
		TopK:                           a.config.TopK,
		FrequencyPenalty:               a.config.FrequencyPenalty,
		PresencePenalty:                a.config.PresencePenalty,
		StopSequences:                  a.config.StopSequences,
		Seed:                           a.config.Seed,
		Headers:                        a.config.Headers,
		Reasoning:                      a.config.Reasoning,
		SendReasoning:                  a.config.SendReasoning,
		ProviderOptions:                a.config.ProviderOptions,
		StopWhen:                       a.config.StopWhen,
		Output:                         a.config.Output,
		Telemetry:                      a.config.Telemetry,
		ExperimentalTelemetry:          a.config.ExperimentalTelemetry, //nolint:staticcheck // forwarded for callers still setting the deprecated field; Telemetry (set above) wins when both are present
		Include:                        a.config.Include,
		Internal:                       a.config.Internal,
		ExperimentalSandbox:            a.config.ExperimentalSandbox,
		ExperimentalRefineToolInput:    a.config.ExperimentalRefineToolInput,
		ExperimentalDownload:           a.config.ExperimentalDownload,
		InstructionMessages:            a.config.InstructionMessages,
		PrepareStep:                    a.config.PrepareStep,
		RepairToolCall:                 firstNonNilRepairToolCall(a.config.RepairToolCall, a.config.ExperimentalRepairToolCall),
		OnLanguageModelCallStart:       firstNonNilLMCallStart(a.config.OnLanguageModelCallStart, a.config.ExperimentalOnLanguageModelCallStart),
		OnLanguageModelCallEnd:         firstNonNilLMCallEnd(a.config.OnLanguageModelCallEnd, a.config.ExperimentalOnLanguageModelCallEnd),
		RuntimeContext:                 a.runtimeContext(),
		ToolsContext:                   a.config.ToolsContext,
		PreviousSteps:                  previousSteps,
		AccumulatedUsage:               accumulatedUsage,
		CustomData:                     customData,
		CallOptions:                    a.config.CallOptions,
		MaxRetries:                     a.config.MaxRetries,
		Timeout:                        a.config.Timeout,
	}

	if a.config.PrepareCall != nil {
		callConfig = a.config.PrepareCall(ctx, callConfig)
	}
	if len(callConfig.InstructionMessages) > 0 {
		// Instructions given as system messages replace the text
		// instructions (TS instructions is one value), as GenerateText
		// reports them: the start, step-start and model-call events carry
		// the messages and no text instructions.
		callConfig.System = ""
	}
	if callConfig.Model == nil {
		callConfig.Model = a.config.Model
	}
	if callConfig.RuntimeContext == nil {
		callConfig.RuntimeContext = a.runtimeContext()
	}
	if callConfig.ToolsContext == nil {
		callConfig.ToolsContext = a.config.ToolsContext
	}
	return callConfig
}

// prepareGenerateCallConfig is prepareCallConfig for Generate and Stream.
// The Go-only FilterActiveTools callback runs once on the call's tool set;
// GenerateText/StreamText then apply ActiveTools after PrepareStep. A
// per-call timeout wins over the one PrepareCall returns (TS
// `timeout ?? preparedCall.timeout`).
func (a *ToolLoopAgent) prepareGenerateCallConfig(ctx context.Context, prompt string, messages []types.Message, callTimeout *ai.TimeoutConfig) PrepareCallConfig {
	callConfig := a.prepareCallConfig(ctx, 0, prompt, messages, nil, types.Usage{}, nil)
	if callTimeout != nil {
		callConfig.Timeout = callTimeout
	}
	if a.config.FilterActiveTools != nil {
		callConfig.Tools = a.config.FilterActiveTools(ctx, 0, callConfig.Tools)
	}
	return callConfig
}

// prepareStepCallConfig is prepareCallConfig plus the per-step tool
// preparation the Go-only AgentResult loop needs: the tools are narrowed to
// ActiveTools, their descriptions resolved and their order applied.
// Generate and Stream use prepareCallConfig instead and leave this to
// GenerateText/StreamText, which do it per step after PrepareStep (TS
// ToolLoopAgent passes the full tool set and activeTools to generateText).
func (a *ToolLoopAgent) prepareStepCallConfig(ctx context.Context, stepNum int, prompt string, messages []types.Message, previousSteps []types.StepResult, accumulatedUsage types.Usage, customData interface{}) PrepareCallConfig {
	return a.finishStepCallConfig(ctx, stepNum, a.prepareCallConfig(ctx, stepNum, prompt, messages, previousSteps, accumulatedUsage, customData))
}

// finishStepCallConfig applies the per-step tool preparation of
// prepareStepCallConfig to an already prepared configuration.
func (a *ToolLoopAgent) finishStepCallConfig(ctx context.Context, stepNum int, callConfig PrepareCallConfig) PrepareCallConfig {
	// TS filterActiveTools: an empty, non-nil ActiveTools leaves no tools
	// active; only nil keeps them all.
	if callConfig.ActiveTools != nil {
		callConfig.Tools = ai.FilterActiveTools(callConfig.Tools, callConfig.ActiveTools)
	}
	if a.config.FilterActiveTools != nil {
		callConfig.Tools = a.config.FilterActiveTools(ctx, stepNum, callConfig.Tools)
	}
	callConfig.Tools = resolveAgentStepTools(ctx, callConfig.Tools, callConfig.ToolsContext, callConfig.ExperimentalSandbox)
	callConfig.Tools = orderAgentStepTools(callConfig.Tools, callConfig.ToolOrder)
	return callConfig
}

func orderAgentStepTools(tools []types.Tool, toolOrder []string) []types.Tool {
	if toolOrder == nil {
		return tools
	}
	byName := make(map[string]types.Tool, len(tools))
	for _, tool := range tools {
		if _, ok := byName[tool.Name]; !ok {
			byName[tool.Name] = tool
		}
	}
	ordered := make([]types.Tool, 0, len(tools))
	seen := make(map[string]bool, len(tools))
	for _, name := range toolOrder {
		tool, ok := byName[name]
		if !ok || seen[name] {
			continue
		}
		ordered = append(ordered, tool)
		seen[name] = true
	}
	remaining := make([]types.Tool, 0, len(tools)-len(ordered))
	for _, tool := range tools {
		if seen[tool.Name] {
			continue
		}
		remaining = append(remaining, tool)
		seen[tool.Name] = true
	}
	sort.SliceStable(remaining, func(i, j int) bool {
		return remaining[i].Name < remaining[j].Name
	})
	return append(ordered, remaining...)
}

// firstNonNilRepairToolCall returns stable if set, else its deprecated alias.
func firstNonNilRepairToolCall(stable, experimental ai.ToolCallRepairFunction) ai.ToolCallRepairFunction {
	if stable != nil {
		return stable
	}
	return experimental
}

// firstNonNilLMCallStart returns stable if set, else its deprecated alias.
func firstNonNilLMCallStart(stable, experimental ai.OnLanguageModelCallStartCallback) ai.OnLanguageModelCallStartCallback {
	if stable != nil {
		return stable
	}
	return experimental
}

// firstNonNilLMCallEnd returns stable if set, else its deprecated alias.
func firstNonNilLMCallEnd(stable, experimental ai.OnLanguageModelCallEndCallback) ai.OnLanguageModelCallEndCallback {
	if stable != nil {
		return stable
	}
	return experimental
}

func (c AgentConfig) withGenerateOptions(opts AgentGenerateOptions) AgentConfig {
	if opts.Model != nil {
		c.Model = opts.Model
	}
	if opts.System != "" {
		c.System = opts.System
	}
	if opts.Instructions != nil {
		c.Instructions = opts.Instructions
		c.System = *opts.Instructions
	}
	if opts.AllowSystemInMessages {
		c.AllowSystemInMessages = true
	}
	if opts.Prompt != "" {
		c.Prompt = opts.Prompt
	}
	if opts.RuntimeContext != nil {
		c.RuntimeContext = opts.RuntimeContext
		c.ExperimentalContext = opts.RuntimeContext
	}
	if opts.ToolsContext != nil {
		c.ToolsContext = opts.ToolsContext
	}
	if opts.CallOptions != nil {
		c.CallOptions = opts.CallOptions
	}
	if opts.Tools != nil {
		c.Tools = opts.Tools
	}
	if opts.ActiveTools != nil {
		c.ActiveTools = opts.ActiveTools
	}
	if opts.ToolOrder != nil {
		c.ToolOrder = opts.ToolOrder
	}
	if opts.ToolCallConcurrency != nil {
		c.ToolCallConcurrency = opts.ToolCallConcurrency
	}
	if opts.ExperimentalToolCallers != nil {
		c.ExperimentalToolCallers = opts.ExperimentalToolCallers
	}
	if opts.ToolChoice.Type != "" {
		c.ToolChoice = opts.ToolChoice
	}
	if len(opts.StopWhen) > 0 {
		c.StopWhen = opts.StopWhen
		c.MaxSteps = 0
	} else if opts.MaxSteps > 0 {
		c.StopWhen = []ai.StopCondition{ai.IsStepCount(opts.MaxSteps)}
		c.MaxSteps = 0
	}
	if opts.Temperature != nil {
		c.Temperature = opts.Temperature
	}
	if opts.MaxTokens != nil {
		c.MaxTokens = opts.MaxTokens
	}
	if opts.TopP != nil {
		c.TopP = opts.TopP
	}
	if opts.TopK != nil {
		c.TopK = opts.TopK
	}
	if opts.FrequencyPenalty != nil {
		c.FrequencyPenalty = opts.FrequencyPenalty
	}
	if opts.PresencePenalty != nil {
		c.PresencePenalty = opts.PresencePenalty
	}
	if opts.StopSequences != nil {
		c.StopSequences = opts.StopSequences
	}
	if opts.Seed != nil {
		c.Seed = opts.Seed
	}
	if opts.Headers != nil {
		c.Headers = opts.Headers
	}
	if opts.Reasoning != nil {
		c.Reasoning = opts.Reasoning
	}
	if opts.SendReasoning != nil {
		c.SendReasoning = opts.SendReasoning
	}
	if opts.ProviderOptions != nil {
		c.ProviderOptions = opts.ProviderOptions
	}
	if opts.Output != nil {
		c.Output = opts.Output
	}
	if opts.Telemetry != nil {
		c.Telemetry = opts.Telemetry
	}
	if opts.ExperimentalTelemetry != nil {
		c.ExperimentalTelemetry = opts.ExperimentalTelemetry
	}
	if opts.SensitiveRuntimeContext {
		c.SensitiveRuntimeContext = true
	}
	if opts.Include != nil {
		c.Include = opts.Include
	}
	if opts.Timeout != nil {
		c.Timeout = opts.Timeout
	}
	if opts.Internal != nil {
		// Internal is per-call only and flows through PrepareCall.
		c.Internal = opts.Internal
	}
	if opts.ToolApproval != nil {
		c.ToolApproval = opts.ToolApproval
	}
	if opts.ExperimentalSandbox != nil {
		c.ExperimentalSandbox = opts.ExperimentalSandbox
	}
	if opts.ExperimentalRefineToolInput != nil {
		c.ExperimentalRefineToolInput = opts.ExperimentalRefineToolInput
	}
	if opts.ExperimentalDownload != nil {
		c.ExperimentalDownload = opts.ExperimentalDownload
	}
	if len(opts.InstructionMessages) > 0 {
		c.InstructionMessages = opts.InstructionMessages
	}
	if opts.PrepareStep != nil {
		c.PrepareStep = opts.PrepareStep
	}
	if opts.RepairToolCall != nil {
		c.RepairToolCall = opts.RepairToolCall
	}
	if opts.ExperimentalRepairToolCall != nil {
		c.ExperimentalRepairToolCall = opts.ExperimentalRepairToolCall
	}
	if opts.OnLanguageModelCallStart != nil {
		c.OnLanguageModelCallStart = opts.OnLanguageModelCallStart
	}
	if opts.ExperimentalOnLanguageModelCallStart != nil {
		c.ExperimentalOnLanguageModelCallStart = opts.ExperimentalOnLanguageModelCallStart
	}
	if opts.OnLanguageModelCallEnd != nil {
		c.OnLanguageModelCallEnd = opts.OnLanguageModelCallEnd
	}
	if opts.ExperimentalOnLanguageModelCallEnd != nil {
		c.ExperimentalOnLanguageModelCallEnd = opts.ExperimentalOnLanguageModelCallEnd
	}
	return c
}

// validateAgentCallOptions validates callOptions against callOptionsSchema
// and returns the validated value. Mirrors TS ToolLoopAgent.generate/stream,
// which run per-call options through validateTypes (a zod/standard-schema
// .parse()): defaults declared on the schema are filled in as part of
// parsing itself, so a field missing a default must not fail validation,
// and the defaulted value -- not the raw one -- is what the rest of the
// call sees (TS: `options = { ...options, options: validatedOptions }`).
// Apply defaults before validating, not after.
func validateAgentCallOptions(callOptionsSchema schema.Schema, callOptions interface{}) (interface{}, error) {
	if callOptionsSchema == nil || callOptions == nil {
		return callOptions, nil
	}
	defaulted := schema.ApplyDefaults(callOptions, callOptionsSchema)
	if err := callOptionsSchema.Validator().Validate(defaulted); err != nil {
		// TS validateTypes throws TypeValidationError with context
		// {field: 'options'}. The cause chain keeps the *InvalidArgumentError
		// v0.5.2 returned, so errors.As on either type matches.
		return nil, providererrors.NewValidationErrorWithContext(callOptions, err.Error(),
			&providererrors.InvalidArgumentError{
				Field:   "options",
				Message: fmt.Sprintf("call options failed schema validation: %v", err),
				Cause:   err,
			},
			&providererrors.ValidationContext{Field: "options"})
	}
	return defaulted, nil
}

func resolveAgentStepTools(ctx context.Context, tools []types.Tool, toolsContext map[string]interface{}, sandbox interface{}) []types.Tool {
	if len(tools) == 0 {
		return tools
	}
	resolved := make([]types.Tool, len(tools))
	copy(resolved, tools)
	for i := range resolved {
		if resolved[i].DescriptionFunc != nil {
			resolved[i].Description = resolved[i].DescriptionFunc(ctx, types.ToolDescriptionOptions{
				Context:             toolsContext[resolved[i].Name],
				ExperimentalSandbox: sandbox,
			})
			resolved[i].DescriptionFunc = nil
		}
	}
	return resolved
}

// parseAgentToolCalls validates and (where configured) repairs each tool
// call from the model response, wiring RepairToolCall into the legacy
// executeWithMessages/executeStep loop (HANDOFF.md item 5). Previously this
// loop never called ai.ParseToolCall at all, so RepairToolCall had no effect
// here and calls to unknown tools or with schema-invalid input were never
// marked Invalid. Mirrors ai.parseToolCalls (unexported; pkg/agent can only
// reach the single-call ai.ParseToolCall).
func parseAgentToolCalls(ctx context.Context, calls []types.ToolCall, callConfig PrepareCallConfig, messages []types.Message, skipRepair bool) ([]types.ToolCall, error) {
	if len(calls) == 0 {
		return calls, nil
	}
	repair := callConfig.RepairToolCall
	refine := callConfig.ExperimentalRefineToolInput
	if skipRepair {
		// With tool choice none every tool call is rejected, so the user's
		// repair and refine callbacks must not process them (TS #23843).
		repair, refine = nil, nil
	}
	parsed := make([]types.ToolCall, len(calls))
	for i, call := range calls {
		if call.Invalid {
			// Already flagged invalid upstream (e.g. a provider-specific
			// quirk surfaced before this point) — trust it rather than
			// re-deriving validity, which could incorrectly clear the flag
			// for a call whose arguments happen to satisfy the schema.
			parsed[i] = call
			continue
		}
		result, err := ai.ParseToolCall(ctx, ai.ParseToolCallOptions{
			ToolCall:            call,
			Tools:               callConfig.Tools,
			RepairToolCall:      repair,
			Instructions:        callConfig.System,
			InstructionMessages: callConfig.InstructionMessages,
			Messages:            messages,
			// The refiner runs inside the parse step (TS parseToolCall), so
			// the language-model-call end event, the tool input callbacks
			// and execution all see the refined input.
			RefineToolInput: refine,
			RuntimeContext:  callConfig.RuntimeContext,
			ToolsContext:    callConfig.ToolsContext,
		})
		if err != nil {
			// Only a context cancellation reaches here; ParseToolCall
			// otherwise returns an Invalid call rather than an error.
			return nil, err
		}
		parsed[i] = result
	}
	return parsed, nil
}

func enrichAgentToolCallMetadata(calls []types.ToolCall, tools []types.Tool) []types.ToolCall {
	if len(calls) == 0 || len(tools) == 0 {
		return calls
	}
	byName := make(map[string]types.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	out := make([]types.ToolCall, len(calls))
	copy(out, calls)
	for i := range out {
		if tool, ok := byName[out[i].ToolName]; ok {
			if out[i].Title == "" {
				out[i].Title = tool.Title
			}
			if out[i].ToolMetadata == nil && tool.Metadata != nil {
				out[i].ToolMetadata = cloneAgentStringAnyMap(tool.Metadata)
			}
		}
	}
	return out
}

func cloneAgentStringAnyMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func validateAgentPromptOptions(prompt string, messages []types.Message) error {
	// TS hands the call to generateText/streamText, whose standardizePrompt
	// throws InvalidPromptError. The cause keeps the *ValidationError that
	// v0.5.2 returned, so errors.As on either type matches.
	if err := promptutils.ValidatePromptInput(prompt, messages); err != nil {
		var invalid *providererrors.InvalidPromptError
		if errors.As(err, &invalid) {
			invalid.Cause = providererrors.NewValidationErrorWithContext(
				map[string]interface{}{"prompt": prompt, "messages": messages},
				invalid.Message,
				nil,
				&providererrors.ValidationContext{Field: "prompt", EntityName: "agentCall"},
			)
		}
		return err
	}
	return nil
}

type responseFormatProvider interface {
	ResponseFormat(context.Context) (*provider.ResponseFormat, error)
}

func responseFormatForOutput(ctx context.Context, output interface{}) (*provider.ResponseFormat, error) {
	if output == nil {
		return nil, nil
	}
	if p, ok := output.(responseFormatProvider); ok {
		return p.ResponseFormat(ctx)
	}
	return nil, nil
}

// shouldParseAgentOutput mirrors TS: parse on a stop finish, or on any other
// finish except tool-calls when the final step produced text.
func shouldParseAgentOutput(finishReason types.FinishReason, text string) bool {
	if finishReason == types.FinishReasonStop {
		return true
	}
	return finishReason != types.FinishReasonToolCalls && text != ""
}

func parseAgentOutput(ctx context.Context, output interface{}, result *AgentResult) (interface{}, error) {
	if output == nil {
		return result.Text, nil
	}
	method := reflect.ValueOf(output).MethodByName("ParseCompleteOutput")
	if !method.IsValid() {
		return result.Text, nil
	}
	// TS parseCompleteOutput gets the last step's usage, not the total.
	var lastUsage types.Usage
	if n := len(result.Steps); n > 0 {
		lastUsage = result.Steps[n-1].Usage
	}
	args := []reflect.Value{
		reflect.ValueOf(ctx),
		reflect.ValueOf(ai.ParseCompleteOutputOptions{
			Text:         result.Text,
			Usage:        &lastUsage,
			FinishReason: result.FinishReason,
		}),
	}
	values := method.Call(args)
	if len(values) != 2 {
		return result.Text, nil
	}
	if !values[1].IsNil() {
		if err, ok := values[1].Interface().(error); ok {
			return nil, err
		}
	}
	return values[0].Interface(), nil
}

// stepTimeoutContext applies the per-step timeout, a PrepareCall override
// included (TS `timeout ?? preparedCall.timeout`; the config value is the
// default). The returned cancel is never nil.
func (a *ToolLoopAgent) stepTimeoutContext(ctx context.Context, callConfig PrepareCallConfig) (context.Context, context.CancelFunc) {
	stepTimeout := callConfig.Timeout
	if stepTimeout == nil {
		stepTimeout = a.config.Timeout
	}
	if stepTimeout.HasPerStep() {
		return stepTimeout.CreateTimeoutContext(ctx, "step")
	}
	return ctx, func() {}
}

// executeStep executes a single agent step
func (a *ToolLoopAgent) executeStep(ctx context.Context, callConfig PrepareCallConfig, callTools []types.Tool, stepTel *loopStepTelemetry) (*types.StepResult, bool, interface{}, []types.Tool, error) {
	// ctx carries the per-step timeout (see stepTimeoutContext); it also
	// covers the step's tool execution, as TS mergedAbortSignal does.
	stepCtx := ctx

	toolChoice := callConfig.ToolChoice
	if toolChoice.Type == "" {
		toolChoice = types.AutoToolChoice()
	}

	// TS awaits output.responseFormat inside doGenerate: a failure fails the
	// model call, after the language model call start.
	responseFormat, responseFormatErr := responseFormatForOutput(stepCtx, callConfig.Output)
	stepModel := callConfig.Model
	if stepModel == nil {
		stepModel = a.config.Model
	}

	// Instructions given as system messages take precedence over System and
	// keep each message's ProviderOptions (TS standardizePrompt: instructions
	// may be SystemModelMessage[]).
	promptSystem := callConfig.System
	if len(callConfig.InstructionMessages) > 0 {
		for _, m := range callConfig.InstructionMessages {
			if m.Role != types.RoleSystem {
				// TS standardizePrompt throws an InvalidPromptError; its Cause is
				// the *InvalidArgumentError v0.5 returned (as ai.GenerateText).
				const message = "instructions must be a string, SystemModelMessage, or array of SystemModelMessage"
				return nil, false, callConfig.CustomData, callConfig.Tools, &providererrors.InvalidPromptError{
					Message: message,
					Cause:   &providererrors.InvalidArgumentError{Field: "instructions", Message: message},
				}
			}
		}
		promptSystem = ""
	}
	// Approval parts stay out of the model's prompt, except a
	// provider-executed call's approval response (TS
	// convertToLanguageModelPrompt, as GenerateText).
	prompt, err := promptutils.NormalizePromptWithDownloadSupport(stepCtx, types.Prompt{
		Messages: aibridge.MessagesForModel(callConfig.Messages),
		System:   promptSystem,
	}, callConfig.AllowSystemInMessages, effectiveAgentDownload(callConfig.ExperimentalDownload), ai.SupportedURLCheckerForModel(stepModel))
	if err != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, err
	}
	if len(callConfig.InstructionMessages) > 0 {
		withInstructions := make([]types.Message, 0, len(callConfig.InstructionMessages)+len(prompt.Messages))
		for _, m := range callConfig.InstructionMessages {
			withInstructions = append(withInstructions, types.Message{Role: types.RoleSystem, Content: m.Content, ProviderOptions: m.ProviderOptions})
		}
		prompt.Messages = append(withInstructions, prompt.Messages...)
	}

	// Reject a prompt that leaves a tool call without a result, as
	// GenerateText does (TS convertToLanguageModelPrompt, MissingToolResultsError).
	if err := validateStepToolResults(prompt.Messages, callConfig.Tools); err != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, err
	}

	// Build generate options using potentially modified config
	genOpts := &provider.GenerateOptions{
		Prompt:                prompt,
		AllowSystemMessages:   callConfig.AllowSystemInMessages,
		AllowSystemInMessages: callConfig.AllowSystemInMessages,
		Temperature:           callConfig.Temperature,
		MaxTokens:             callConfig.MaxTokens,
		TopP:                  callConfig.TopP,
		TopK:                  callConfig.TopK,
		FrequencyPenalty:      callConfig.FrequencyPenalty,
		PresencePenalty:       callConfig.PresencePenalty,
		StopSequences:         callConfig.StopSequences,
		Tools:                 toolutil.ResolveToolSchemas(callConfig.Tools),
		ToolChoice:            toolChoice,
		RuntimeContext:        callConfig.RuntimeContext,
		ToolsContext:          callConfig.ToolsContext,
		Seed:                  callConfig.Seed,
		// The same tags Generate sends: the agent segment, then GenerateText's
		// ai/<version> (TS ToolLoopAgent always runs through generateText).
		Headers:         loopStepHeaders(callConfig.Internal, callConfig.Headers),
		Reasoning:       callConfig.Reasoning,
		SendReasoning:   callConfig.SendReasoning,
		ProviderOptions: callConfig.ProviderOptions,
		Telemetry:       callConfig.Telemetry,
		ResponseFormat:  responseFormat,
	}

	// Call the model with step context, retrying transient Gateway failures
	// per callConfig.MaxRetries (WORKFLOW-AGENT-OPTIONS, e6064c5). As TS
	// doStreamStep, prepareRetries rejects an invalid maxRetries before the
	// model call; ToolCallConcurrency was validated when the run started.
	if err := validateAgentMaxRetries(callConfig.MaxRetries); err != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, err
	}
	// TS prepareLanguageModelCallOptions follows prepareRetries (generateText).
	if err := validateAgentMaxOutputTokens(callConfig.MaxTokens); err != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, err
	}
	modelCallCtx := stepCtx
	// TS generateText takes stepStartTimestampMs here, just before
	// onLanguageModelCallStart, for both the response and the step time.
	modelCallStarted := time.Now()
	if stepTel != nil {
		stepTel.startedAt = modelCallStarted
	}
	ai.Notify(stepCtx, ai.LanguageModelCallStartEvent{
		CallID:              stepTel.callID(),
		Provider:            stepModel.Provider(),
		ModelID:             stepModel.ModelID(),
		Instructions:        callConfig.System,
		InstructionMessages: callConfig.InstructionMessages,
		Messages:            callConfig.Messages,
		Tools:               genOpts.Tools,
		MaxOutputTokens:     genOpts.MaxTokens,
		Temperature:         genOpts.Temperature,
		TopP:                genOpts.TopP,
		TopK:                genOpts.TopK,
		PresencePenalty:     genOpts.PresencePenalty,
		FrequencyPenalty:    genOpts.FrequencyPenalty,
		StopSequences:       genOpts.StopSequences,
		Seed:                genOpts.Seed,
		Reasoning:           genOpts.Reasoning,
	}, callConfig.OnLanguageModelCallStart)
	if mc := stepTel.modelCallStart(callConfig, stepModel, prompt, callConfig.Tools); mc != nil {
		// Run the provider call inside the model-call span so provider HTTP
		// spans nest under it, while the step ctx stays unchanged.
		modelCallCtx = mc
		if stepCtx != stepTel.stepCtx {
			// Keep the per-step deadline on the model-call ctx, so it expires
			// with context.DeadlineExceeded like the step ctx (a plain cancel
			// would surface as context.Canceled, an abort).
			var cancel context.CancelFunc
			if deadline, ok := stepCtx.Deadline(); ok {
				modelCallCtx, cancel = context.WithDeadline(mc, deadline)
			} else {
				modelCallCtx, cancel = context.WithCancel(mc)
			}
			defer cancel()
			stop := context.AfterFunc(stepCtx, func() {
				// The deadline itself expires modelCallCtx above.
				if !errors.Is(stepCtx.Err(), context.DeadlineExceeded) {
					cancel()
				}
			})
			defer stop()
		}
	}
	var genResult *types.GenerateResult
	err = responseFormatErr
	if err == nil {
		genResult, err = ai.DoGenerateWithGatewayRetry(modelCallCtx, stepModel, genOpts, callConfig.MaxRetries)
	}
	if err != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, err
	}

	modelCallMs := time.Since(modelCallStarted).Milliseconds()
	performance := ai.NonStreamingStepPerformance(time.Since(modelCallStarted).Milliseconds(), genResult.Usage)
	rawFinishReason := genResult.RawFinishReason

	// Create step result
	include := ai.IncludeOptions{}
	if callConfig.Include != nil {
		include = *callConfig.Include
	}
	// The repair function gets the step's messages (TS stepMessages), not the
	// converted prompt with the instruction messages prepended.
	toolCalls, err := parseAgentToolCalls(stepCtx, genResult.ToolCalls, callConfig, callConfig.Messages, genOpts.ToolChoice.Type == types.ToolChoiceNone)
	if err != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, err
	}
	toolCalls = enrichAgentToolCallMetadata(toolCalls, callConfig.Tools)
	// URL-backed generated files are downloaded once, before anything observes
	// the content (TS convertLanguageModelContent).
	if resolvedContent, resolveErr := aibridge.ResolveContentFiles(stepCtx, genResult.Content); resolveErr != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, resolveErr
	} else if len(resolvedContent) > 0 {
		resolvedResult := *genResult
		resolvedResult.Content = resolvedContent
		genResult = &resolvedResult
	}
	// A tool-result part must answer a tool call of the same response (TS
	// convertLanguageModelContent: "Tool call <id> not found."). A deferred
	// provider result is checked against the call's tools, not the step's
	// active tools (TS convertLanguageModelContent gets `tools`).
	if err := aibridge.ValidateToolResultParts(genResult.Content, toolCalls, callTools); err != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, err
	}
	if content, changed := agentDeferredToolResultMetadata(genResult.Content, toolCalls, callTools); changed {
		withMetadata := *genResult
		withMetadata.Content = content
		genResult = &withMetadata
	}
	modelCallContent := contentFromGenerateResult(genResult, toolCalls)
	// Sources, files and reasoning come from the content parts, as in
	// GenerateText (TS DefaultStepResult getters).
	var stepSources []types.SourceContent
	var stepFiles []types.GeneratedFileContent
	var stepReasoning []types.ReasoningContent
	var stepReasoningTexts []string
	for _, part := range genResult.Content {
		switch v := part.(type) {
		case types.SourceContent:
			stepSources = append(stepSources, v)
		case types.GeneratedFileContent:
			stepFiles = append(stepFiles, v)
		case types.ReasoningContent:
			stepReasoning = append(stepReasoning, v)
			if v.Text != "" {
				stepReasoningTexts = append(stepReasoningTexts, v.Text)
			}
		}
	}
	stepResponse := types.StepResponse{
		Timestamp: time.Now(),
		ModelID:   stepModel.ModelID(),
		Headers:   genResult.ResponseHeaders,
		Body:      includeValue(include.ResponseBody, genResult.RawResponse),
	}
	if meta := genResult.ResponseMetadata; meta != nil {
		stepResponse.ID = meta.ID
		if !meta.Timestamp.IsZero() {
			stepResponse.Timestamp = meta.Timestamp
		}
		if meta.ModelID != "" {
			stepResponse.ModelID = meta.ModelID
		}
		if meta.Headers != nil {
			stepResponse.Headers = meta.Headers
		}
	}
	if stepResponse.ID == "" {
		// TS `result.response?.id ?? generateId()`: the run's generator is
		// called only for a response without an ID.
		stepResponse.ID = agentGenerateID(callConfig.Internal)()
	}
	// The language-model-call end event follows the parsed response and
	// precedes client-side tool execution (TS onLanguageModelCallEnd): its
	// model id is the response model and its content includes the answer text.
	ai.Notify(stepCtx, ai.LanguageModelCallEndEvent{
		CallID:           stepTel.callID(),
		Provider:         stepModel.Provider(),
		ModelID:          stepResponse.ModelID,
		FinishReason:     genResult.FinishReason,
		Usage:            genResult.Usage,
		Content:          modelCallContent,
		ResponseID:       stepResponse.ID,
		ProviderMetadata: genResult.ProviderMetadata,
		Performance:      ai.NonStreamingModelCallPerformance(modelCallMs, genResult.Usage),
	}, callConfig.OnLanguageModelCallEnd)
	stepTel.modelCallEnd(stepModel, genResult, modelCallContent, stepResponse, modelCallMs)
	// An enforced tool choice (required, or a named tool) must produce a tool
	// call, as in GenerateText (TS ToolChoiceViolationError). TS checks the
	// parsed calls (stepToolCalls), so a call repaired to the forced tool
	// satisfies it; the error content is the raw response content.
	if violation := aibridge.CheckToolChoiceViolation(genOpts.ToolChoice, toolCalls, genResult.FinishReason, stepModel.Provider(), stepModel.ModelID(), contentFromGenerateResult(genResult, genResult.ToolCalls)); violation != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, violation
	}
	// The tools are notified of their calls (and the approvals resolved) by
	// the run loop, before any tool runs (see notifyToolCallsAndResolveApprovals).
	// TS logs each step's model warnings just before the step is recorded.
	ai.LogWarnings(ai.LogWarningsOptions{Warnings: genResult.Warnings, Provider: stepModel.Provider(), Model: stepModel.ModelID()})
	stepResult := &types.StepResult{
		StepNumber:       callConfig.StepNumber,
		Model:            types.StepModel{Provider: stepModel.Provider(), ModelID: stepModel.ModelID()},
		Text:             genResult.Text,
		Content:          contentFromGenerateResult(genResult, toolCalls),
		Reasoning:        stepReasoning,
		ReasoningText:    strings.Join(stepReasoningTexts, ""),
		Files:            stepFiles,
		Sources:          stepSources,
		ToolCalls:        toolCalls,
		StaticToolCalls:  filterAgentStaticToolCalls(toolCalls),
		DynamicToolCalls: filterAgentDynamicToolCalls(toolCalls),
		ToolResults:      []types.ToolResult{},
		FinishReason:     genResult.FinishReason,
		RawFinishReason:  rawFinishReason,
		Usage:            genResult.Usage,
		Warnings:         genResult.Warnings,
		ProviderMetadata: genResult.ProviderMetadata,
		Request: types.StepRequest{
			Body:     includeValue(include.RequestBody, genResult.RawRequest),
			Messages: includeAgentRequestMessages(include.RequestMessages, callConfig.Internal, callConfig.Messages, prompt),
		},
		Response:       stepResponse,
		ToolsContext:   callConfig.ToolsContext,
		RuntimeContext: callConfig.RuntimeContext,
		Performance:    performance,
	}

	// Determine if we should continue. TS generateText continues on the
	// presence of client tool calls whatever the finish reason (a "stop"
	// response that carries tool calls still runs them); the caller's later
	// checks stop the loop when a call is left without a result, which is
	// what happens for the finish reasons that never execute tools. The run
	// loop refines this with agentStepContinues (client calls only, plus
	// pending deferred provider results).
	shouldContinue := len(toolCalls) > 0

	return stepResult, shouldContinue, callConfig.CustomData, callConfig.Tools, nil
}

// executeTools executes a list of tool calls with optional approval
// Updated in v6.0.57 to handle provider-executed (deferrable) tools
// Updated in v6.1 (CB-T23) to fire structured OnToolCallStart/Finish events
func (a *ToolLoopAgent) executeTools(ctx context.Context, toolCalls []types.ToolCall, tools []types.Tool, messages []types.Message, stepNum int, runtimeContext interface{}, toolsContext map[string]interface{}, experimentalSandbox interface{}, toolApproval types.ToolApprovalConfig, preApprovals map[string]types.ToolApprovalResult, cbs agentCallbacks, usage types.Usage, timings *toolTimings) ([]types.ToolResult, error) {
	if toolsContext == nil {
		toolsContext = map[string]interface{}{}
	}
	runCall := func(call types.ToolCall) ([]types.ToolResult, error) {
		results := make([]types.ToolResult, 0, 1)
		// Call tool call callback
		if a.config.OnToolCall != nil {
			a.config.OnToolCall(call)
		}

		if call.Invalid {
			// Invalid provider-executed calls get no synthesized client
			// tool-error result; the provider is responsible for surfacing
			// its own error on a subsequent response (TS 7bd6bdd, mirrored
			// in ai.executeTools).
			if call.ProviderExecuted {
				return results, nil
			}
			invalidErr := call.Error
			if invalidErr == nil {
				invalidErr = fmt.Errorf("invalid tool call for %s", call.ToolName)
			}
			results = append(results, types.ToolResult{
				ToolCallID:   call.ID,
				ToolName:     call.ToolName,
				Title:        call.Title,
				Input:        call.Arguments,
				Error:        invalidErr,
				ToolMetadata: call.ToolMetadata,
			})
			if a.config.OnToolError != nil {
				a.config.OnToolError(call, invalidErr)
			}
			return results, nil
		}

		// Find the tool
		var tool *types.Tool
		for j := range tools {
			if tools[j].Name == call.ToolName {
				tool = &tools[j]
				break
			}
		}

		if tool == nil {
			notFoundErr := fmt.Errorf("tool not found: %s", call.ToolName)
			results = append(results, types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Title:            call.Title,
				Input:            call.Arguments,
				Error:            notFoundErr,
				ProviderExecuted: false,
				ToolMetadata:     call.ToolMetadata,
			})

			// Call OnToolError for tool not found
			if a.config.OnToolError != nil {
				a.config.OnToolError(call, notFoundErr)
			}
			return results, nil
		}

		// A call the provider marks as executed is never run locally, even
		// when a local tool has its name (TS: client tool calls are the calls
		// without providerExecuted).
		providerExecuted := tool.ProviderExecuted || call.ProviderExecuted
		providerMetadata := mergeProviderMetadataMaps(call.ProviderMetadata, tool.ProviderMetadata)

		approval, resolved := preApprovals[call.ID]
		var approvalErr error
		if !resolved {
			approval, approvalErr = a.resolveToolApproval(ctx, call, tools, messages, runtimeContext, toolsContext, toolApproval)
		}
		if approvalErr != nil {
			// TS generateText: a tool approval that cannot be resolved
			// throws and fails the run (no tool-error result).
			return nil, approvalErr
		}
		approverDenied := false
		if a.config.ToolApprovalRequired && a.config.ToolApprover != nil && approval.Status == types.ToolApprovalStatusNotApplicable {
			if !a.config.ToolApprover(call) {
				approval = types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied}
				approverDenied = true
			}
		}
		switch approval.Status {
		case types.ToolApprovalStatusDenied:
			// A ToolApproval denial without a reason leaves it unset, as
			// GenerateText and TS do, so each provider converter applies its
			// own default denial text. The Go-only ToolApprover names its
			// policy.
			policyText := fmt.Sprintf("Tool call to %s was denied by ToolApproval policy.", call.ToolName)
			reason := approval.Reason
			if reason == nil && approverDenied {
				reason = strPtr(policyText)
			}
			reasonText := ""
			if reason != nil {
				reasonText = *reason
			}
			errText := reasonText
			if errText == "" {
				errText = policyText
			}
			rejectionErr := errors.New(errText)
			results = append(results, types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Title:            call.Title,
				Input:            call.Arguments,
				Error:            rejectionErr,
				Result:           types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: reasonText},
				ApprovalStatus:   types.ToolApprovalStatusDenied,
				ApprovalReason:   reason,
				ProviderExecuted: providerExecuted,
				// TS approval parts carry toolCall.providerExecuted.
				ProviderExecutedSet: call.ProviderExecutedSet,
				ProviderMetadata:    providerMetadata,
				ToolMetadata:        call.ToolMetadata,
			})
			if a.config.OnToolError != nil {
				a.config.OnToolError(call, rejectionErr)
			}
			return results, nil
		case types.ToolApprovalStatusUserApproval:
			results = append(results, types.ToolResult{
				ToolCallID:          call.ID,
				ToolName:            call.ToolName,
				Title:               call.Title,
				Input:               call.Arguments,
				Result:              map[string]interface{}{"type": "tool-approval-request", "approvalId": call.ID, "toolCall": call},
				ApprovalStatus:      types.ToolApprovalStatusUserApproval,
				ProviderExecuted:    providerExecuted,
				ProviderExecutedSet: call.ProviderExecutedSet,
				ProviderMetadata:    providerMetadata,
				ToolMetadata:        call.ToolMetadata,
			})
			return results, nil
		}

		if providerExecuted {
			// Provider-executed tool: result will come from provider in next response
			// Call OnToolStart for provider-executed tools
			if a.config.OnToolStart != nil {
				a.config.OnToolStart(call)
			}

			result := types.ToolResult{
				ToolCallID:          call.ID,
				ToolName:            call.ToolName,
				Title:               call.Title,
				Input:               call.Arguments,
				Result:              nil,
				Error:               nil,
				ApprovalStatus:      approval.Status,
				ApprovalReason:      approval.Reason,
				ProviderExecuted:    true,
				ProviderExecutedSet: true,
				ProviderMetadata:    providerMetadata,
				ToolMetadata:        call.ToolMetadata,
			}
			results = append(results, result)

			// Call tool result callback with pending result
			if a.config.OnToolResult != nil {
				a.config.OnToolResult(result)
			}

			// Call OnToolEnd for provider-executed tools (they're deferred but considered started)
			if a.config.OnToolEnd != nil {
				a.config.OnToolEnd(result)
			}
		} else {
			if tool.Execute == nil {
				return results, nil
			}

			toolContext, err := validateAgentToolContext(tool, call.ToolName, toolsContext[call.ToolName])
			if err != nil {
				// TS executeToolCall validates the tool context outside its
				// try: a failure fails the run, not a tool-error result.
				return nil, err
			}

			// Locally-executed tool: execute now
			// Call OnToolStart before execution (legacy)
			if a.config.OnToolStart != nil {
				a.config.OnToolStart(call)
			}

			// CB-T23: Emit OnToolCallStartEvent
			eventCallID := ""
			if lt, ok := ctx.Value(loopTelemetryKey{}).(*loopTelemetry); ok && lt != nil {
				eventCallID = lt.callID
			}
			ai.Notify(ctx, ai.OnToolCallStartEvent{
				CallID:      eventCallID,
				ToolContext: toolContext,
				Messages:    messages,
				ToolCallID:  call.ID,
				ToolName:    call.ToolName,
				ToolCall:    call,
				// Args/StepNumber/ModelProvider are deprecated, JSON-excluded
				// aliases kept for callbacks still reading the old fields
				// (mirrors generate.go's OnToolCallStartEvent population);
				// ToolCall.Arguments is the replacement for Args above.
				Args:                call.Arguments,            //nolint:staticcheck
				StepNumber:          stepNum,                   //nolint:staticcheck
				ModelProvider:       a.config.Model.Provider(), //nolint:staticcheck
				ModelID:             a.config.Model.ModelID(),
				ExperimentalContext: a.config.ExperimentalContext,
				RuntimeContext:      runtimeContext,
				ToolsContext:        toolsContext,
			}, cbs.onToolCallStart)

			execUsage := usage // per-call copy: calls may run concurrently
			execOptions := types.ToolExecutionOptions{
				ToolCallID:          call.ID,
				UserContext:         runtimeContext,
				RuntimeContext:      runtimeContext,
				ToolContext:         toolContext,
				ToolMetadata:        call.ToolMetadata,
				Messages:            messages,
				Usage:               &execUsage,
				Metadata:            make(map[string]interface{}),
				ExperimentalSandbox: experimentalSandbox,
			}
			// Dispatch tool-call telemetry: integrations open a child span of
			// the step span in toolCtx and wrap the execution (loop_telemetry.go).
			var tsettings *ai.TelemetrySettings
			tcallID := ""
			if lt, ok := ctx.Value(loopTelemetryKey{}).(*loopTelemetry); ok && lt != nil {
				tsettings, tcallID = lt.settings, lt.callID
			}
			toolTelCtx := telemetry.FireOnToolCallStart(ctx, telemetry.TelemetryToolCallStartEvent{
				Settings:    tsettings,
				CallID:      tcallID,
				ToolCallID:  call.ID,
				ToolName:    call.ToolName,
				Args:        call.Arguments,
				ToolContext: telemetryToolContextFor(tsettings, call.ToolName, toolContext),
			})
			// Per-tool timeout (TS timeout.toolMs / tools[name]).
			execCtx := toolTelCtx
			execCancel := func() {}
			toolTimeout := a.config.Timeout.GetToolTimeout(call.ToolName)
			if toolTimeout != nil {
				execCtx, execCancel = context.WithTimeout(toolTelCtx, *toolTimeout)
			}
			toolStarted := time.Now()
			toolResult, toolErr := telemetry.FireExecuteToolWithSettings(execCtx, tsettings, call.ToolName, call.Arguments,
				func(execCtx context.Context, args map[string]interface{}) (interface{}, error) {
					return tool.Execute(execCtx, args, execOptions)
				})
			durationMs := time.Since(toolStarted).Milliseconds()
			// Only the tool timeout's own deadline counts (see ai.executeToolsConfigured).
			if toolTimeout != nil && errors.Is(execCtx.Err(), context.DeadlineExceeded) {
				if toolErr == nil {
					toolErr = context.DeadlineExceeded
				}
				toolErr = &ai.TimeoutError{Reason: ai.TimeoutReasonTool, Err: toolErr}
			}
			execCancel()
			timings.record(call.ID, durationMs)
			telemetry.FireOnToolCallFinish(toolTelCtx, telemetry.TelemetryToolCallFinishEvent{
				Settings:    tsettings,
				CallID:      tcallID,
				ToolCallID:  call.ID,
				ToolName:    call.ToolName,
				Args:        call.Arguments,
				Result:      toolResult,
				Error:       toolErr,
				DurationMs:  durationMs,
				ToolContext: telemetryToolContextFor(tsettings, call.ToolName, toolContext),
			})
			var modelOutput *types.ToolResultOutput
			callUsage := usage // per-call copy: calls may run concurrently
			if toolErr == nil && tool.ToModelOutput != nil {
				converted, convertErr := tool.ToModelOutput(ctx, types.ToModelOutputOptions{
					ToolCallID: call.ID,
					Input:      call.Arguments,
					Output:     toolResult,
					// Result is a deprecated alias of Output, kept for
					// ToModelOutput implementations still reading it.
					Result: toolResult, //nolint:staticcheck
					ToolCall: &types.ToolCall{
						ID:                  call.ID,
						ToolName:            call.ToolName,
						Title:               call.Title,
						Arguments:           call.Arguments,
						ProviderExecuted:    call.ProviderExecuted,
						ProviderExecutedSet: call.ProviderExecutedSet,
						ProviderMetadata:    call.ProviderMetadata,
						ToolMetadata:        call.ToolMetadata,
						Dynamic:             call.Dynamic,
					},
					Usage: &callUsage,
				})
				if convertErr != nil {
					return results, &toolResultModelOutputError{err: convertErr}
				}
				modelOutput = converted
			}

			result := types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Title:            call.Title,
				Input:            call.Arguments,
				Result:           toolResult,
				ModelOutput:      modelOutput,
				Error:            toolErr,
				ProviderExecuted: false,
				ProviderMetadata: providerMetadata,
				ToolMetadata:     call.ToolMetadata,
			}
			if approval.Status == types.ToolApprovalStatusApproved {
				// An automatically approved call keeps its approval so its
				// request/response parts are recorded, as in GenerateText.
				result.ApprovalStatus = approval.Status
				result.ApprovalReason = approval.Reason
				result.ProviderExecutedSet = call.ProviderExecutedSet
			}
			results = append(results, result)

			// CB-T23: Emit OnToolCallFinishEvent
			ai.Notify(ctx, ai.OnToolCallFinishEvent{
				CallID:          eventCallID,
				ToolContext:     toolContext,
				Messages:        messages,
				ToolCallID:      call.ID,
				ToolName:        call.ToolName,
				ToolCall:        call,
				ToolOutput:      result,
				ToolExecutionMs: durationMs,
				// Args/Result/Error/DurationMs/StepNumber/ModelProvider are
				// deprecated, JSON-excluded aliases kept for callbacks still
				// reading the old fields (mirrors generate.go's
				// OnToolCallFinishEvent population); ToolCall/ToolOutput/
				// ToolExecutionMs above are the replacements.
				Args:                call.Arguments,            //nolint:staticcheck
				Result:              toolResult,                //nolint:staticcheck
				Error:               toolErr,                   //nolint:staticcheck
				DurationMs:          durationMs,                //nolint:staticcheck
				StepNumber:          stepNum,                   //nolint:staticcheck
				ModelProvider:       a.config.Model.Provider(), //nolint:staticcheck
				ModelID:             a.config.Model.ModelID(),
				ExperimentalContext: a.config.ExperimentalContext,
				RuntimeContext:      runtimeContext,
				ToolsContext:        toolsContext,
			}, cbs.onToolCallFinish)

			// Call tool result callback (legacy)
			if a.config.OnToolResult != nil {
				a.config.OnToolResult(result)
			}

			// Call OnToolEnd or OnToolError based on execution result (legacy)
			if toolErr != nil {
				if a.config.OnToolError != nil {
					a.config.OnToolError(call, toolErr)
				}
			} else {
				if a.config.OnToolEnd != nil {
					a.config.OnToolEnd(result)
				}
			}
		}
		return results, nil
	}
	results, err := a.runToolCalls(ctx, toolCalls, runCall)
	aibridge.MarkDynamicToolResults(results, toolCalls)
	return results, err
}

func (a *ToolLoopAgent) runtimeContext() interface{} {
	if a.config.RuntimeContext != nil {
		return a.config.RuntimeContext
	}
	return a.config.ExperimentalContext
}

func effectiveAgentDownload(download ai.DownloadFunction) ai.DownloadFunction {
	if download != nil {
		return download
	}
	return ai.DefaultDownload
}

func includeValue(include bool, value interface{}) interface{} {
	if !include {
		return nil
	}
	return value
}

// includeAgentRequestMessages is a step's request messages when
// Include.RequestMessages is set: the step's messages as given to the model
// call, before prompt conversion and without the instructions (TS
// generateText cloneModelMessages(stepMessages)). With
// InternalOptions.RequestMessagesAsPrompt (WorkflowAgent) it is the model
// call's prompt, its system message first (TS WorkflowAgent
// conversationPrompt).
func includeAgentRequestMessages(include bool, internal *ai.InternalOptions, messages []types.Message, prompt types.Prompt) []types.Message {
	if !include {
		return nil
	}
	if internal != nil && internal.RequestMessagesAsPrompt {
		out := make([]types.Message, 0, len(prompt.Messages)+1)
		if prompt.System != "" {
			out = append(out, types.Message{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: prompt.System}}})
		}
		return append(out, prompt.Messages...)
	}
	return append([]types.Message(nil), messages...)
}

func contentFromGenerateResult(result *types.GenerateResult, toolCalls []types.ToolCall) []types.ContentPart {
	if result == nil {
		return nil
	}
	parts := append([]types.ContentPart(nil), result.Content...)
	if result.Text != "" && !agentContentHasText(parts) {
		parts = types.InsertLeadText(parts, result.Text)
	}
	seenToolCalls := map[string]bool{}
	for i, part := range parts {
		switch p := part.(type) {
		case types.ToolResultContent:
			// TS convertLanguageModelContent: every tool-result or
			// tool-error the model returns is provider-executed.
			p.ProviderExecuted = true
			parts[i] = p
		case *types.ToolResultContent:
			if p != nil {
				c := *p
				c.ProviderExecuted = true
				parts[i] = c
			}
		case types.ToolErrorContent:
			p.ProviderExecuted = true
			parts[i] = p
		case *types.ToolErrorContent:
			if p != nil {
				c := *p
				c.ProviderExecuted = true
				parts[i] = c
			}
		case types.ToolCallContent:
			seenToolCalls[p.ToolCallID] = true
			if call, ok := agentToolCallByID(toolCalls, p.ToolCallID); ok {
				parts[i] = agentContentPartFromToolCall(call)
			}
		case *types.ToolCallContent:
			if p != nil {
				seenToolCalls[p.ToolCallID] = true
				if call, ok := agentToolCallByID(toolCalls, p.ToolCallID); ok {
					parts[i] = agentContentPartFromToolCall(call)
				}
			}
		case types.ToolApprovalRequestContent:
			parts[i] = agentApprovalRequestWithToolCall(p, toolCalls)
		case *types.ToolApprovalRequestContent:
			if p != nil {
				parts[i] = agentApprovalRequestWithToolCall(*p, toolCalls)
			}
		}
	}
	for _, call := range toolCalls {
		if seenToolCalls[call.ID] {
			continue
		}
		parts = append(parts, agentContentPartFromToolCall(call))
	}
	parts = replaceAgentToolResultContentParts(parts, toolCalls)
	return parts
}

// agentApprovalRequestWithToolCall gives a provider tool-approval-request part
// (an MCP server asking for approval of its call) the parsed call it belongs
// to, as TS convertLanguageModelContent does ({type, approvalId, toolCall}).
func agentApprovalRequestWithToolCall(p types.ToolApprovalRequestContent, calls []types.ToolCall) types.ToolApprovalRequestContent {
	if p.ToolCallID == "" {
		p.ToolCallID = p.ToolCall.ID
	}
	if call, ok := agentToolCallByID(calls, p.ToolCallID); ok {
		p.ToolCall = call
	}
	return p
}

// agentDeferredToolResultMetadata gives a deferred provider tool result (a
// tool-result or tool-error part that answers no tool call of the response)
// its tool definition's metadata (TS convertLanguageModelContent
// `toolMetadata: tool.metadata`). content is not modified.
func agentDeferredToolResultMetadata(content []types.ContentPart, calls []types.ToolCall, tools []types.Tool) (_ []types.ContentPart, changed bool) {
	metadataFor := func(id, name string, current map[string]interface{}) map[string]interface{} {
		if current != nil {
			return nil
		}
		if _, ok := agentToolCallByID(calls, id); ok {
			return nil
		}
		for i := range tools {
			if tools[i].Name == name {
				return tools[i].Metadata
			}
		}
		return nil
	}
	var out []types.ContentPart
	for i, part := range content {
		var updated types.ContentPart
		switch p := part.(type) {
		case types.ToolResultContent:
			if md := metadataFor(p.ToolCallID, p.ToolName, p.ToolMetadata); md != nil {
				p.ToolMetadata = md
				updated = p
			}
		case *types.ToolResultContent:
			if p != nil {
				if md := metadataFor(p.ToolCallID, p.ToolName, p.ToolMetadata); md != nil {
					c := *p
					c.ToolMetadata = md
					updated = c
				}
			}
		case types.ToolErrorContent:
			if md := metadataFor(p.ToolCallID, p.ToolName, p.ToolMetadata); md != nil {
				p.ToolMetadata = md
				updated = p
			}
		case *types.ToolErrorContent:
			if p != nil {
				if md := metadataFor(p.ToolCallID, p.ToolName, p.ToolMetadata); md != nil {
					c := *p
					c.ToolMetadata = md
					updated = c
				}
			}
		}
		if updated == nil {
			continue
		}
		if out == nil {
			out = append([]types.ContentPart(nil), content...)
		}
		out[i] = updated
	}
	if out == nil {
		return content, false
	}
	return out, true
}

func agentToolCallByID(calls []types.ToolCall, id string) (types.ToolCall, bool) {
	for _, call := range calls {
		if call.ID == id {
			return call, true
		}
	}
	return types.ToolCall{}, false
}

func agentContentPartFromToolCall(call types.ToolCall) types.ToolCallContent {
	return types.ToolCallContent{
		ToolCallID:       call.ID,
		ToolName:         call.ToolName,
		Title:            call.Title,
		Input:            call.RawArguments,
		Arguments:        call.Arguments,
		ProviderExecuted: call.ProviderExecuted,
		ProviderMetadata: agentProviderMetadataRaw(call.ProviderMetadata),
		ToolMetadata:     call.ToolMetadata,
		Dynamic:          call.Dynamic,
		Invalid:          call.Invalid,
		Error:            agentToolCallContentError(call.Error),
		ThoughtSignature: call.ThoughtSignature,
	}
}

func agentToolCallContentError(err error) interface{} {
	if err == nil {
		return nil
	}
	return err.Error()
}

func replaceAgentToolResultContentParts(parts []types.ContentPart, calls []types.ToolCall) []types.ContentPart {
	if len(parts) == 0 || len(calls) == 0 {
		return parts
	}
	for i, part := range parts {
		switch p := part.(type) {
		case types.ToolResultContent:
			if call, ok := agentToolCallByID(calls, p.ToolCallID); ok {
				parts[i] = enrichAgentToolResultContent(p, call)
			}
		case *types.ToolResultContent:
			if p != nil {
				if call, ok := agentToolCallByID(calls, p.ToolCallID); ok {
					parts[i] = enrichAgentToolResultContent(*p, call)
				}
			}
		case types.ToolErrorContent:
			if call, ok := agentToolCallByID(calls, p.ToolCallID); ok {
				parts[i] = enrichAgentToolErrorContent(p, call)
			}
		case *types.ToolErrorContent:
			if p != nil {
				if call, ok := agentToolCallByID(calls, p.ToolCallID); ok {
					parts[i] = enrichAgentToolErrorContent(*p, call)
				}
			}
		}
	}
	return parts
}

func enrichAgentToolResultContent(part types.ToolResultContent, call types.ToolCall) types.ToolResultContent {
	if part.Input == nil {
		part.Input = call.Arguments
	}
	if part.ToolMetadata == nil {
		part.ToolMetadata = call.ToolMetadata
	}
	part.Dynamic = call.Dynamic
	return part
}

func enrichAgentToolErrorContent(part types.ToolErrorContent, call types.ToolCall) types.ToolErrorContent {
	if part.Input == nil {
		part.Input = call.Arguments
	}
	if part.ToolMetadata == nil {
		part.ToolMetadata = call.ToolMetadata
	}
	part.Dynamic = call.Dynamic
	return part
}

func agentProviderMetadataRaw(metadata map[string]interface{}) json.RawMessage {
	if len(metadata) == 0 {
		return nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil
	}
	return raw
}

func agentContentHasText(parts []types.ContentPart) bool {
	for _, part := range parts {
		switch p := part.(type) {
		case types.TextContent:
			if p.Text != "" {
				return true
			}
		case *types.TextContent:
			if p != nil && p.Text != "" {
				return true
			}
		}
	}
	return false
}

// assignAgentApprovalIDs gives the approvals of a step's tool calls their IDs,
// in call order: InternalOptions.ApprovalID when it is set (TS WorkflowAgent:
// `approval-<toolCallId>`), else one ID per approval from the run's ID
// generator, as TS ToolLoopAgent's generateText calls generateId() for each
// call whose approval applies. Tool call IDs can repeat across steps with some
// providers, so they do not name approvals.
func assignAgentApprovalIDs(results []types.ToolResult, internal *ai.InternalOptions) {
	approvalID := func(string) string { return agentGenerateID(internal)() }
	if internal != nil && internal.ApprovalID != nil {
		approvalID = internal.ApprovalID
	}
	for i := range results {
		switch results[i].ApprovalStatus {
		case types.ToolApprovalStatusUserApproval, types.ToolApprovalStatusApproved, types.ToolApprovalStatusDenied:
		default:
			continue
		}
		id := approvalID(results[i].ToolCallID)
		results[i].ApprovalID = id
		if request, ok := results[i].Result.(map[string]interface{}); ok && request["type"] == "tool-approval-request" {
			copied := make(map[string]interface{}, len(request))
			for k, v := range request {
				copied[k] = v
			}
			copied["approvalId"] = id
			results[i].Result = copied
		}
	}
}

// agentApprovedUnexecuted is the Result of an entry that carries only the
// automatic approval of a local call that did not run (its tool has no
// Execute, or the step's finish reason does not allow execution): TS records
// the call's approval request and response, but no tool output.
type agentApprovedUnexecuted struct{}

// agentApprovedUnexecutedCalls returns, by call ID, an approval-only entry for
// each valid local call whose approval was approved but that got no result.
func agentApprovedUnexecutedCalls(calls []types.ToolCall, tools []types.Tool, approvals map[string]types.ToolApprovalResult, results []types.ToolResult) map[string]types.ToolResult {
	answered := make(map[string]bool, len(results))
	for _, r := range results {
		answered[r.ToolCallID] = true
	}
	var out map[string]types.ToolResult
	for _, call := range calls {
		approval, ok := approvals[call.ID]
		if !ok || call.Invalid || answered[call.ID] || approval.Status != types.ToolApprovalStatusApproved {
			continue
		}
		tool := findAgentTool(call.ToolName, tools)
		if tool == nil || tool.ProviderExecuted || call.ProviderExecuted {
			continue
		}
		if out == nil {
			out = map[string]types.ToolResult{}
		}
		out[call.ID] = types.ToolResult{
			ToolCallID:          call.ID,
			ToolName:            call.ToolName,
			Title:               call.Title,
			Input:               call.Arguments,
			Result:              agentApprovedUnexecuted{},
			ApprovalStatus:      types.ToolApprovalStatusApproved,
			ApprovalReason:      approval.Reason,
			ProviderExecutedSet: call.ProviderExecutedSet,
			ProviderMetadata:    mergeProviderMetadataMaps(call.ProviderMetadata, tool.ProviderMetadata),
			ToolMetadata:        call.ToolMetadata,
			Dynamic:             call.Dynamic,
		}
	}
	return out
}

// mergeAgentApprovalOnly inserts the approval-only entries into results (in
// call order) at their calls' positions. results is returned as is when
// there are none.
func mergeAgentApprovalOnly(results []types.ToolResult, calls []types.ToolCall, approvalOnly map[string]types.ToolResult) []types.ToolResult {
	if len(approvalOnly) == 0 {
		return results
	}
	out := make([]types.ToolResult, 0, len(results)+len(approvalOnly))
	j := 0
	for _, call := range calls {
		start := j
		for j < len(results) && results[j].ToolCallID == call.ID {
			j++
		}
		if j > start {
			out = append(out, results[start:j]...)
			continue
		}
		if entry, ok := approvalOnly[call.ID]; ok {
			out = append(out, entry)
		}
	}
	return append(out, results[j:]...)
}

// splitAgentApprovalOnly names the approvals of merged (assignAgentApprovalIDs)
// and splits it back into the tool results and the approval-only entries.
func splitAgentApprovalOnly(merged []types.ToolResult, internal *ai.InternalOptions) ([]types.ToolResult, map[string]types.ToolResult) {
	assignAgentApprovalIDs(merged, internal)
	var approvalOnly map[string]types.ToolResult
	results := merged[:0:0]
	for _, r := range merged {
		if _, ok := r.Result.(agentApprovedUnexecuted); ok {
			if approvalOnly == nil {
				approvalOnly = map[string]types.ToolResult{}
			}
			approvalOnly[r.ToolCallID] = r
			continue
		}
		results = append(results, r)
	}
	if approvalOnly == nil {
		return merged, nil
	}
	return results, approvalOnly
}

// agentGenerateID is the run's ID generator: InternalOptions.GenerateID (TS
// _internal.generateId), else TS generateText's default (`aitxt-` plus 24
// characters).
func agentGenerateID(internal *ai.InternalOptions) ai.IDGenerator {
	if internal != nil && internal.GenerateID != nil {
		return internal.GenerateID
	}
	return newTextResponseID
}

// agentApprovalID is a result's approval ID: the assigned one, else the
// call's ID.
func agentApprovalID(result types.ToolResult) string {
	if result.ApprovalID != "" {
		return result.ApprovalID
	}
	return result.ToolCallID
}

// agentToolResultsToContentParts converts tool results to content parts. When
// secret is non-nil, approval requests are signed so they can be verified on
// resume. The parts are ordered as TS convertLanguageModelContent (and
// GenerateText) orders them: outputs without an approval response, the
// approval requests, the approval responses, then the outputs with one.
func agentToolResultsToContentParts(results []types.ToolResult, secret []byte) []types.ContentPart {
	if len(results) == 0 {
		return nil
	}
	var withoutApproval, requests, responses, withApproval []types.ContentPart
	for _, result := range results {
		parts := &withoutApproval
		if result.ApprovalStatus == types.ToolApprovalStatusApproved || result.ApprovalStatus == types.ToolApprovalStatusDenied {
			parts = &withApproval
		}
		hasApproval := result.ApprovalStatus == types.ToolApprovalStatusUserApproval ||
			result.ApprovalStatus == types.ToolApprovalStatusApproved ||
			result.ApprovalStatus == types.ToolApprovalStatusDenied
		// A provider-executed call without an approval is the pending marker
		// (its result, if any, is the provider's own content part), whatever
		// its approval status ("" or not-applicable): it adds no content, as
		// in GenerateText. Only the synthetic WorkflowAgent empty result
		// (ContinueOnMissingProviderResult) carries an output.
		if result.ProviderExecuted && !hasApproval && result.Result == nil && result.Error == nil && result.ModelOutput == nil {
			continue
		}
		if hasApproval {
			toolCall := types.ToolCall{
				ID:                  result.ToolCallID,
				ToolName:            result.ToolName,
				Title:               result.Title,
				Arguments:           result.Input,
				ProviderExecuted:    result.ProviderExecuted,
				ProviderExecutedSet: result.ProviderExecutedSet,
				ProviderMetadata:    result.ProviderMetadata,
				ToolMetadata:        result.ToolMetadata,
				Dynamic:             result.Dynamic,
			}
			signature := ""
			if secret != nil {
				signature, _ = ai.SignToolApproval(secret, agentApprovalID(result), result.ToolCallID, result.ToolName, result.Input)
			}
			approvalRequest := types.ToolApprovalRequestContent{
				ApprovalID:  agentApprovalID(result),
				ToolCallID:  result.ToolCallID,
				ToolCall:    toolCall,
				Signature:   signature,
				IsAutomatic: result.ApprovalStatus == types.ToolApprovalStatusApproved || result.ApprovalStatus == types.ToolApprovalStatusDenied,
			}
			// User-approval reasons are shown on the request (approved/denied
			// reasons are emitted on the response instead), matching
			// toolApprovalRequestFromToolResult (pkg/ai/content_parts.go).
			if result.ApprovalStatus == types.ToolApprovalStatusUserApproval && result.ApprovalReason != nil {
				approvalRequest.Reason = *result.ApprovalReason
			}
			requests = append(requests, approvalRequest)
			if result.ApprovalStatus == types.ToolApprovalStatusUserApproval {
				continue
			}
			reason := ""
			if result.ApprovalReason != nil {
				reason = *result.ApprovalReason
			}
			responses = append(responses, types.ToolApprovalResponseContent{
				ApprovalID:       agentApprovalID(result),
				ToolCallID:       result.ToolCallID,
				ToolCall:         toolCall,
				Approved:         result.ApprovalStatus == types.ToolApprovalStatusApproved,
				Reason:           reason,
				ProviderExecuted: result.ProviderExecuted,
				// TS: providerExecuted: toolCall.providerExecuted.
				ProviderExecutedSet: result.ProviderExecutedSet,
			})
			if result.ProviderExecuted || result.ApprovalStatus == types.ToolApprovalStatusDenied {
				continue
			}
			if _, approvalOnly := result.Result.(agentApprovedUnexecuted); approvalOnly {
				continue
			}
		}
		if result.Error != nil {
			*parts = append(*parts, types.ToolErrorContent{
				ToolCallID:       result.ToolCallID,
				ToolName:         result.ToolName,
				Input:            result.Input,
				Error:            result.Error.Error(),
				ErrorText:        providerutils.ToolErrorText(result.Error),
				ProviderExecuted: result.ProviderExecuted,
				ProviderMetadata: agentProviderMetadataRaw(result.ProviderMetadata),
				ToolMetadata:     result.ToolMetadata,
				Dynamic:          result.Dynamic,
			})
			continue
		}
		part := types.ToolResultContent{
			ToolCallID:       result.ToolCallID,
			ToolName:         result.ToolName,
			Input:            result.Input,
			Result:           result.Result,
			ProviderExecuted: result.ProviderExecuted,
			ProviderMetadata: agentProviderMetadataRaw(result.ProviderMetadata),
			ToolMetadata:     result.ToolMetadata,
			Dynamic:          result.Dynamic,
			Preliminary:      result.Preliminary,
		}
		switch output := modelFacingToolResultOutput(result); output.Type {
		case "":
		case types.ToolResultOutputText, types.ToolResultOutputJSON, types.ToolResultOutputContent, types.ToolResultOutputError, types.ToolResultOutputErrorText, types.ToolResultOutputErrorJSON, types.ToolResultOutputExecutionDenied:
			part.Output = &output
			part.Result = nil
		}
		if part.Output == nil {
			switch output := result.Result.(type) {
			case types.ToolResultOutput:
				part.Output = &output
				part.Result = nil
			case *types.ToolResultOutput:
				part.Output = output
				part.Result = nil
			case string:
				part.Output = &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: output}
				part.Result = nil
			default:
				part.Output = &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: result.Result}
				part.Result = nil
			}
		}
		*parts = append(*parts, part)
	}
	out := make([]types.ContentPart, 0, len(withoutApproval)+len(requests)+len(responses)+len(withApproval))
	out = append(out, withoutApproval...)
	out = append(out, requests...)
	out = append(out, responses...)
	return append(out, withApproval...)
}

func modelFacingToolResultOutput(result types.ToolResult) types.ToolResultOutput {
	if result.ModelOutput == nil {
		return types.ToolResultOutput{}
	}
	return *result.ModelOutput
}

func filterAgentStaticToolCalls(calls []types.ToolCall) []types.ToolCall {
	out := make([]types.ToolCall, 0, len(calls))
	for _, call := range calls {
		if !call.Dynamic {
			out = append(out, call)
		}
	}
	return out
}

func filterAgentDynamicToolCalls(calls []types.ToolCall) []types.ToolCall {
	out := make([]types.ToolCall, 0, len(calls))
	for _, call := range calls {
		if call.Dynamic {
			out = append(out, call)
		}
	}
	return out
}

func filterAgentStaticToolResults(results []types.ToolResult) []types.ToolResult {
	out := make([]types.ToolResult, 0, len(results))
	for _, result := range results {
		if !result.Dynamic {
			out = append(out, result)
		}
	}
	return out
}

func filterAgentDynamicToolResults(results []types.ToolResult) []types.ToolResult {
	out := make([]types.ToolResult, 0, len(results))
	for _, result := range results {
		if result.Dynamic {
			out = append(out, result)
		}
	}
	return out
}

// notifyToolCallsAndResolveApprovals runs, for every valid call with a known
// tool and in call order, the tool's OnInputStart and OnInputAvailable and
// then (when resolve is set) the call's approval, as TS generateText's
// tool-call notification loop does before any tool executes. inputErr is a
// failed input callback (or tool context), approvalErr an approval that
// could not be resolved.
func (a *ToolLoopAgent) notifyToolCallsAndResolveApprovals(ctx context.Context, calls []types.ToolCall, tools []types.Tool, messages []types.Message, runtimeContext interface{}, toolsContext map[string]interface{}, toolApproval types.ToolApprovalConfig, resolve bool) (approvals map[string]types.ToolApprovalResult, inputErr, approvalErr error) {
	if toolsContext == nil {
		toolsContext = map[string]interface{}{}
	}
	approvals = map[string]types.ToolApprovalResult{}
	for _, call := range calls {
		if call.Invalid || findAgentTool(call.ToolName, tools) == nil {
			continue
		}
		if err := aibridge.InvokeToolInputCallbacks(ctx, []types.ToolCall{call}, tools, messages, toolsContext); err != nil {
			return nil, err, nil
		}
		if !resolve {
			continue
		}
		approval, err := a.resolveToolApproval(ctx, call, tools, messages, runtimeContext, toolsContext, toolApproval)
		if err != nil {
			return nil, nil, err
		}
		approvals[call.ID] = approval
	}
	return approvals, nil, nil
}

func (a *ToolLoopAgent) resolveToolApproval(ctx context.Context, call types.ToolCall, tools []types.Tool, messages []types.Message, runtimeContext interface{}, toolsContext map[string]interface{}, toolApproval types.ToolApprovalConfig) (types.ToolApprovalResult, error) {
	// Shared with ai.GenerateText so the loop fails closed the same way: an
	// unknown status or unsupported configuration is an error, not a pass.
	return aibridge.ResolveToolApproval(ctx, call, tools, messages, runtimeContext, toolsContext, toolApproval)
}

func findAgentTool(toolName string, tools []types.Tool) *types.Tool {
	for i := range tools {
		if tools[i].Name == toolName {
			return &tools[i]
		}
	}
	return nil
}

func validateAgentToolContext(tool *types.Tool, toolName string, ctxValue interface{}) (interface{}, error) {
	return aibridge.ValidateToolContext(tool, toolName, ctxValue)
}

func strPtr(s string) *string {
	return &s
}

func mergeProviderMetadataMaps(primary, fallback map[string]interface{}) map[string]interface{} {
	if len(primary) == 0 && len(fallback) == 0 {
		return nil
	}
	merged := make(map[string]interface{}, len(primary)+len(fallback))
	for k, v := range fallback {
		merged[k] = v
	}
	for k, v := range primary {
		merged[k] = v
	}
	return merged
}

// SetSystem updates the system prompt
func (a *ToolLoopAgent) SetSystem(system string) {
	a.config.System = system
}

// AddTool adds a tool to the agent
func (a *ToolLoopAgent) AddTool(tool types.Tool) {
	a.config.Tools = append(a.config.Tools, tool)
}

// RemoveTool removes a tool from the agent by name
func (a *ToolLoopAgent) RemoveTool(toolName string) {
	for i, tool := range a.config.Tools {
		if tool.Name == toolName {
			a.config.Tools = append(a.config.Tools[:i], a.config.Tools[i+1:]...)
			return
		}
	}
}

// SetMaxSteps updates the maximum number of steps.
func (a *ToolLoopAgent) SetMaxSteps(maxSteps int) {
	a.config.MaxSteps = 0
	a.config.StopWhen = []ai.StopCondition{ai.IsStepCount(maxSteps)}
	a.warnings = append(a.warnings, types.Warning{
		Type:    "deprecated-setting",
		Message: fmt.Sprintf("ToolLoopAgent.SetMaxSteps is deprecated; use SetStopConditions([]ai.StopCondition{ai.IsStepCount(%d)}) instead.", maxSteps),
	})
}

// SetStopConditions replaces the agent's stop conditions.
func (a *ToolLoopAgent) SetStopConditions(conditions []ai.StopCondition) {
	if len(conditions) == 0 {
		conditions = []ai.StopCondition{ai.IsStepCount(20)}
	}
	a.config.StopWhen = conditions
	a.config.MaxSteps = 0
}

// ========================================================================
// Skills Management
// ========================================================================

// AddSkill adds a skill to the agent
func (a *ToolLoopAgent) AddSkill(skill *Skill) error {
	if a.config.Skills == nil {
		a.config.Skills = NewSkillRegistry()
	}
	return a.config.Skills.Register(skill)
}

// RemoveSkill removes a skill from the agent by name
func (a *ToolLoopAgent) RemoveSkill(name string) {
	if a.config.Skills != nil {
		a.config.Skills.Unregister(name)
	}
}

// GetSkill retrieves a skill by name
func (a *ToolLoopAgent) GetSkill(name string) (*Skill, bool) {
	if a.config.Skills == nil {
		return nil, false
	}
	return a.config.Skills.Get(name)
}

// ListSkills returns all registered skills
func (a *ToolLoopAgent) ListSkills() []*Skill {
	if a.config.Skills == nil {
		return []*Skill{}
	}
	return a.config.Skills.List()
}

// ExecuteSkill runs a skill by name with the given input
func (a *ToolLoopAgent) ExecuteSkill(ctx context.Context, name string, input string) (string, error) {
	if a.config.Skills == nil {
		return "", fmt.Errorf("no skills registry configured")
	}
	return a.config.Skills.Execute(ctx, name, input)
}

// ========================================================================
// Subagent Management
// ========================================================================

// AddSubagent registers a subagent with the given name
func (a *ToolLoopAgent) AddSubagent(name string, subagent Agent) error {
	if a.config.Subagents == nil {
		a.config.Subagents = NewSubagentRegistry()
	}
	return a.config.Subagents.Register(name, subagent)
}

// RemoveSubagent removes a subagent from the agent by name
func (a *ToolLoopAgent) RemoveSubagent(name string) {
	if a.config.Subagents != nil {
		a.config.Subagents.Unregister(name)
	}
}

// GetSubagent retrieves a subagent by name
func (a *ToolLoopAgent) GetSubagent(name string) (Agent, bool) {
	if a.config.Subagents == nil {
		return nil, false
	}
	return a.config.Subagents.Get(name)
}

// ListSubagents returns all registered subagent names
func (a *ToolLoopAgent) ListSubagents() []string {
	if a.config.Subagents == nil {
		return []string{}
	}
	return a.config.Subagents.List()
}

// DelegateToSubagent delegates execution to a named subagent
func (a *ToolLoopAgent) DelegateToSubagent(ctx context.Context, name string, prompt string) (*AgentResult, error) {
	if a.config.Subagents == nil {
		return nil, fmt.Errorf("no subagents registry configured")
	}
	return a.config.Subagents.Execute(ctx, name, prompt)
}

// DelegateToSubagentWithMessages delegates execution to a named subagent with message history
func (a *ToolLoopAgent) DelegateToSubagentWithMessages(ctx context.Context, name string, messages []types.Message) (*AgentResult, error) {
	if a.config.Subagents == nil {
		return nil, fmt.Errorf("no subagents registry configured")
	}
	return a.config.Subagents.ExecuteWithMessages(ctx, name, messages)
}

// ========================================================================
// Run Tracking Helpers (v6.0.61+)
// ========================================================================

// WithRunID adds a run ID to the context for tracking agent execution
// If a run ID already exists in the context, it is preserved and this has no effect
// Use this to provide a custom run ID or to manually initialize run tracking
func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, runIDKey, runID)
}

// WithParentRunID adds a parent run ID to the context for nested/subagent executions
// Use this when delegating to subagents to maintain the execution hierarchy
func WithParentRunID(ctx context.Context, parentRunID string) context.Context {
	return context.WithValue(ctx, parentRunIDKey, parentRunID)
}

// WithTags adds tags to the context for categorizing agent runs
// Tags can be used for filtering, grouping, or labeling runs in monitoring systems
// Example: WithTags(ctx, []string{"production", "user:123", "session:abc"})
func WithTags(ctx context.Context, tags []string) context.Context {
	return context.WithValue(ctx, tagsKey, tags)
}

// GetRunID retrieves the run ID from the context
// Returns empty string if no run ID is present
func GetRunID(ctx context.Context) string {
	runID, _ := ctx.Value(runIDKey).(string)
	return runID
}

// GetParentRunID retrieves the parent run ID from the context
// Returns empty string if no parent run ID is present
func GetParentRunID(ctx context.Context) string {
	parentRunID, _ := ctx.Value(parentRunIDKey).(string)
	return parentRunID
}

// GetTags retrieves the tags from the context
// Returns nil if no tags are present
func GetTags(ctx context.Context) []string {
	tags, _ := ctx.Value(tagsKey).([]string)
	return tags
}

// validateStepToolResults returns a *types.MissingToolResultsError when the
// step prompt has a tool call without a tool result before later context or
// the end of the conversation. Provider-executed tools are exempt.
func validateStepToolResults(messages []types.Message, tools []types.Tool) error {
	var providerTools []string
	for _, tool := range tools {
		if tool.ProviderExecuted {
			providerTools = append(providerTools, tool.Name)
		}
	}
	return promptutils.ValidateToolResults(messages, providerTools...)
}

// agentStepContinues applies the TS generateText continuation rule to a
// step's model response (before client tools run): continue when the step has
// a client tool call or a deferred provider result is still pending. It also
// updates pending (TS pendingDeferredToolCalls): provider-executed calls of a
// SupportsDeferredResults tool without an inline result are added, and every
// tool-result part in the response resolves its entry.
func agentStepContinues(step *types.StepResult, tools []types.Tool, pending map[string]string) bool {
	hasResult := func(id string) bool {
		for _, part := range step.Content {
			if tr, ok := part.(types.ToolResultContent); ok && tr.ToolCallID == id {
				return true
			}
		}
		return false
	}
	hasClient := false
	for _, call := range step.ToolCalls {
		tool := findAgentTool(call.ToolName, tools)
		if !call.ProviderExecuted && (tool == nil || !tool.ProviderExecuted) {
			hasClient = true
			continue
		}
		if tool != nil && tool.ProviderExecuted && tool.SupportsDeferredResults && !hasResult(call.ID) {
			pending[call.ID] = call.ToolName
		}
	}
	for _, part := range step.Content {
		if tr, ok := part.(types.ToolResultContent); ok {
			delete(pending, tr.ToolCallID)
		}
	}
	return hasClient || len(pending) > 0
}

// hasUnresolvedLocalAgentToolCalls reports whether a locally executed tool call
// of the step has no tool result (mirrors ai.hasUnresolvedLocalToolCalls).
func hasUnresolvedLocalAgentToolCalls(calls []types.ToolCall, tools []types.Tool, results []types.ToolResult) bool {
	resolved := make(map[string]bool, len(results))
	for _, r := range results {
		resolved[r.ToolCallID] = true
	}
	for _, call := range calls {
		tool := findAgentTool(call.ToolName, tools)
		if tool == nil || tool.ProviderExecuted {
			continue
		}
		if !resolved[call.ID] {
			return true
		}
	}
	return false
}

// agentAbortEvent adapts AgentStreamOptions.OnAbort (completed steps only) to
// the stable ai.StreamTextOptions.OnAbortEvent callback.
func agentAbortEvent(onAbort func(ctx context.Context, steps []types.StepResult)) ai.OnAbortCallback {
	if onAbort == nil {
		return nil
	}
	return func(ctx context.Context, e ai.GenerateTextAbortEvent) { onAbort(ctx, e.Steps) }
}

// toolTimings collects per-tool-call execution times of one step (TS
// performance.toolExecutionMs, keyed by tool call id). Tool calls may run
// concurrently. A nil *toolTimings records nothing.
type toolTimings struct {
	mu sync.Mutex
	ms map[string]int64
}

func (t *toolTimings) record(toolCallID string, ms int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ms == nil {
		t.ms = map[string]int64{}
	}
	t.ms[toolCallID] = ms
}

func (t *toolTimings) snapshot() map[string]int64 {
	out := map[string]int64{}
	if t == nil {
		return out
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, v := range t.ms {
		out[k] = v
	}
	return out
}

func responseMessagesOf(steps []types.StepResult) []types.Message {
	var messages []types.Message
	for _, step := range steps {
		messages = append(messages, step.Response.Messages...)
	}
	return messages
}

// agentHasProviderApprovalRequest reports whether content holds a
// non-automatic tool-approval-request for a provider-executed call.
func agentHasProviderApprovalRequest(content []types.ContentPart) bool {
	for _, part := range content {
		switch p := part.(type) {
		case types.ToolApprovalRequestContent:
			if p.ToolCall.ProviderExecuted && !p.IsAutomatic {
				return true
			}
		case *types.ToolApprovalRequestContent:
			if p != nil && p.ToolCall.ProviderExecuted && !p.IsAutomatic {
				return true
			}
		}
	}
	return false
}
