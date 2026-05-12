package agent

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/schema"
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

// mergeCallbacks combines settings-level and per-call structured callbacks.
// When both are provided, both fire in order: settings first, then call-level.
// Either (or both) may be nil.
func mergeCallbacks(settings AgentConfig, callOpts agentCallbacks) agentCallbacks {
	return agentCallbacks{
		onStart:          mergeListener(settings.OnStart, callOpts.onStart),
		onStepStart:      mergeListener(settings.OnStepStartEvent, callOpts.onStepStart),
		onToolCallStart:  mergeListener(settings.OnToolCallStart, callOpts.onToolCallStart),
		onToolCallFinish: mergeListener(settings.OnToolCallFinish, callOpts.onToolCallFinish),
		onStepFinish:     mergeListener(settings.OnStepFinishEvent, callOpts.onStepFinish),
		onFinish:         mergeListener(settings.OnFinishEvent, callOpts.onFinish),
	}
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
		config.StopWhen = []ai.StopCondition{ai.IsStepCount(20)}
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

// Generate runs the agent with per-call options, mirroring TypeScript
// ToolLoopAgent.generate with an idiomatic Go options struct.
func (a *ToolLoopAgent) Generate(ctx context.Context, opts AgentGenerateOptions) (*AgentResult, error) {
	if err := validateAgentPromptOptions(opts.Prompt, opts.Messages); err != nil {
		return nil, err
	}
	callAgent := &ToolLoopAgent{
		config:   a.config.withGenerateOptions(opts),
		warnings: append([]types.Warning{}, a.warnings...),
	}
	messages := opts.Messages
	if len(messages) == 0 {
		if opts.Prompt != "" {
			messages = []types.Message{{
				Role:    types.RoleUser,
				Content: []types.ContentPart{types.TextContent{Text: opts.Prompt}},
			}}
		}
	}
	cbs := agentCallbacks{
		onStart:          opts.OnStart,
		onStepStart:      opts.OnStepStart,
		onToolCallStart:  opts.OnToolCallStart,
		onToolCallFinish: opts.OnToolCallFinish,
		onStepFinish:     opts.OnStepFinish,
		onFinish:         opts.OnFinish,
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
	if err := validateAgentCallOptions(config.CallOptionsSchema, config.CallOptions); err != nil {
		return nil, err
	}
	messages := opts.Messages
	prompt := opts.Prompt
	callConfig := (&ToolLoopAgent{config: config}).prepareStepCallConfig(ctx, 1, messages, nil, types.Usage{}, nil)
	cbs := mergeCallbacks(config, agentCallbacks{
		onStart:          opts.OnStart,
		onStepStart:      opts.OnStepStart,
		onToolCallStart:  opts.OnToolCallStart,
		onToolCallFinish: opts.OnToolCallFinish,
		onStepFinish:     opts.OnStepFinish,
		onFinish:         opts.OnFinish,
	})
	streamOpts := ai.StreamTextOptions{
		Model:                config.Model,
		Prompt:               prompt,
		Messages:             messages,
		System:               callConfig.System,
		Temperature:          callConfig.Temperature,
		MaxTokens:            callConfig.MaxTokens,
		TopP:                 callConfig.TopP,
		TopK:                 callConfig.TopK,
		FrequencyPenalty:     callConfig.FrequencyPenalty,
		PresencePenalty:      callConfig.PresencePenalty,
		StopSequences:        callConfig.StopSequences,
		Seed:                 callConfig.Seed,
		Tools:                callConfig.Tools,
		ToolChoice:           defaultToolChoice(callConfig.ToolChoice),
		ToolApproval:         config.ToolApproval,
		StopWhen:             config.StopWhen,
		Timeout:              config.Timeout,
		Reasoning:            callConfig.Reasoning,
		SendReasoning:        callConfig.SendReasoning,
		ProviderOptions:      callConfig.ProviderOptions,
		RuntimeContext:       callConfig.RuntimeContext,
		ToolsContext:         callConfig.ToolsContext,
		ExperimentalContext:  config.ExperimentalContext,
		Output:               config.Output,
		Telemetry:            config.Telemetry,
		OnChunk:              opts.OnChunk,
		OnStart:              cbs.onStart,
		OnStepStart:          cbs.onStepStart,
		OnToolExecutionStart: cbs.onToolCallStart,
		OnToolExecutionEnd:   cbs.onToolCallFinish,
		OnStepFinishEvent:    cbs.onStepFinish,
		OnFinishEvent:        cbs.onFinish,
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
	if a.config.Model.SpecificationVersion() == "" || a.config.Model.Provider() == "" || a.config.Model.ModelID() == "" {
		return nil, fmt.Errorf("model must implement provider.LanguageModel metadata methods")
	}
	if err := validateAgentCallOptions(a.config.CallOptionsSchema, a.config.CallOptions); err != nil {
		return nil, err
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
		ModelProvider:       a.config.Model.Provider(),
		ModelID:             a.config.Model.ModelID(),
		System:              a.config.System,
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

	// Custom data for PrepareCall (persists across steps)
	var customData interface{}

	// Execute agent loop
	for stepNum := 1; a.config.MaxSteps <= 0 || stepNum <= a.config.MaxSteps; stepNum++ {
		callConfig := a.prepareStepCallConfig(ctx, stepNum, currentMessages, result.Steps, result.Usage, customData)

		// CB-T23: Emit OnStepStartEvent
		ai.Notify(ctx, ai.OnStepStartEvent{
			StepNumber:          stepNum,
			ModelProvider:       a.config.Model.Provider(),
			ModelID:             a.config.Model.ModelID(),
			System:              callConfig.System,
			Messages:            callConfig.Messages,
			Tools:               callConfig.Tools,
			PreviousSteps:       result.Steps,
			ExperimentalContext: a.config.ExperimentalContext,
			RuntimeContext:      callConfig.RuntimeContext,
			ToolsContext:        callConfig.ToolsContext,
		}, cbs.onStepStart)

		// Call step start callback (legacy)
		if a.config.OnStepStart != nil {
			a.config.OnStepStart(stepNum)
		}

		// Execute one step with custom data
		stepResult, shouldContinue, newCustomData, activeTools, err := a.executeStep(ctx, callConfig)
		customData = newCustomData
		if err != nil {
			// Call OnChainError callback
			if a.config.OnChainError != nil {
				a.config.OnChainError(err)
			}
			return nil, fmt.Errorf("step %d failed: %w", stepNum, err)
		}

		responseContent := make([]types.ContentPart, 0, 1)
		if stepResult.Text != "" {
			responseContent = append(responseContent, types.TextContent{Text: stepResult.Text})
		}

		// If there are tool calls, execute them
		var stepToolResults []types.ToolResult
		if len(stepResult.ToolCalls) > 0 {
			// Call OnAgentAction callback for each tool call
			if a.config.OnAgentAction != nil {
				// Extract run tracking from context
				runID, _ := ctx.Value(runIDKey).(string)
				parentRunID, _ := ctx.Value(parentRunIDKey).(string)
				tags, _ := ctx.Value(tagsKey).([]string)

				for _, toolCall := range stepResult.ToolCalls {
					action := AgentAction{
						ToolCall:    toolCall,
						StepNumber:  stepNum,
						Reasoning:   stepResult.Text, // Include any reasoning text from the step
						RunID:       runID,
						ParentRunID: parentRunID,
						Tags:        tags,
					}
					a.config.OnAgentAction(action)
				}
			}
			toolResults, err := a.executeTools(ctx, stepResult.ToolCalls, activeTools, currentMessages, stepNum, callConfig.RuntimeContext, callConfig.ToolsContext, cbs)
			if err != nil {
				// Call OnChainError callback
				if a.config.OnChainError != nil {
					a.config.OnChainError(err)
				}
				return nil, fmt.Errorf("tool execution failed at step %d: %w", stepNum, err)
			}

			stepToolResults = toolResults
			stepResult.ToolResults = toolResults
			result.ToolResults = append(result.ToolResults, toolResults...)
			if len(toolResults) == 0 {
				shouldContinue = false
			}
		}
		responseMessages := providerutils.ConvertToResponseMessages(stepResult.ToolCalls, responseContent, stepToolResults)
		stepResult.ResponseMessages = responseMessages
		currentMessages = append(currentMessages, responseMessages...)

		// Add step to results after tool execution so step data mirrors TS step objects.
		result.Steps = append(result.Steps, *stepResult)
		result.Usage = result.Usage.Add(stepResult.Usage)
		result.Warnings = append(result.Warnings, stepResult.Warnings...)

		// Call step finish callback (legacy)
		if a.config.OnStepFinish != nil {
			a.config.OnStepFinish(*stepResult)
		}

		// CB-T23: Emit OnStepFinishEvent (after tool execution so ToolResults is populated)
		ai.Notify(ctx, ai.OnStepFinishEvent{
			StepNumber:          stepResult.StepNumber,
			ModelProvider:       a.config.Model.Provider(),
			ModelID:             a.config.Model.ModelID(),
			Text:                stepResult.Text,
			ToolCalls:           stepResult.ToolCalls,
			ToolResults:         stepToolResults,
			FinishReason:        stepResult.FinishReason,
			Usage:               stepResult.Usage,
			Warnings:            stepResult.Warnings,
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
					StepNumber:   stepNum,
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
		if len(a.config.StopWhen) > 0 {
			state := ai.StopConditionState{
				Steps:    result.Steps,
				Messages: currentMessages,
				Usage:    result.Usage,
			}
			if reason := ai.EvaluateStopConditions(a.config.StopWhen, state); reason != "" {
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
						StepNumber:   stepNum,
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
					StepNumber:   stepNum,
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
	if result.Output == nil {
		output, err := parseAgentOutput(ctx, a.config.Output, result)
		if err != nil {
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

	// Aggregate all tool calls across steps for the finish event
	var allToolCalls []types.ToolCall
	for _, s := range result.Steps {
		allToolCalls = append(allToolCalls, s.ToolCalls...)
	}

	// CB-T23: Emit OnFinishEvent
	ai.Notify(ctx, ai.OnFinishEvent{
		Text:                result.Text,
		ToolCalls:           allToolCalls,
		ToolResults:         result.ToolResults,
		FinishReason:        result.FinishReason,
		Steps:               result.Steps,
		TotalUsage:          result.Usage,
		Warnings:            result.Warnings,
		ExperimentalContext: a.config.ExperimentalContext,
		RuntimeContext:      a.runtimeContext(),
		ToolsContext:        a.config.ToolsContext,
	}, cbs.onFinish)

	return result, nil
}

func (a *ToolLoopAgent) prepareStepCallConfig(ctx context.Context, stepNum int, messages []types.Message, previousSteps []types.StepResult, accumulatedUsage types.Usage, customData interface{}) PrepareCallConfig {
	callConfig := PrepareCallConfig{
		StepNumber:       stepNum,
		System:           a.config.System,
		Messages:         messages,
		Tools:            a.config.Tools,
		ToolChoice:       a.config.ToolChoice,
		Temperature:      a.config.Temperature,
		MaxTokens:        a.config.MaxTokens,
		TopP:             a.config.TopP,
		TopK:             a.config.TopK,
		FrequencyPenalty: a.config.FrequencyPenalty,
		PresencePenalty:  a.config.PresencePenalty,
		StopSequences:    a.config.StopSequences,
		Seed:             a.config.Seed,
		Headers:          a.config.Headers,
		Reasoning:        a.config.Reasoning,
		SendReasoning:    a.config.SendReasoning,
		ProviderOptions:  a.config.ProviderOptions,
		RuntimeContext:   a.runtimeContext(),
		ToolsContext:     a.config.ToolsContext,
		PreviousSteps:    previousSteps,
		AccumulatedUsage: accumulatedUsage,
		CustomData:       customData,
		CallOptions:      a.config.CallOptions,
	}

	if a.config.PrepareCall != nil {
		callConfig = a.config.PrepareCall(ctx, callConfig)
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
	return callConfig
}

func (c AgentConfig) withGenerateOptions(opts AgentGenerateOptions) AgentConfig {
	if opts.System != "" {
		c.System = opts.System
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
	return c
}

func validateAgentCallOptions(callOptionsSchema schema.Schema, callOptions interface{}) error {
	if callOptionsSchema == nil || callOptions == nil {
		return nil
	}
	if err := callOptionsSchema.Validator().Validate(callOptions); err != nil {
		return providererrors.NewValidationErrorWithContext(
			callOptions,
			fmt.Sprintf("invalid call options at schema path options: %v", err),
			err,
			&providererrors.ValidationContext{Field: "options", EntityName: "callOptions"},
		)
	}
	return nil
}

func validateAgentPromptOptions(prompt string, messages []types.Message) error {
	if prompt != "" && len(messages) > 0 {
		return providererrors.NewValidationErrorWithContext(
			map[string]interface{}{"prompt": prompt, "messages": messages},
			"agent call requires either prompt or messages, not both",
			nil,
			&providererrors.ValidationContext{Field: "prompt", EntityName: "agentCall"},
		)
	}
	if prompt == "" && len(messages) == 0 {
		return providererrors.NewValidationErrorWithContext(
			nil,
			"agent call requires either prompt or messages",
			nil,
			&providererrors.ValidationContext{Field: "prompt", EntityName: "agentCall"},
		)
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
func (a *ToolLoopAgent) executeStep(ctx context.Context, callConfig PrepareCallConfig) (*types.StepResult, bool, interface{}, []types.Tool, error) {
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

	responseFormat, err := responseFormatForOutput(stepCtx, a.config.Output)
	if err != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, err
	}

	// Build generate options using potentially modified config
	genOpts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: callConfig.Messages,
			System:   callConfig.System,
		},
		Temperature:      callConfig.Temperature,
		MaxTokens:        callConfig.MaxTokens,
		TopP:             callConfig.TopP,
		TopK:             callConfig.TopK,
		FrequencyPenalty: callConfig.FrequencyPenalty,
		PresencePenalty:  callConfig.PresencePenalty,
		StopSequences:    callConfig.StopSequences,
		Tools:            callConfig.Tools,
		ToolChoice:       toolChoice,
		RuntimeContext:   callConfig.RuntimeContext,
		ToolsContext:     callConfig.ToolsContext,
		Seed:             callConfig.Seed,
		Headers:          callConfig.Headers,
		Reasoning:        callConfig.Reasoning,
		SendReasoning:    callConfig.SendReasoning,
		ProviderOptions:  callConfig.ProviderOptions,
		Telemetry:        a.config.Telemetry,
		ResponseFormat:   responseFormat,
	}

	// Call the model with step context
	genResult, err := a.config.Model.DoGenerate(stepCtx, genOpts)
	if err != nil {
		return nil, false, callConfig.CustomData, callConfig.Tools, err
	}

	// Extract raw finish reason if available
	rawFinishReason := ""
	if genResult.RawResponse != nil {
		if respMap, ok := genResult.RawResponse.(map[string]interface{}); ok {
			if fr, ok := respMap["finish_reason"].(string); ok {
				rawFinishReason = fr
			}
		}
	}

	// Create step result
	stepResult := &types.StepResult{
		StepNumber:       callConfig.StepNumber,
		Text:             genResult.Text,
		ToolCalls:        genResult.ToolCalls,
		ToolResults:      []types.ToolResult{},
		FinishReason:     genResult.FinishReason,
		RawFinishReason:  rawFinishReason,
		Usage:            genResult.Usage,
		Warnings:         genResult.Warnings,
		ProviderMetadata: genResult.ProviderMetadata,
	}

	// Determine if we should continue
	shouldContinue := genResult.FinishReason == types.FinishReasonToolCalls && len(genResult.ToolCalls) > 0

	return stepResult, shouldContinue, callConfig.CustomData, callConfig.Tools, nil
}

// executeTools executes a list of tool calls with optional approval
// Updated in v6.0.57 to handle provider-executed (deferrable) tools
// Updated in v6.1 (CB-T23) to fire structured OnToolCallStart/Finish events
func (a *ToolLoopAgent) executeTools(ctx context.Context, toolCalls []types.ToolCall, tools []types.Tool, messages []types.Message, stepNum int, runtimeContext interface{}, toolsContext map[string]interface{}, cbs agentCallbacks) ([]types.ToolResult, error) {
	results := make([]types.ToolResult, 0, len(toolCalls))
	if toolsContext == nil {
		toolsContext = map[string]interface{}{}
	}

	for _, call := range toolCalls {
		// Call tool call callback
		if a.config.OnToolCall != nil {
			a.config.OnToolCall(call)
		}

		if call.Invalid {
			invalidErr := call.Error
			if invalidErr == nil {
				invalidErr = fmt.Errorf("invalid tool call for %s", call.ToolName)
			}
			results = append(results, types.ToolResult{
				ToolCallID: call.ID,
				ToolName:   call.ToolName,
				Input:      call.Arguments,
				Error:      invalidErr,
				Result:     types.ToolResultOutput{Type: types.ToolResultOutputError, Value: invalidErr.Error()},
			})
			continue
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
				Input:            call.Arguments,
				Error:            notFoundErr,
				ProviderExecuted: false,
			})

			// Call OnToolError for tool not found
			if a.config.OnToolError != nil {
				a.config.OnToolError(call, notFoundErr)
			}
			continue
		}

		providerExecuted := tool.ProviderExecuted
		providerMetadata := mergeProviderMetadataMaps(call.ProviderMetadata, tool.ProviderMetadata)

		if providerExecuted {
			// Provider-executed tool: result will come from provider in next response
			// Call OnToolStart for provider-executed tools
			if a.config.OnToolStart != nil {
				a.config.OnToolStart(call)
			}

			result := types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Input:            call.Arguments,
				Result:           nil,
				Error:            nil,
				ProviderExecuted: true,
				ProviderMetadata: providerMetadata,
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
			toolContext, err := validateAgentToolContext(tool, call.ToolName, toolsContext[call.ToolName])
			if err != nil {
				results = append(results, types.ToolResult{
					ToolCallID:       call.ID,
					ToolName:         call.ToolName,
					Input:            call.Arguments,
					Error:            err,
					ProviderMetadata: providerMetadata,
				})
				continue
			}

			approval := a.resolveToolApproval(ctx, call, tools, messages, runtimeContext, toolsContext)
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
					Input:            call.Arguments,
					Error:            rejectionErr,
					Result:           types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: *reason},
					ApprovalStatus:   types.ToolApprovalStatusDenied,
					ApprovalReason:   reason,
					ProviderMetadata: providerMetadata,
				})
				if a.config.OnToolError != nil {
					a.config.OnToolError(call, rejectionErr)
				}
				continue
			case types.ToolApprovalStatusUserApproval:
				results = append(results, types.ToolResult{
					ToolCallID:       call.ID,
					ToolName:         call.ToolName,
					Input:            call.Arguments,
					Result:           map[string]interface{}{"type": "tool-approval-request", "approvalId": call.ID, "toolCall": call},
					ApprovalStatus:   types.ToolApprovalStatusUserApproval,
					ProviderMetadata: providerMetadata,
				})
				continue
			}

			if tool.Execute == nil {
				continue
			}

			// Locally-executed tool: execute now
			// Call OnToolStart before execution (legacy)
			if a.config.OnToolStart != nil {
				a.config.OnToolStart(call)
			}

			// CB-T23: Emit OnToolCallStartEvent
			ai.Notify(ctx, ai.OnToolCallStartEvent{
				ToolCallID:          call.ID,
				ToolName:            call.ToolName,
				Args:                call.Arguments,
				StepNumber:          stepNum,
				ModelProvider:       a.config.Model.Provider(),
				ModelID:             a.config.Model.ModelID(),
				ExperimentalContext: a.config.ExperimentalContext,
				RuntimeContext:      runtimeContext,
				ToolsContext:        toolsContext,
			}, cbs.onToolCallStart)

			execOptions := types.ToolExecutionOptions{
				ToolCallID:     call.ID,
				UserContext:    runtimeContext,
				RuntimeContext: runtimeContext,
				ToolContext:    toolContext,
			}
			startMs := time.Now().UnixMilli()
			toolResult, toolErr := tool.Execute(ctx, call.Arguments, execOptions)
			durationMs := time.Now().UnixMilli() - startMs

			result := types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Input:            call.Arguments,
				Result:           toolResult,
				Error:            toolErr,
				ProviderExecuted: false,
				ProviderMetadata: providerMetadata,
			}
			results = append(results, result)

			// CB-T23: Emit OnToolCallFinishEvent
			ai.Notify(ctx, ai.OnToolCallFinishEvent{
				ToolCallID:          call.ID,
				ToolName:            call.ToolName,
				Args:                call.Arguments,
				Result:              toolResult,
				Error:               toolErr,
				DurationMs:          durationMs,
				StepNumber:          stepNum,
				ModelProvider:       a.config.Model.Provider(),
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
	}

	return results, nil
}

func (a *ToolLoopAgent) runtimeContext() interface{} {
	if a.config.RuntimeContext != nil {
		return a.config.RuntimeContext
	}
	return a.config.ExperimentalContext
}

func (a *ToolLoopAgent) resolveToolApproval(ctx context.Context, call types.ToolCall, tools []types.Tool, messages []types.Message, runtimeContext interface{}, toolsContext map[string]interface{}) types.ToolApprovalResult {
	approval := normalizeAgentToolApproval(call, tools, messages, a.config.ToolApproval, runtimeContext, toolsContext)
	if approval.Status != types.ToolApprovalStatusNotApplicable {
		return approval
	}
	for i := range tools {
		tool := &tools[i]
		if tool.Name != call.ToolName {
			continue
		}
		switch v := tool.NeedsApproval.(type) {
		case nil:
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
		case bool:
			if v {
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
			}
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
		case types.NeedsApprovalFunc:
			if v(ctx, call.Arguments) {
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
			}
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
		case types.ToolApprovalStatus:
			return normalizeAgentApprovalValue(v)
		case string:
			return normalizeAgentApprovalValue(types.ToolApprovalStatus(v))
		}
	}
	return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
}

func normalizeAgentToolApproval(call types.ToolCall, tools []types.Tool, messages []types.Message, cfg interface{}, runtimeCtx interface{}, toolsCtx map[string]interface{}) types.ToolApprovalResult {
	switch v := cfg.(type) {
	case nil:
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	case types.ToolApprovalFunc:
		return normalizeAgentApprovalValue(v(call, tools, messages, runtimeCtx, toolsCtx))
	case map[string]interface{}:
		return normalizeAgentToolApprovalValue(call, tools, messages, v[call.ToolName], runtimeCtx, toolsCtx)
	case map[string]types.ToolApprovalValue:
		return normalizeAgentToolApprovalValue(call, tools, messages, v[call.ToolName], runtimeCtx, toolsCtx)
	default:
		return normalizeAgentApprovalValue(v)
	}
}

func normalizeAgentToolApprovalValue(call types.ToolCall, tools []types.Tool, messages []types.Message, value interface{}, runtimeCtx interface{}, toolsCtx map[string]interface{}) types.ToolApprovalResult {
	if fn, ok := value.(types.ToolApprovalFunc); ok {
		return normalizeAgentApprovalValue(fn(call, tools, messages, runtimeCtx, toolsCtx))
	}
	return normalizeAgentApprovalValue(value)
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
	if err := tool.ContextSchema.Validator().Validate(ctxValue); err != nil {
		return nil, providererrors.NewValidationErrorWithContext(
			ctxValue,
			fmt.Sprintf("invalid tool context for %s at schema path toolsContext.%s: %v", toolName, toolName, err),
			err,
			&providererrors.ValidationContext{Field: "toolsContext." + toolName, EntityName: "toolContext", EntityID: toolName},
		)
	}
	return ctxValue, nil
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
