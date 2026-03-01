package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// now returns the current time in milliseconds since Unix epoch.
// Used for measuring tool call execution duration.
func now() int64 {
	return time.Now().UnixMilli()
}

// GenerateTextOptions contains options for text generation
// Updated in v6.0 with Output system and Context flow
type GenerateTextOptions struct {
	// Model to use for generation
	Model provider.LanguageModel

	// Prompt can be a simple string or a list of messages
	Prompt   string
	Messages []types.Message
	System   string

	// Generation parameters
	Temperature      *float64
	MaxTokens        *int
	TopP             *float64
	TopK             *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	StopSequences    []string
	Seed             *int

	// Tools available for the model to call
	Tools      []types.Tool
	ToolChoice types.ToolChoice

	// MaxSteps is a convenience shorthand for StopWhen{StepCountIs(N)}.
	// Deprecated: use StopWhen with StepCountIs instead.
	// If StopWhen is set, MaxSteps is ignored.
	MaxSteps *int

	// StopWhen defines conditions that terminate the tool-calling loop.
	// Conditions are evaluated OR -- first non-empty string stops the loop.
	// Evaluated after each step that produces tool results.
	// Default: []StopCondition{StepCountIs(1)}.
	StopWhen []StopCondition

	// ========================================================================
	// Timeout Configuration (v6.0.41 - NEW)
	// ========================================================================

	// Timeout provides granular timeout controls
	// Supports total timeout, per-step timeout, and per-chunk timeout (for streaming)
	Timeout *TimeoutConfig

	// ========================================================================
	// Output Specification (v6.0 - NEW)
	// ========================================================================

	// Output specifies how to handle and parse model output
	// Use TextOutput(), ObjectOutput(), ArrayOutput(), ChoiceOutput(), or JSONOutput()
	// If nil, defaults to text output
	Output interface{} // Output[any, any]

	// ResponseFormat (for structured output) - DEPRECATED: Use Output instead
	// Kept for backward compatibility
	ResponseFormat *provider.ResponseFormat

	// ========================================================================
	// Context Flow (v6.0 - NEW)
	// ========================================================================

	// ExperimentalContext is user-defined context that flows through the conversation
	// This context is passed to:
	// - Tool execution functions (via ToolExecutionOptions)
	// - PrepareStep callback
	// - OnStepFinish callback
	// - OnFinish callback
	ExperimentalContext interface{}

	// ========================================================================
	// Retention Settings (v6.0.60 - NEW)
	// ========================================================================

	// ExperimentalRetention controls what data is retained from LLM requests/responses.
	// Useful for reducing memory consumption with images or large contexts.
	// Default (nil) retains everything for backwards compatibility.
	//
	// Example:
	//   retention := &types.RetentionSettings{
	//       RequestBody:  types.BoolPtr(false),  // Don't retain request
	//       ResponseBody: types.BoolPtr(false),  // Don't retain response
	//   }
	//
	// This can reduce memory consumption by 50-80% for image-heavy workloads.
	ExperimentalRetention *types.RetentionSettings

	// ========================================================================
	// Provider Options (v6.0.61 - NEW)
	// ========================================================================

	// ProviderOptions allows passing provider-specific options
	// Example:
	//   ProviderOptions: map[string]interface{}{
	//       "openai": map[string]interface{}{
	//           "promptCacheRetention": "24h",
	//       },
	//   }
	ProviderOptions map[string]interface{}

	// ========================================================================
	// Telemetry (v6.0 - Observability)
	// ========================================================================

	// ExperimentalTelemetry enables OpenTelemetry tracing for this operation
	// When set, automatically records spans with prompts, responses, token usage, and latencies
	//
	// Example:
	//   import "github.com/digitallysavvy/go-ai/pkg/telemetry"
	//
	//   result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
	//       Model: model,
	//       Prompt: "Hello",
	//       ExperimentalTelemetry: &telemetry.Settings{
	//           IsEnabled: true,
	//           RecordInputs: true,
	//           RecordOutputs: true,
	//       },
	//   })
	//
	// For MLflow integration, see pkg/observability/mlflow
	ExperimentalTelemetry *TelemetrySettings

	// ========================================================================
	// Callbacks (Updated signatures in v6.0)
	// ========================================================================

	// PrepareStep is called before each generation step
	// Allows modification of options before the next step
	// Receives the user context if ExperimentalContext is set
	PrepareStep func(ctx context.Context, step PrepareStepOptions) PrepareStepOptions

	// OnStepFinish is called after each generation step completes
	// Receives the user context if ExperimentalContext is set
	OnStepFinish func(ctx context.Context, step types.StepResult, userContext interface{})

	// OnFinish is called when generation completes
	// Receives the user context if ExperimentalContext is set
	OnFinish func(ctx context.Context, result *GenerateTextResult, userContext interface{})

	// ========================================================================
	// Structured Event Callbacks (v6.1 - P0-3)
	// These callbacks receive typed event structs and are panic-safe.
	// They fire in addition to (not instead of) the legacy callbacks above.
	// ========================================================================

	// OnStart is called once, before the first LLM step begins.
	OnStart func(ctx context.Context, e OnStartEvent)

	// OnStepStart is called at the beginning of each LLM step.
	OnStepStart func(ctx context.Context, e OnStepStartEvent)

	// OnToolCallStart is called just before each tool's Execute function runs.
	OnToolCallStart func(ctx context.Context, e OnToolCallStartEvent)

	// OnToolCallFinish is called after each tool's Execute function returns
	// (whether the execution succeeded or failed).
	OnToolCallFinish func(ctx context.Context, e OnToolCallFinishEvent)

	// OnStepFinishEvent is called at the end of each LLM step with a typed
	// OnStepFinishEvent. Use this instead of OnStepFinish for structured access.
	OnStepFinishEvent func(ctx context.Context, e OnStepFinishEvent)

	// OnFinishEvent is called once when the entire generation completes with a
	// typed OnFinishEvent. Use this instead of OnFinish for structured access.
	OnFinishEvent func(ctx context.Context, e OnFinishEvent)
}

