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
	callConfig := (&ToolLoopAgent{config: config}).prepareStepCallConfig(ctx, 0, prompt, messages, nil, types.Usage{}, nil)
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
	return ai.GenerateText(ctx, ai.GenerateTextOptions{
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
		ToolChoice:                     defaultToolChoice(callConfig.ToolChoice),
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
		OnToolExecutionStart:           cbs.onToolCallStart,
		OnToolExecutionEnd:             cbs.onToolCallFinish,
		OnStepEndEvent:                 cbs.onStepFinish,
		OnEndEvent:                     cbs.onFinish,
	})
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
	callConfig := (&ToolLoopAgent{config: config}).prepareStepCallConfig(ctx, 0, prompt, messages, nil, types.Usage{}, nil)
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
		ToolChoice:                     defaultToolChoice(callConfig.ToolChoice),
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
		OnError:                        opts.OnError,
		OnAbortEvent:                   agentAbortEvent(opts.OnAbort),
		OnStart:                        cbs.onStart,
		OnStepStart:                    cbs.onStepStart,
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
	if err := validateToolCallConcurrency(a.config.ToolCallConcurrency); err != nil {
		return nil, err
	}
	// Open the telemetry root span (unless the caller already did) so the
	// step, model-call and tool-call spans below have a parent; see
	// loop_telemetry.go.
	var lt *loopTelemetry
	ctx, lt = a.beginLoopTelemetry(ctx, messages)
	ctx = context.WithValue(ctx, loopTelemetryKey{}, lt)
	result, err := a.runLoop(ctx, messages, callCallbacks, lt)
	a.endLoopTelemetry(ctx, lt, result, err)
	return result, err
}