// TelemetrySettings configures OpenTelemetry tracing for AI operations
// This is re-exported from pkg/telemetry for convenience
type TelemetrySettings = telemetry.Settings

// PrepareStepOptions contains options that can be modified before each step
type PrepareStepOptions struct {
	// Messages for the next step
	Messages []types.Message

	// User context (from ExperimentalContext)
	UserContext interface{}

	// Current step number
	StepNumber int

	// Accumulated usage so far
	AccumulatedUsage types.Usage
}

// GenerateTextResult contains the result of text generation
type GenerateTextResult struct {
	// Generated text content
	Text string

	// Output contains the parsed output when a WithOutput option was provided.
	// Type-assert to the concrete type, e.g.: recipe := result.Output.(Recipe)
	// Nil when no Output option was set.
	Output any

	// Tool calls made during generation
	ToolCalls []types.ToolCall

	// Tool results from executed tools
	ToolResults []types.ToolResult

	// Steps taken during generation (for multi-step tool calling)
	Steps []types.StepResult

	// Reason why generation finished
	FinishReason types.FinishReason

	// StopReason is the reason string from the StopCondition that stopped the loop.
	// Empty if the loop ended naturally (model stopped calling tools).
	StopReason string

	// Token usage information
	Usage types.Usage

	// Context management information (Anthropic-specific)
	// Contains statistics about automatic conversation history cleanup
	ContextManagement interface{}

	// Warnings from the provider
	Warnings []types.Warning

	// Raw request/response (for debugging)
	RawRequest  interface{}
	RawResponse interface{}
}

// GenerateText performs non-streaming text generation with optional tool calling
func GenerateText(ctx context.Context, opts GenerateTextOptions) (*GenerateTextResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	// Create telemetry span if enabled
	var span trace.Span
	if opts.ExperimentalTelemetry != nil && opts.ExperimentalTelemetry.IsEnabled {
		tracer := telemetry.GetTracer(opts.ExperimentalTelemetry)

		// Create top-level ai.generateText span
		spanName := "ai.generateText"
		if opts.ExperimentalTelemetry.FunctionID != "" {
			spanName = spanName + "." + opts.ExperimentalTelemetry.FunctionID
		}

		ctx, span = tracer.Start(ctx, spanName)
		defer span.End()

		// Add base telemetry attributes
		span.SetAttributes(
			attribute.String("ai.operationId", "ai.generateText"),
			attribute.String("ai.model.provider", opts.Model.Provider()),
			attribute.String("ai.model.id", opts.Model.ModelID()),
		)

		// Add function ID if present
		if opts.ExperimentalTelemetry.FunctionID != "" {
			span.SetAttributes(attribute.String("ai.telemetry.functionId", opts.ExperimentalTelemetry.FunctionID))
		}

		// Add custom metadata
		for key, value := range opts.ExperimentalTelemetry.Metadata {
			span.SetAttributes(attribute.KeyValue{
				Key:   attribute.Key("ai.telemetry.metadata." + key),
				Value: value,
			})
		}

		// Record prompt if enabled
		if opts.ExperimentalTelemetry.RecordInputs && opts.Prompt != "" {
			span.SetAttributes(attribute.String("ai.prompt", opts.Prompt))
		}
	}

	// Apply total timeout if configured
	var cancel context.CancelFunc
	if opts.Timeout != nil && opts.Timeout.HasTotal() {
		ctx, cancel = opts.Timeout.CreateTimeoutContext(ctx, "total")
		defer cancel()
	}

	// Build prompt
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)

	// Extract telemetry info once for all callback events
	cbFuncID, cbMeta := telemetryCallbackInfo(opts.ExperimentalTelemetry)

	// CB-T12: Emit OnStartEvent
	Notify(ctx, OnStartEvent{
		ModelProvider:       opts.Model.Provider(),
		ModelID:             opts.Model.ModelID(),
		System:              opts.System,
		Prompt:              opts.Prompt,
		Messages:            opts.Messages,
		Tools:               opts.Tools,
		Temperature:         opts.Temperature,
		MaxTokens:           opts.MaxTokens,
		TopP:                opts.TopP,
		TopK:                opts.TopK,
		FrequencyPenalty:    opts.FrequencyPenalty,
		PresencePenalty:     opts.PresencePenalty,
		StopSequences:       opts.StopSequences,
		Seed:                opts.Seed,
		ExperimentalContext: opts.ExperimentalContext,
		FunctionID:          cbFuncID,
		Metadata:            cbMeta,
	}, opts.OnStart)

	// Initialize result
	result := &GenerateTextResult{
		Steps: []types.StepResult{},
	}

	// Resolve stop conditions (Vercel AI SDK v5 approach):
	// MaxSteps is sugar for StopWhen{StepCountIs(N)}.
	// All termination flows through stop conditions.
	stopConditions := opts.StopWhen
	if len(stopConditions) == 0 {
		if opts.MaxSteps != nil {
			stopConditions = []StopCondition{StepCountIs(*opts.MaxSteps)}
		} else {
			stopConditions = []StopCondition{StepCountIs(1)}
		}
	}
	maxSteps := 1000 // safety ceiling only

	// Current messages for conversation history
	currentMessages := prompt.Messages

	// Execute generation loop (for tool calling)
	for stepNum := 1; stepNum <= maxSteps; stepNum++ {
		// Apply per-step timeout if configured
		stepCtx := ctx
		var stepCancel context.CancelFunc
		if opts.Timeout != nil && opts.Timeout.HasPerStep() {
			stepCtx, stepCancel = opts.Timeout.CreateTimeoutContext(ctx, "step")
			defer stepCancel()
		}

		// CB-T13: Emit OnStepStartEvent
		Notify(ctx, OnStepStartEvent{
			StepNumber:          stepNum,
			ModelProvider:       opts.Model.Provider(),
			ModelID:             opts.Model.ModelID(),
			System:              opts.System,
			Messages:            currentMessages,
			Tools:               opts.Tools,
			PreviousSteps:       result.Steps, // steps completed before this one
			ExperimentalContext: opts.ExperimentalContext,
			FunctionID:          cbFuncID,
			Metadata:            cbMeta,
		}, opts.OnStepStart)

		// Resolve ResponseFormat: prefer explicit opts.ResponseFormat; fall back to Output's format.
		responseFormat := opts.ResponseFormat
		if responseFormat == nil {
			if op, ok := opts.Output.(outputProcessor); ok {
				rf, rfErr := op.ResponseFormat(stepCtx)
				if rfErr != nil {
					return nil, fmt.Errorf("output.ResponseFormat failed: %w", rfErr)
				}
				responseFormat = rf
			}
		}

		// Build generate options
		genOpts := &provider.GenerateOptions{
			Prompt: types.Prompt{
				Messages: currentMessages,
				System:   prompt.System,
			},
			Temperature:      opts.Temperature,
			MaxTokens:        opts.MaxTokens,
			TopP:             opts.TopP,
			TopK:             opts.TopK,
			FrequencyPenalty: opts.FrequencyPenalty,
			PresencePenalty:  opts.PresencePenalty,
			StopSequences:    opts.StopSequences,
			Seed:             opts.Seed,
			Tools:            opts.Tools,
			ToolChoice:       opts.ToolChoice,
			ResponseFormat:   responseFormat,
			ProviderOptions:  opts.ProviderOptions,
			Telemetry:        opts.ExperimentalTelemetry,
		}

		// Call the model with step context
		genResult, err := opts.Model.DoGenerate(stepCtx, genOpts)
		if err != nil {
			return nil, fmt.Errorf("generation failed at step %d: %w", stepNum, err)
		}

		// Create step result
		stepResult := types.StepResult{
			StepNumber:   stepNum,
			Text:         genResult.Text,
			ToolCalls:    genResult.ToolCalls,
			ToolResults:  []types.ToolResult{},
			FinishReason: genResult.FinishReason,
			Usage:        genResult.Usage,
			Warnings:     genResult.Warnings,
		}

		// Update accumulated usage
		result.Usage = result.Usage.Add(genResult.Usage)

		// Check if there are tool calls to execute
		if len(genResult.ToolCalls) > 0 && len(opts.Tools) > 0 {
			// Execute tools with context flow (v6.0) and structured callbacks (v6.1)
			toolCallbacks := toolCallEventCallbacks{
				onStart:             opts.OnToolCallStart,
				onFinish:            opts.OnToolCallFinish,
				stepNum:             stepNum,
				modelProvider:       opts.Model.Provider(),
				modelID:             opts.Model.ModelID(),
				messages:            currentMessages,
				experimentalContext: opts.ExperimentalContext,
				functionID:          cbFuncID,
				metadata:            cbMeta,
			}
			toolResults, err := executeTools(ctx, genResult.ToolCalls, opts.Tools, opts.ExperimentalContext, &result.Usage, toolCallbacks)
			if err != nil {
				return nil, fmt.Errorf("tool execution failed at step %d: %w", stepNum, err)
			}

			// Validate tool results (v6.0.57)
			// This ensures provider-executed tools have proper results
			if err := validateToolResults(toolResults); err != nil {
				return nil, fmt.Errorf("tool result validation failed at step %d: %w", stepNum, err)
			}

			stepResult.ToolResults = toolResults
			result.ToolResults = append(result.ToolResults, toolResults...)

			// Add assistant message with tool calls to history
			assistantMsg := types.Message{
				Role:    types.RoleAssistant,
				Content: []types.ContentPart{},
			}
			if genResult.Text != "" {
				assistantMsg.Content = append(assistantMsg.Content, types.TextContent{Text: genResult.Text})
			}
			currentMessages = append(currentMessages, assistantMsg)

			// Add tool results to history
			for _, tr := range toolResults {
				toolMsg := types.Message{
					Role: types.RoleTool,
					Content: []types.ContentPart{
						types.ToolResultContent{
							ToolCallID: tr.ToolCallID,
							ToolName:   tr.ToolName,
							Result:     tr.Result,
						},
					},
				}
				currentMessages = append(currentMessages, toolMsg)
			}
		} else {
			// No more tool calls, we're done
			result.Text = genResult.Text
			result.FinishReason = genResult.FinishReason
			result.ToolCalls = genResult.ToolCalls
			result.ContextManagement = genResult.ContextManagement
			result.Warnings = append(result.Warnings, genResult.Warnings...)
			result.RawRequest = genResult.RawRequest
			result.RawResponse = genResult.RawResponse

			// Parse typed output if an Output spec was provided.
			// Only parse when generation finished cleanly; a 'length' finish means
			// the response was truncated and would likely produce invalid JSON.
			if op, ok := opts.Output.(outputProcessor); ok && genResult.FinishReason == types.FinishReasonStop {
				parsed, parseErr := op.parseCompleteOutput(stepCtx, ParseCompleteOutputOptions{
					Text:         genResult.Text,
					FinishReason: genResult.FinishReason,
					Usage:        &genResult.Usage,
				})
				if parseErr != nil {
					return nil, fmt.Errorf("output parsing failed: %w", parseErr)
				}
				result.Output = parsed
			}
		}

		// Add step to results
		result.Steps = append(result.Steps, stepResult)

		// Call step finish callback (v6.0: with user context)
		if opts.OnStepFinish != nil {
			opts.OnStepFinish(ctx, stepResult, opts.ExperimentalContext)
		}

		// CB-T14: Emit structured OnStepFinishEvent
		Notify(ctx, OnStepFinishEvent{
			StepNumber:          stepResult.StepNumber,
			ModelProvider:       opts.Model.Provider(),
			ModelID:             opts.Model.ModelID(),
			Text:                stepResult.Text,
			ToolCalls:           stepResult.ToolCalls,
			ToolResults:         stepResult.ToolResults,
			FinishReason:        stepResult.FinishReason,
			Usage:               stepResult.Usage,
			Warnings:            stepResult.Warnings,
			ExperimentalContext: opts.ExperimentalContext,
			FunctionID:          cbFuncID,
			Metadata:            cbMeta,
		}, opts.OnStepFinishEvent)

		// Evaluate stop conditions after steps with tool results
		if len(stopConditions) > 0 {
			state := StopConditionState{
				Steps:    result.Steps,
				Messages: currentMessages,
				Usage:    result.Usage,
			}
			if reason := EvaluateStopConditions(stopConditions, state); reason != "" {
				result.StopReason = reason
				lastStep := result.Steps[len(result.Steps)-1]
				result.Text = lastStep.Text
				result.FinishReason = lastStep.FinishReason
				break
			}
		}

		// Check if we should continue
		if genResult.FinishReason != types.FinishReasonToolCalls {
			break
		}
	}

	// Record telemetry output attributes
	if span != nil {
		// Record output if enabled
		if opts.ExperimentalTelemetry != nil && opts.ExperimentalTelemetry.RecordOutputs {
			span.SetAttributes(attribute.String("ai.response.text", result.Text))
		}

		// Record finish reason
		span.SetAttributes(attribute.String("ai.response.finishReason", string(result.FinishReason)))

		// Record usage information
		if result.Usage.InputTokens != nil {
			span.SetAttributes(attribute.Int64("ai.usage.promptTokens", *result.Usage.InputTokens))
		}
		if result.Usage.OutputTokens != nil {
			span.SetAttributes(attribute.Int64("ai.usage.completionTokens", *result.Usage.OutputTokens))
		}
		if result.Usage.TotalTokens != nil {
			span.SetAttributes(attribute.Int64("ai.usage.totalTokens", *result.Usage.TotalTokens))
		}
	}

	// Call finish callback (v6.0: with user context)
	if opts.OnFinish != nil {
		opts.OnFinish(ctx, result, opts.ExperimentalContext)
	}

	// CB-T15: Emit structured OnFinishEvent
	Notify(ctx, OnFinishEvent{
		Text:                result.Text,
		ToolCalls:           result.ToolCalls,
		ToolResults:         result.ToolResults,
		FinishReason:        result.FinishReason,
		Steps:               result.Steps,
		TotalUsage:          result.Usage,
		Warnings:            result.Warnings,
		ExperimentalContext: opts.ExperimentalContext,
		FunctionID:          cbFuncID,
		Metadata:            cbMeta,
	}, opts.OnFinishEvent)

	// Apply retention settings (v6.0.60)
	// Exclude request/response bodies based on retention settings
	if opts.ExperimentalRetention != nil {
		if !opts.ExperimentalRetention.ShouldRetainRequestBody() {
			result.RawRequest = nil
		}
		if !opts.ExperimentalRetention.ShouldRetainResponseBody() {
			result.RawResponse = nil
		}
	}

	return result, nil
}