func (a *ToolLoopAgent) runLoop(ctx context.Context, messages []types.Message, callCallbacks agentCallbacks, lt *loopTelemetry) (*AgentResult, error) {
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

	// CB-T23: Emit OnStartEvent
	ai.Notify(ctx, ai.OnStartEvent{
		Provider: a.config.Model.Provider(),
		// ModelProvider/System are deprecated aliases of Provider/Instructions;
		// set for callbacks still reading the old fields (mirrors
		// generate.go's OnStartEvent population).
		ModelProvider:       a.config.Model.Provider(), //nolint:staticcheck
		ModelID:             a.config.Model.ModelID(),
		Instructions:        a.config.System,
		System:              a.config.System, //nolint:staticcheck
		Messages:            messages,
		Tools:               a.config.Tools,
		ToolChoice:          a.config.ToolChoice,
		Temperature:         a.config.Temperature,
		MaxTokens:           a.config.MaxTokens,
		TopP:                a.config.TopP,
		TopK:                a.config.TopK,
		FrequencyPenalty:    a.config.FrequencyPenalty,
		PresencePenalty:     a.config.PresencePenalty,
		StopSequences:       a.config.StopSequences,
		Seed:                a.config.Seed,
		ProviderOptions:     a.config.ProviderOptions,
		ExperimentalContext: a.config.ExperimentalContext,
		RuntimeContext:      a.runtimeContext(),
		ToolsContext:        a.config.ToolsContext,
	}, cbs.onStart)

	// Apply total timeout if configured
	var cancel context.CancelFunc
	if a.config.Timeout != nil && a.config.Timeout.HasTotal() {
		ctx, cancel = a.config.Timeout.CreateTimeoutContext(ctx, "total")
		defer cancel()
	}

	// Initialize result
	result := &AgentResult{
		Steps:       []types.StepResult{},
		ToolResults: []types.ToolResult{},
		Delegations: []SubagentDelegation{},
		Warnings:    append([]types.Warning{}, a.warnings...),
	}

	// Current conversation state
	currentMessages := make([]types.Message, len(messages))
	copy(currentMessages, messages)

	// Resume tool approvals from the input messages before the first step
	// (review finding F5): this loop previously signed approval requests
	// (see agentToolResultsToContentParts) but never resumed them, so a
	// history ending in an approved tool-approval-response silently
	// continued without executing the tool or verifying its signature.
	resumed, resumeErr := ai.ResumeToolApprovals(ctx, ai.ResumeToolApprovalsOptions{
		Messages:             currentMessages,
		Tools:                a.config.Tools,
		ToolApproval:         a.config.ToolApproval,
		ToolsContext:         a.config.ToolsContext,
		RuntimeContext:       a.runtimeContext(),
		Secret:               a.config.ExperimentalToolApprovalSecret,
		RefineToolInput:      a.config.ExperimentalRefineToolInput,
		ToolCallConcurrency:  a.config.ToolCallConcurrency,
		ModelProvider:        a.config.Model.Provider(),
		ModelID:              a.config.Model.ModelID(),
		OnToolExecutionStart: cbs.onToolCallStart,
		OnToolExecutionEnd:   cbs.onToolCallFinish,
	})
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
	resolvedToolCallers, err := ai.ResolveToolCallerConfiguration(a.config.Tools, a.config.ExperimentalToolCallers)
	if err != nil {
		if a.config.OnChainError != nil {
			a.config.OnChainError(err)
		}
		return nil, err
	}
	toolSearchState, err := ai.NewToolSearchState(a.config.Tools, resolvedToolCallers)
	if err != nil {
		if a.config.OnChainError != nil {
			a.config.OnChainError(err)
		}
		return nil, err
	}

	// Execute agent loop
	for stepNum := 1; a.config.MaxSteps <= 0 || stepNum <= a.config.MaxSteps; stepNum++ {
		stepIndex := stepNum - 1
		callConfig := a.prepareStepCallConfig(ctx, stepIndex, "", currentMessages, result.Steps, result.Usage, customData)
		// A PrepareCall override of the telemetry settings (TS prepareCall
		// `telemetry`) applies to this and the following steps' spans.
		if callConfig.Telemetry != nil && callConfig.Telemetry != a.config.Telemetry {
			ctx = a.adoptPreparedTelemetry(ctx, lt, callConfig.Telemetry, currentMessages)
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
		// the model-visible set reported to OnStepStartEvent and sent to
		// the model). A local caller's catalog-announcement message, if
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

		// CB-T23: Emit OnStepStartEvent
		ai.Notify(ctx, ai.OnStepStartEvent{
			StepNumber: stepIndex,
			Provider:   callConfig.Model.Provider(),
			// ModelProvider/System are deprecated aliases of Provider/
			// Instructions; set for callbacks still reading the old fields
			// (mirrors generate.go's OnStepStartEvent population).
			ModelProvider:       callConfig.Model.Provider(), //nolint:staticcheck
			ModelID:             callConfig.Model.ModelID(),
			Instructions:        callConfig.System,
			System:              callConfig.System, //nolint:staticcheck
			Messages:            callConfig.Messages,
			Tools:               callConfig.Tools,
			PreviousSteps:       result.Steps,
			ExperimentalContext: a.config.ExperimentalContext,
			RuntimeContext:      callConfig.RuntimeContext,
			ToolsContext:        callConfig.ToolsContext,
		}, cbs.onStepStart)

		// Call step start callback (legacy)
		if a.config.OnStepStart != nil {
			a.config.OnStepStart(stepIndex)
		}

		// Open the step telemetry span; tools and the model call run under it.
		stepTel := a.startStepTelemetry(ctx, lt, stepIndex, callConfig)
		stepCtx := stepTel.stepCtx

		// Execute one step with custom data
		stepStarted := time.Now()
		stepResult, shouldContinue, newCustomData, _, err := a.executeStep(stepCtx, callConfig, stepTel)
		customData = newCustomData
		if err != nil {
			stepTel.fail(err)
			// Call OnChainError callback
			if a.config.OnChainError != nil {
				a.config.OnChainError(err)
			}
			return nil, fmt.Errorf("step %d failed: %w", stepNum, err)
		}

		responseContent := append([]types.ContentPart(nil), stepResult.Content...)

		// If there are tool calls, execute them
		var stepToolResults []types.ToolResult
		// WorkflowAgent: a step that finished with a reason other than stop or
		// tool-calls ends the run; its tool calls are neither executed nor
		// answered (TS modelCallIterator shouldProcessTools).
		workflowBlocked := ai.WorkflowBlockedToolStep(callConfig.Internal, stepResult.FinishReason, stepResult.Content)
		if workflowBlocked {
			shouldContinue = false
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
			if len(callConfig.ExperimentalRefineToolInput) > 0 {
				refined, refineErr := ai.RefineToolCalls(stepCtx, stepResult.ToolCalls, callConfig.Tools, callConfig.ExperimentalRefineToolInput, callConfig.RuntimeContext, callConfig.ToolsContext)
				if refineErr != nil {
					stepTel.fail(refineErr)
					return nil, refineErr
				}
				stepResult.ToolCalls = refined
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
					stepTel.fail(err)
					return nil, err
				}
				local := *a
				local.config.ToolCallConcurrency = callConfig.ToolCallConcurrency
				stepAgent = &local
			}
			// The tool-call events report the step's model (a PrepareCall model
			// override), not the agent's configured one.
			if callConfig.Model != nil && callConfig.Model != stepAgent.config.Model {
				local := *stepAgent
				local.config.Model = callConfig.Model
				stepAgent = &local
			}
			toolResults, err := stepAgent.executeTools(stepCtx, stepResult.ToolCalls, callTools, callConfig.Messages, stepIndex, callConfig.RuntimeContext, callConfig.ToolsContext, callConfig.ExperimentalSandbox, callConfig.ToolApproval, cbs, result.Usage.Add(stepResult.Usage), timings)
			if err != nil {
				stepTel.fail(err)
				// Call OnChainError callback
				if a.config.OnChainError != nil {
					a.config.OnChainError(err)
				}
				var conversionErr *toolResultModelOutputError
				if errors.As(err, &conversionErr) {
					return nil, conversionErr.Unwrap()
				}
				return nil, err
			}

			// WorkflowAgent: a provider-executed call with no result gets an
			// empty text result (TS resolveProviderToolResult) instead of the
			// pending marker, so the loop continues with a text output.
			if callConfig.Internal != nil && callConfig.Internal.ContinueOnMissingProviderResult {
				for _, synthetic := range ai.EmptyProviderResults(stepResult.ToolCalls, callConfig.Tools, stepResult.Content, stepResult.FinishReason, a.config.Model.Provider(), a.config.Model.ModelID()) {
					for i := range toolResults {
						if toolResults[i].ToolCallID == synthetic.ToolCallID && toolResults[i].ProviderExecuted && toolResults[i].Result == nil && toolResults[i].Error == nil {
							toolResults[i].Result = synthetic.Result
							toolResults[i].ModelOutput = synthetic.ModelOutput
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
			stepResult.Content = append(stepResult.Content, agentToolResultsToContentParts(toolResults, callConfig.ExperimentalToolApprovalSecret)...)
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
		// A provider-executed call the provider gated behind its own
		// tool-approval-request (MCP) pauses the loop until the caller
		// answers; TS WorkflowAgent returns on providerApprovalRequests.
		if agentHasProviderApprovalRequest(stepResult.Content) {
			shouldContinue = false
		}
		// TS stepTimeMs spans the model call and the tool execution.
		stepResult.Performance.StepTimeMs = time.Since(stepStarted).Milliseconds()
		responseCalls := stepResult.ToolCalls
		if workflowBlocked {
			responseCalls, responseContent = ai.WorkflowBlockedResponse(responseCalls, responseContent)
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
			CallID:             stepTel.callID(),
			StepNumber:         stepResult.StepNumber,
			Model:              stepResult.Model,
			ModelProvider:      stepResult.Model.Provider,
			ModelID:            stepResult.Model.ModelID,
			Text:               stepResult.Text,
			Reasoning:          stepResult.Reasoning,
			ReasoningText:      stepResult.ReasoningText,
			ToolCalls:          stepResult.ToolCalls,
			StaticToolCalls:    stepResult.StaticToolCalls,
			DynamicToolCalls:   stepResult.DynamicToolCalls,
			ToolResults:        stepToolResults,
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
	if result.Output == nil && (a.config.Output == nil || shouldParseAgentOutput(result.FinishReason, result.Text)) {
		output, err := parseAgentOutput(ctx, a.config.Output, result)
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

func (a *ToolLoopAgent) prepareStepCallConfig(ctx context.Context, stepNum int, prompt string, messages []types.Message, previousSteps []types.StepResult, accumulatedUsage types.Usage, customData interface{}) PrepareCallConfig {
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
	if callConfig.Model == nil {
		callConfig.Model = a.config.Model
	}
	if len(callConfig.ActiveTools) > 0 {
		callConfig.Tools = ai.FilterActiveTools(callConfig.Tools, callConfig.ActiveTools)
	}
	if a.config.FilterActiveTools != nil {
		callConfig.Tools = a.config.FilterActiveTools(ctx, stepNum, callConfig.Tools)
	}
	if callConfig.RuntimeContext == nil {
		callConfig.RuntimeContext = a.runtimeContext()
	}
	if callConfig.ToolsContext == nil {
		callConfig.ToolsContext = a.config.ToolsContext
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
func parseAgentToolCalls(ctx context.Context, calls []types.ToolCall, callConfig PrepareCallConfig, messages []types.Message) ([]types.ToolCall, error) {
	if len(calls) == 0 {
		return calls, nil
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
			RepairToolCall:      callConfig.RepairToolCall,
			Instructions:        callConfig.System,
			InstructionMessages: callConfig.InstructionMessages,
			Messages:            messages,
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

func defaultToolChoice(choice types.ToolChoice) types.ToolChoice {
	if choice.Type == "" {
		return types.AutoToolChoice()
	}
	return choice
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
	args := []reflect.Value{
		reflect.ValueOf(ctx),
		reflect.ValueOf(ai.ParseCompleteOutputOptions{
			Text:         result.Text,
			Usage:        &result.Usage,
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

// executeStep executes a single agent step
func (a *ToolLoopAgent) executeStep(ctx context.Context, callConfig PrepareCallConfig, stepTel *loopStepTelemetry) (*types.StepResult, bool, interface{}, []types.Tool, error) {
	// Apply per-step timeout if configured
	stepCtx := ctx
	var stepCancel context.CancelFunc
	if a.config.Timeout != nil && a.config.Timeout.HasPerStep() {
		stepCtx, stepCancel = a.config.Timeout.CreateTimeoutContext(ctx, "step")
		defer stepCancel()
	}

	toolChoice := callConfig.ToolChoice
	if toolChoice.Type == "" {
		toolChoice = types.AutoToolChoice()
	}

	responseFormat, err := responseFormatForOutput(stepCtx, callConfig.Output)
	if err != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, err
	}
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
				return nil, false, callConfig.CustomData, callConfig.Tools, &providererrors.InvalidArgumentError{
					Field:   "instructions",
					Message: "instructions must be a string, SystemModelMessage, or array of SystemModelMessage",
				}
			}
		}
		promptSystem = ""
	}
	prompt, err := promptutils.NormalizePromptWithDownloadSupport(stepCtx, types.Prompt{
		Messages: callConfig.Messages,
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
		Headers:               callConfig.Headers,
		Reasoning:             callConfig.Reasoning,
		SendReasoning:         callConfig.SendReasoning,
		ProviderOptions:       callConfig.ProviderOptions,
		Telemetry:             callConfig.Telemetry,
		ResponseFormat:        responseFormat,
	}

	// Call the model with step context, retrying transient Gateway failures
	// per callConfig.MaxRetries (WORKFLOW-AGENT-OPTIONS, e6064c5).
	modelCallCtx := stepCtx
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
		if stepCtx != ctx {
			// keep the per-step timeout deadline on the model-call ctx
			var cancel context.CancelFunc
			modelCallCtx, cancel = context.WithCancel(mc)
			defer cancel()
			stop := context.AfterFunc(stepCtx, cancel)
			defer stop()
		}
	}
	modelCallStarted := time.Now()
	genResult, err := ai.DoGenerateWithGatewayRetry(modelCallCtx, stepModel, genOpts, callConfig.MaxRetries)
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
	toolCalls, err := parseAgentToolCalls(stepCtx, genResult.ToolCalls, callConfig, prompt.Messages)
	if err != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, err
	}
	toolCalls = enrichAgentToolCallMetadata(toolCalls, callConfig.Tools)
	// URL-backed generated files are downloaded once, before anything observes
	// the content (TS convertLanguageModelContent).
	if resolvedContent, resolveErr := ai.ResolveContentFiles(stepCtx, genResult.Content); resolveErr != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, resolveErr
	} else if len(resolvedContent) > 0 {
		resolvedResult := *genResult
		resolvedResult.Content = resolvedContent
		genResult = &resolvedResult
	}
	// A tool-result part must answer a tool call of the same response (TS
	// convertLanguageModelContent: "Tool call <id> not found.").
	if err := ai.ValidateToolResultParts(genResult.Content, toolCalls, callConfig.Tools); err != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, err
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
		ID:        ai.GenerateID(),
		Timestamp: time.Now(),
		ModelID:   stepModel.ModelID(),
		Headers:   genResult.ResponseHeaders,
		Body:      includeValue(include.ResponseBody, genResult.RawResponse),
	}
	if meta := genResult.ResponseMetadata; meta != nil {
		if meta.ID != "" {
			stepResponse.ID = meta.ID
		}
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
	// call, as in GenerateText (TS ToolChoiceViolationError).
	if violation := ai.CheckToolChoiceViolation(genOpts.ToolChoice, genResult.ToolCalls, genResult.FinishReason, stepModel.Provider(), stepModel.ModelID(), contentFromGenerateResult(genResult, genResult.ToolCalls)); violation != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, violation
	}
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
			Messages: includeAgentRequestMessages(include.RequestMessages, prompt.Messages),
		},
		Response:       stepResponse,
		ToolsContext:   callConfig.ToolsContext,
		RuntimeContext: callConfig.RuntimeContext,
		Performance:    performance,
	}

	// Determine if we should continue
	shouldContinue := genResult.FinishReason == types.FinishReasonToolCalls && len(toolCalls) > 0

	return stepResult, shouldContinue, callConfig.CustomData, callConfig.Tools, nil
}

// executeTools executes a list of tool calls with optional approval
// Updated in v6.0.57 to handle provider-executed (deferrable) tools
// Updated in v6.1 (CB-T23) to fire structured OnToolCallStart/Finish events
func (a *ToolLoopAgent) executeTools(ctx context.Context, toolCalls []types.ToolCall, tools []types.Tool, messages []types.Message, stepNum int, runtimeContext interface{}, toolsContext map[string]interface{}, experimentalSandbox interface{}, toolApproval types.ToolApprovalConfig, cbs agentCallbacks, usage types.Usage, timings *toolTimings) ([]types.ToolResult, error) {
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

		providerExecuted := tool.ProviderExecuted
		providerMetadata := mergeProviderMetadataMaps(call.ProviderMetadata, tool.ProviderMetadata)

		approval, approvalErr := a.resolveToolApproval(ctx, call, tools, messages, runtimeContext, toolsContext, toolApproval)
		if approvalErr != nil {
			results = append(results, types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Title:            call.Title,
				Input:            call.Arguments,
				Error:            approvalErr,
				ProviderExecuted: providerExecuted,
				ProviderMetadata: providerMetadata,
				ToolMetadata:     call.ToolMetadata,
			})
			return results, nil
		}
		if a.config.ToolApprovalRequired && a.config.ToolApprover != nil && approval.Status == types.ToolApprovalStatusNotApplicable {
			if !a.config.ToolApprover(call) {
				approval = types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied}
			}
		}
		switch approval.Status {
		case types.ToolApprovalStatusDenied:
			reason := approval.Reason
			if reason == nil {
				reason = strPtr(fmt.Sprintf("Tool call to %s was denied by ToolApproval policy.", call.ToolName))
			}
			rejectionErr := fmt.Errorf("%s", *reason)
			results = append(results, types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Title:            call.Title,
				Input:            call.Arguments,
				Error:            rejectionErr,
				Result:           types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: *reason},
				ApprovalStatus:   types.ToolApprovalStatusDenied,
				ApprovalReason:   reason,
				ProviderExecuted: providerExecuted,
				ProviderMetadata: providerMetadata,
				ToolMetadata:     call.ToolMetadata,
			})
			if a.config.OnToolError != nil {
				a.config.OnToolError(call, rejectionErr)
			}
			return results, nil
		case types.ToolApprovalStatusUserApproval:
			results = append(results, types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Title:            call.Title,
				Input:            call.Arguments,
				Result:           map[string]interface{}{"type": "tool-approval-request", "approvalId": call.ID, "toolCall": call},
				ApprovalStatus:   types.ToolApprovalStatusUserApproval,
				ProviderExecuted: providerExecuted,
				ProviderMetadata: providerMetadata,
				ToolMetadata:     call.ToolMetadata,
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
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Title:            call.Title,
				Input:            call.Arguments,
				Result:           nil,
				Error:            nil,
				ApprovalStatus:   approval.Status,
				ApprovalReason:   approval.Reason,
				ProviderExecuted: true,
				ProviderMetadata: providerMetadata,
				ToolMetadata:     call.ToolMetadata,
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
				results = append(results, types.ToolResult{
					ToolCallID:       call.ID,
					ToolName:         call.ToolName,
					Title:            call.Title,
					Input:            call.Arguments,
					Error:            err,
					ProviderMetadata: providerMetadata,
					ToolMetadata:     call.ToolMetadata,
				})
				return results, nil
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
						ID:               call.ID,
						ToolName:         call.ToolName,
						Title:            call.Title,
						Arguments:        call.Arguments,
						ProviderExecuted: call.ProviderExecuted,
						ProviderMetadata: call.ProviderMetadata,
						ToolMetadata:     call.ToolMetadata,
						Dynamic:          call.Dynamic,
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
	ai.MarkDynamicToolResults(results, toolCalls)
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

func includeAgentRequestMessages(include bool, messages []types.Message) []types.Message {
	if !include {
		return nil
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

// agentToolResultsToContentParts converts tool results to content parts. When
// secret is non-nil, approval requests are signed so they can be verified on
// resume.
func agentToolResultsToContentParts(results []types.ToolResult, secret []byte) []types.ContentPart {
	if len(results) == 0 {
		return nil
	}
	parts := make([]types.ContentPart, 0, len(results))
	for _, result := range results {
		if result.ProviderExecuted && result.ApprovalStatus == "" {
			continue
		}
		if result.ApprovalStatus == types.ToolApprovalStatusUserApproval ||
			result.ApprovalStatus == types.ToolApprovalStatusApproved ||
			result.ApprovalStatus == types.ToolApprovalStatusDenied {
			toolCall := types.ToolCall{
				ID:               result.ToolCallID,
				ToolName:         result.ToolName,
				Title:            result.Title,
				Arguments:        result.Input,
				ProviderExecuted: result.ProviderExecuted,
				ProviderMetadata: result.ProviderMetadata,
				ToolMetadata:     result.ToolMetadata,
				Dynamic:          result.Dynamic,
			}
			signature := ""
			if secret != nil {
				signature, _ = ai.SignToolApproval(secret, result.ToolCallID, result.ToolCallID, result.ToolName, result.Input)
			}
			approvalRequest := types.ToolApprovalRequestContent{
				ApprovalID:  result.ToolCallID,
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
			parts = append(parts, approvalRequest)
			if result.ApprovalStatus == types.ToolApprovalStatusUserApproval {
				continue
			}
			reason := ""
			if result.ApprovalReason != nil {
				reason = *result.ApprovalReason
			}
			parts = append(parts, types.ToolApprovalResponseContent{
				ApprovalID:       result.ToolCallID,
				ToolCallID:       result.ToolCallID,
				ToolCall:         toolCall,
				Approved:         result.ApprovalStatus == types.ToolApprovalStatusApproved,
				Reason:           reason,
				ProviderExecuted: result.ProviderExecuted,
			})
			if result.ProviderExecuted || result.ApprovalStatus == types.ToolApprovalStatusDenied {
				continue
			}
		}
		if result.Error != nil {
			parts = append(parts, types.ToolErrorContent{
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
		parts = append(parts, part)
	}
	return parts
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

func (a *ToolLoopAgent) resolveToolApproval(ctx context.Context, call types.ToolCall, tools []types.Tool, messages []types.Message, runtimeContext interface{}, toolsContext map[string]interface{}, toolApproval types.ToolApprovalConfig) (types.ToolApprovalResult, error) {
	tool := findAgentTool(call.ToolName, tools)
	approval, err := normalizeAgentToolApproval(call, tool, tools, messages, toolApproval, runtimeContext, toolsContext)
	if err != nil {
		return types.ToolApprovalResult{}, err
	}
	if approval.Status != types.ToolApprovalStatusNotApplicable {
		return approval, nil
	}
	if tool == nil {
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}, nil
	}
	setting := tool.ToolApproval
	if setting == nil {
		setting = tool.NeedsApproval
	}
	switch v := setting.(type) {
	case nil:
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}, nil
	case bool:
		if v {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}, nil
		}
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}, nil
	case types.ToolNeedsApprovalFunc:
		toolCtx, err := validateAgentToolContext(tool, call.ToolName, toolsContext[tool.Name])
		if err != nil {
			return types.ToolApprovalResult{}, err
		}
		if v(ctx, call.Arguments, types.ToolNeedsApprovalOptions{
			ToolCallID: call.ID,
			Messages:   messages,
			Context:    toolCtx,
		}) {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}, nil
		}
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}, nil
	case types.NeedsApprovalFunc: //nolint:staticcheck // legacy function type still accepted for backward compatibility
		if _, err := validateAgentToolContext(tool, call.ToolName, toolsContext[tool.Name]); err != nil {
			return types.ToolApprovalResult{}, err
		}
		if v(ctx, call.Arguments) {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}, nil
		}
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}, nil
	case types.ToolApprovalStatus:
		return normalizeAgentApprovalValue(v), nil
	case string:
		return normalizeAgentApprovalValue(types.ToolApprovalStatus(v)), nil
	}
	return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}, nil
}

func normalizeAgentToolApproval(call types.ToolCall, tool *types.Tool, tools []types.Tool, messages []types.Message, cfg interface{}, runtimeCtx interface{}, toolsCtx map[string]interface{}) (types.ToolApprovalResult, error) {
	switch v := cfg.(type) {
	case nil:
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}, nil
	case types.GenericToolApprovalFunc:
		return normalizeAgentApprovalValue(v(types.ToolApprovalOptions{
			ToolCall:       call,
			Tools:          tools,
			ToolsContext:   toolsCtx,
			RuntimeContext: runtimeCtx,
			Messages:       messages,
		})), nil
	case types.ToolApprovalFunc: //nolint:staticcheck // legacy function type still accepted for backward compatibility
		return normalizeAgentApprovalValue(v(call, tools, messages, runtimeCtx, toolsCtx)), nil
	case map[string]interface{}:
		return normalizeAgentToolApprovalValue(call, tool, tools, messages, v[call.ToolName], runtimeCtx, toolsCtx)
	case map[string]types.ToolApprovalValue:
		return normalizeAgentToolApprovalValue(call, tool, tools, messages, v[call.ToolName], runtimeCtx, toolsCtx)
	default:
		return normalizeAgentApprovalValue(v), nil
	}
}

func normalizeAgentToolApprovalValue(call types.ToolCall, tool *types.Tool, tools []types.Tool, messages []types.Message, value interface{}, runtimeCtx interface{}, toolsCtx map[string]interface{}) (types.ToolApprovalResult, error) {
	switch fn := value.(type) {
	case types.SingleToolApprovalFunc:
		toolCtx, err := validateAgentToolContext(tool, call.ToolName, toolsCtx[call.ToolName])
		if err != nil {
			return types.ToolApprovalResult{}, err
		}
		return normalizeAgentApprovalValue(fn(call.Arguments, types.SingleToolApprovalOptions{
			ToolContext:    toolCtx,
			RuntimeContext: runtimeCtx,
			ToolCallID:     call.ID,
			Messages:       messages,
		})), nil
	case types.GenericToolApprovalFunc:
		return normalizeAgentApprovalValue(fn(types.ToolApprovalOptions{
			ToolCall:       call,
			Tools:          tools,
			ToolsContext:   toolsCtx,
			RuntimeContext: runtimeCtx,
			Messages:       messages,
		})), nil
	case types.ToolApprovalFunc: //nolint:staticcheck // legacy function type still accepted for backward compatibility
		return normalizeAgentApprovalValue(fn(call, tools, messages, runtimeCtx, toolsCtx)), nil
	}
	return normalizeAgentApprovalValue(value), nil
}

func findAgentTool(toolName string, tools []types.Tool) *types.Tool {
	for i := range tools {
		if tools[i].Name == toolName {
			return &tools[i]
		}
	}
	return nil
}

func normalizeAgentApprovalValue(value interface{}) types.ToolApprovalResult {
	switch v := value.(type) {
	case nil:
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	case types.ToolApprovalResult:
		if v.Status == "" {
			v.Status = types.ToolApprovalStatusNotApplicable
		}
		return v
	case *types.ToolApprovalResult:
		if v == nil {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
		}
		return normalizeAgentApprovalValue(*v)
	case types.ToolApprovalStatus:
		if v == "" {
			v = types.ToolApprovalStatusNotApplicable
		}
		return types.ToolApprovalResult{Status: v}
	case string:
		if v == "" {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
		}
		return types.ToolApprovalResult{Status: types.ToolApprovalStatus(v)}
	default:
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	}
}

func validateAgentToolContext(tool *types.Tool, toolName string, ctxValue interface{}) (interface{}, error) {
	if tool == nil || tool.ContextSchema == nil {
		return ctxValue, nil
	}
	normalized := schema.ApplyDefaults(ctxValue, tool.ContextSchema)
	if err := tool.ContextSchema.Validator().Validate(normalized); err != nil {
		return nil, providererrors.NewValidationErrorWithContext(
			normalized,
			fmt.Sprintf("invalid tool context for %s: %v", toolName, err),
			err,
			&providererrors.ValidationContext{Field: "tool context", EntityName: toolName},
		)
	}
	return normalized, nil
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