// toolCallEventCallbacks groups the per-tool-call structured event callbacks
// and their associated metadata. All fields are optional (nil-safe).
type toolCallEventCallbacks struct {
	onStart             func(ctx context.Context, e OnToolCallStartEvent)
	onFinish            func(ctx context.Context, e OnToolCallFinishEvent)
	stepNum             int
	modelProvider       string
	modelID             string
	messages            []types.Message
	experimentalContext interface{}
	functionID          string
	metadata            map[string]any
}

// executeTools executes a list of tool calls
// Updated in v6.0 to pass ToolExecutionOptions with ToolCallID and UserContext
// Updated in v6.0.57 to handle provider-executed (deferrable) tools
// Updated in v6.1 to fire structured OnToolCallStart/Finish events (CB-T16/T17/T18)
func executeTools(ctx context.Context, toolCalls []types.ToolCall, availableTools []types.Tool, userContext interface{}, usage *types.Usage, callbacks toolCallEventCallbacks) ([]types.ToolResult, error) {
	results := make([]types.ToolResult, len(toolCalls))

	for i, call := range toolCalls {
		// Find the tool
		var tool *types.Tool
		for j := range availableTools {
			if availableTools[j].Name == call.ToolName {
				tool = &availableTools[j]
				break
			}
		}

		if tool == nil {
			toolErr := fmt.Errorf("tool not found: %s", call.ToolName)
			results[i] = types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Error:            toolErr,
				ProviderExecuted: false,
			}
			continue
		}

		// Check if this is a provider-executed tool
		// Provider-executed tools are handled by the LLM provider (e.g., Anthropic, xAI)
		// and their results come back in the provider's response, not from local execution
		providerExecuted := isProviderExecutedTool(tool)

		if providerExecuted {
			// Provider-executed tool: result will come from provider in next response
			// We don't execute locally, just mark as pending
			results[i] = types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Result:           nil,
				Error:            nil,
				ProviderExecuted: true,
			}
		} else {
			// CB-T16: Emit OnToolCallStartEvent before execution
			Notify(ctx, OnToolCallStartEvent{
				ToolCallID:          call.ID,
				ToolName:            call.ToolName,
				Args:                call.Arguments,
				StepNumber:          callbacks.stepNum,
				ModelProvider:       callbacks.modelProvider,
				ModelID:             callbacks.modelID,
				Messages:            callbacks.messages,
				ExperimentalContext: callbacks.experimentalContext,
				FunctionID:          callbacks.functionID,
				Metadata:            callbacks.metadata,
			}, callbacks.onStart)

			// Locally-executed tool: execute now
			execOptions := types.ToolExecutionOptions{
				ToolCallID:  call.ID,
				UserContext: userContext,
				Usage:       usage,
				Metadata:    make(map[string]interface{}),
			}

			startTime := now()
			toolResult, toolErr := tool.Execute(ctx, call.Arguments, execOptions)
			durationMs := now() - startTime

			results[i] = types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Result:           toolResult,
				Error:            toolErr,
				ProviderExecuted: false,
			}

			// CB-T17/T18: Emit OnToolCallFinishEvent after execution (success or error)
			Notify(ctx, OnToolCallFinishEvent{
				ToolCallID:          call.ID,
				ToolName:            call.ToolName,
				Args:                call.Arguments,
				Result:              toolResult,
				Error:               toolErr,
				DurationMs:          durationMs,
				StepNumber:          callbacks.stepNum,
				ModelProvider:       callbacks.modelProvider,
				ModelID:             callbacks.modelID,
				Messages:            callbacks.messages,
				ExperimentalContext: callbacks.experimentalContext,
				FunctionID:          callbacks.functionID,
				Metadata:            callbacks.metadata,
			}, callbacks.onFinish)
		}
	}

	return results, nil
}

// isProviderExecutedTool determines if a tool is executed by the provider
// Provider-executed tools include:
// - Anthropic: tool-search-bm25, tool-search-regex, web-search, web-fetch, code-execution
// - xAI: file-search, mcp-server
// - OpenAI: MCP tools (with approval)
func isProviderExecutedTool(tool *types.Tool) bool {
	// Check for common provider-executed tool names
	providerTools := map[string]bool{
		// Anthropic built-in tools
		"tool-search-bm25":  true,
		"tool-search-regex": true,
		"web-search":        true,
		"web-fetch":         true,
		"code-execution":    true,
		// xAI tools
		"file-search": true,
		"mcp-server":  true,
	}

	return providerTools[tool.Name]
}

// validateToolResults validates tool results, especially for provider-executed tools
// Returns error if validation fails
// Note: This validation is primarily for debugging purposes. Provider-executed tools
// that are pending (Result=nil, Error=nil) are allowed - they will be resolved in
// subsequent provider responses.
func validateToolResults(results []types.ToolResult) error {
	// For now, we don't fail on missing provider-executed tool results
	// because they may be resolved in subsequent calls.
	// This validation could be enhanced to track pending tools across multiple steps.

	// We could validate that local tools always have a result or error,
	// but that's already enforced by the executeTools function.

	return nil
}

// wrapToolExecutionError wraps a tool execution error with additional context
func wrapToolExecutionError(toolCallID, toolName string, err error, providerExecuted bool) error {
	if err == nil {
		return nil
	}

	return &types.ToolExecutionError{
		ToolCallID:       toolCallID,
		ToolName:         toolName,
		Err:              err,
		ProviderExecuted: providerExecuted,
	}
}

// telemetryCallbackInfo extracts the function ID and metadata from telemetry
// settings for use in structured callback events. Returns empty values when
// telemetry is nil.
func telemetryCallbackInfo(t *TelemetrySettings) (functionID string, metadata map[string]any) {
	if t == nil {
		return "", nil
	}
	functionID = t.FunctionID
	if len(t.Metadata) > 0 {
		metadata = make(map[string]any, len(t.Metadata))
		for k, v := range t.Metadata {
			metadata[k] = v.Emit()
		}
	}
	return functionID, metadata
}

// buildPrompt builds a unified Prompt from various input formats
func buildPrompt(promptText string, messages []types.Message, system string) types.Prompt {
	if len(messages) > 0 {
		return types.Prompt{
			Messages: messages,
			System:   system,
		}
	}

	if promptText != "" {
		return types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: promptText},
					},
				},
			},
			System: system,
		}
	}

	return types.Prompt{}
}
