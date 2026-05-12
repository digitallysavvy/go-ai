package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
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

	// AllowSystemMessages permits system-role messages in Messages.
	// Defaults to false; use System for system instructions unless you are
	// intentionally passing provider-native system messages.
	AllowSystemMessages bool

	// AllowSystemInMessages is a TypeScript-compatible alias for
	// AllowSystemMessages. Either field enables system-role messages.
	AllowSystemInMessages bool

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

	// ToolApproval configures approval handling for tool execution.
	ToolApproval types.ToolApprovalConfig

	// MaxSteps is a convenience shorthand for StopWhen{StepCountIs(N)}.
	// Deprecated: use StopWhen with StepCountIs instead.
	// If StopWhen is set, MaxSteps is ignored.
	MaxSteps *int

	// StopWhen defines conditions that terminate the tool-calling loop.
	// Conditions are evaluated OR -- first non-empty string stops the loop.
	// Evaluated after each step that produces tool results.
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

	// RuntimeContext is user-defined context that flows through the conversation.
	// This context is passed to tool execution functions, callbacks, and approval hooks.
	RuntimeContext interface{}

	// ToolsContext is the per-tool context map passed to approval and execution hooks.
	ToolsContext map[string]interface{}

	// ExperimentalContext is a deprecated alias for RuntimeContext.
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
	// Reasoning (v6.1)
	// ========================================================================

	// Reasoning controls how much thinking effort the model applies.
	// nil means unset (use provider default). Set to types.ReasoningDefault to
	// explicitly omit from the API request. Providers map this to their native
	// reasoning APIs (Anthropic: thinking.budget_tokens, OpenAI: reasoning_effort,
	// Google: thinkingConfig.thinkingBudget, Bedrock: reasoningConfig).
	Reasoning *types.ReasoningLevel

	// SendReasoning controls whether reasoning/thinking stream boundary chunks
	// are exposed to callbacks. nil and false suppress reasoning-start/end,
	// matching the TypeScript SDK default.
	SendReasoning *bool

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

	// Telemetry configures observability for this operation.
	// When both Telemetry and ExperimentalTelemetry are set, Telemetry wins.
	Telemetry *TelemetrySettings

	// ExperimentalTelemetry enables OpenTelemetry tracing for this operation
	//
	// Deprecated: use Telemetry.
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
	// Structured Event Callbacks (v6.1)
	// These callbacks receive typed event structs and are panic-safe.
	// They fire in addition to (not instead of) the legacy callbacks above.
	// ========================================================================

	// OnStart is called once, before the first LLM step begins.
	OnStart func(ctx context.Context, e OnStartEvent)

	// OnStepStart is called at the beginning of each LLM step.
	OnStepStart func(ctx context.Context, e OnStepStartEvent)

	// OnToolExecutionStart is called just before each tool's Execute function runs.
	OnToolExecutionStart func(ctx context.Context, e OnToolCallStartEvent)

	// OnToolExecutionEnd is called after each tool's Execute function returns
	// (whether the execution succeeded or failed).
	OnToolExecutionEnd func(ctx context.Context, e OnToolCallFinishEvent)

	// Deprecated: use OnToolExecutionStart.
	OnToolCallStart func(ctx context.Context, e OnToolCallStartEvent)

	// Deprecated: use OnToolExecutionEnd.
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

// TelemetryOptions is the stable telemetry option type.
type TelemetryOptions = telemetry.Options

func effectiveTelemetrySettings(stable, experimental *TelemetrySettings) *TelemetrySettings {
	if stable != nil {
		return stable
	}
	return experimental
}

func allowSystemMessages(allowSystemMessages, allowSystemInMessages bool) bool {
	return allowSystemMessages || allowSystemInMessages
}

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

// GenerateTextResult contains the result of text generation.
type GenerateTextResult struct {
	// Generated text content
	Text string

	// Reasoning holds the reasoning/thinking content from the final step.
	Reasoning []types.ReasoningContent

	// ReasoningText is the concatenated reasoning text from the final step.
	ReasoningText string

	// Output contains the parsed output when a WithOutput option was provided.
	// Type-assert to the concrete type, e.g.: recipe := result.Output.(Recipe)
	// Nil when no Output option was set.
	Output any

	// Tool calls made during the final step
	ToolCalls []types.ToolCall

	// StaticToolCalls are tool calls from non-dynamic (typed) tools in the final step.
	StaticToolCalls []types.ToolCall

	// DynamicToolCalls are tool calls from dynamically registered tools in the final step.
	DynamicToolCalls []types.ToolCall

	// Tool results from the final step
	ToolResults []types.ToolResult

	// StaticToolResults are results from non-dynamic (typed) tools in the final step.
	StaticToolResults []types.ToolResult

	// DynamicToolResults are results from dynamically registered tools in the final step.
	DynamicToolResults []types.ToolResult

	// Steps taken during generation (for multi-step tool calling)
	Steps []types.StepResult

	// Reason why generation finished
	FinishReason types.FinishReason

	// RawFinishReason is the raw finish reason string from the provider.
	RawFinishReason string

	// StopReason is the reason string from the StopCondition that stopped the loop.
	// Empty if the loop ended naturally (model stopped calling tools).
	StopReason string

	// Token usage information (last step)
	Usage types.Usage

	// Context management information (Anthropic-specific)
	ContextManagement interface{}

	// Warnings from the provider
	Warnings []types.Warning

	// ProviderMetadata holds provider-specific metadata from the last generation step.
	ProviderMetadata map[string]interface{}

	// Sources contains citation or grounding references from the final generation step.
	Sources []types.SourceContent

	// Files contains model-generated output files (e.g. images, audio) from the final step.
	Files []types.GeneratedFileContent

	// TotalUsage is the sum of token usage across all steps.
	// For single-step generation, TotalUsage == Usage.
	TotalUsage types.Usage

	// Request contains metadata about the last request sent to the provider.
	Request types.StepRequest

	// Response contains metadata about the last response from the provider.
	Response types.StepResponse

	// Raw request/response (for debugging). Deprecated: use Request.Body and Response.Body.
	RawRequest  interface{}
	RawResponse interface{}

	// ResponseHeaders are the raw HTTP response headers from the provider.
	// Deprecated: use Response.Headers instead.
	ResponseHeaders map[string]string
}

// GenerateText performs non-streaming text generation with optional tool calling
func GenerateText(ctx context.Context, opts GenerateTextOptions) (result *GenerateTextResult, err error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	telemetrySettings := effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)
	runtimeContext := effectiveRuntimeContext(opts.RuntimeContext, opts.ExperimentalContext)
	toolsContext := opts.ToolsContext
	if toolsContext == nil {
		toolsContext = map[string]interface{}{}
	}

	// Fire OnStart — registered integrations start their root spans here and
	// embed them in the returned context.  When no integration is registered
	// the fire function is a no-op.
	telPrompt := ""
	telSystem := ""
	if telemetrySettings != nil && telemetrySettings.RecordInputs {
		telPrompt = opts.Prompt
		telSystem = opts.System
	}
	ctx = telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
		OperationType:  "ai.generateText",
		ModelProvider:  opts.Model.Provider(),
		ModelID:        opts.Model.ModelID(),
		Settings:       telemetrySettings,
		Prompt:         telPrompt,
		System:         telSystem,
		RuntimeContext: telemetryRuntimeContext(telemetrySettings, runtimeContext),
		ToolsContext:   telemetryToolsContext(telemetrySettings, toolsContext),
	})

	// Ensure telemetry is always closed — OnError ends the span on failure,
	// OnFinish ends it on success.
	defer func() {
		if err != nil {
			telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: err})
		}
	}()

	// Apply total timeout if configured
	var cancel context.CancelFunc
	if opts.Timeout != nil && opts.Timeout.HasTotal() {
		ctx, cancel = opts.Timeout.CreateTimeoutContext(ctx, "total")
		defer cancel()
	}

	// Build prompt
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)
	allowSystem := allowSystemMessages(opts.AllowSystemMessages, opts.AllowSystemInMessages)
	prompt, err = promptutils.NormalizePrompt(prompt, allowSystem)
	if err != nil {
		return nil, err
	}
	// Extract telemetry info once for all callback events
	cbFuncID, cbMeta := telemetryCallbackInfo(telemetrySettings)
	callID := newCallID()

	// Emit OnStartEvent.
	Notify(ctx, OnStartEvent{
		CallID:              callID,
		OperationID:         "ai.generateText",
		ModelProvider:       opts.Model.Provider(),
		ModelID:             opts.Model.ModelID(),
		System:              opts.System,
		Prompt:              opts.Prompt,
		Messages:            opts.Messages,
		Tools:               opts.Tools,
		ToolChoice:          opts.ToolChoice,
		Output:              opts.Output,
		ProviderOptions:     opts.ProviderOptions,
		Temperature:         opts.Temperature,
		MaxTokens:           opts.MaxTokens,
		TopP:                opts.TopP,
		TopK:                opts.TopK,
		FrequencyPenalty:    opts.FrequencyPenalty,
		PresencePenalty:     opts.PresencePenalty,
		StopSequences:       opts.StopSequences,
		Seed:                opts.Seed,
		ExperimentalContext: runtimeContext,
		RuntimeContext:      runtimeContext,
		ToolsContext:        toolsContext,
	}, opts.OnStart)

	// Initialize result (named return — assign, not declare)
	result = &GenerateTextResult{
		Steps: []types.StepResult{},
	}

	stopConditions := resolveStopConditions(opts.StopWhen, opts.MaxSteps)
	maxSteps := 1000 // safety ceiling only

	// Current messages for conversation history
	currentMessages := prompt.Messages

	// Build tool name → pointer map for deferred provider tool tracking.
	toolsByName := make(map[string]*types.Tool, len(opts.Tools))
	for i := range opts.Tools {
		toolsByName[opts.Tools[i].Name] = &opts.Tools[i]
	}
	// pendingDeferredToolCalls tracks provider tools whose results will arrive in
	// a subsequent response (SupportsDeferredResults=true). Key = toolCallID, value = toolName.
	pendingDeferredToolCalls := make(map[string]string)

	// Execute generation loop (for tool calling)
	for stepNum := 1; stepNum <= maxSteps; stepNum++ {
		// Apply per-step timeout if configured
		stepCtx := ctx
		var stepCancel context.CancelFunc
		if opts.Timeout != nil && opts.Timeout.HasPerStep() {
			stepCtx, stepCancel = opts.Timeout.CreateTimeoutContext(ctx, "step")
			defer stepCancel()
		}

		// Emit OnStepStartEvent.
		Notify(ctx, OnStepStartEvent{
			CallID:              callID,
			StepNumber:          stepNum,
			ModelProvider:       opts.Model.Provider(),
			ModelID:             opts.Model.ModelID(),
			System:              opts.System,
			Messages:            currentMessages,
			Tools:               opts.Tools,
			Steps:               result.Steps,
			PreviousSteps:       result.Steps, // deprecated alias
			ExperimentalContext: runtimeContext,
			RuntimeContext:      runtimeContext,
			ToolsContext:        toolsContext,
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
			AllowSystemMessages:   allowSystem,
			AllowSystemInMessages: allowSystem,
			Temperature:           opts.Temperature,
			MaxTokens:             opts.MaxTokens,
			TopP:                  opts.TopP,
			TopK:                  opts.TopK,
			FrequencyPenalty:      opts.FrequencyPenalty,
			PresencePenalty:       opts.PresencePenalty,
			StopSequences:         opts.StopSequences,
			Seed:                  opts.Seed,
			Tools:                 opts.Tools,
			ToolChoice:            opts.ToolChoice,
			RuntimeContext:        runtimeContext,
			ToolsContext:          toolsContext,
			ResponseFormat:        responseFormat,
			Reasoning:             opts.Reasoning,
			SendReasoning:         opts.SendReasoning,
			ProviderOptions:       opts.ProviderOptions,
			Telemetry:             telemetrySettings,
		}

		// Fire step-start telemetry. OTel implementations create a child step span
		// and embed it in the returned context so OnStepFinish can end it.
		stepCtx = telemetry.FireOnStepStart(stepCtx, telemetry.TelemetryStepStartEvent{
			OperationType:  "ai.generateText",
			Settings:       telemetrySettings,
			StepNumber:     stepNum,
			ModelProvider:  opts.Model.Provider(),
			ModelID:        opts.Model.ModelID(),
			RuntimeContext: telemetryRuntimeContext(telemetrySettings, runtimeContext),
			ToolsContext:   telemetryToolsContext(telemetrySettings, toolsContext),
		})

		telemetry.FireOnLanguageModelCallStart(stepCtx, telemetry.LanguageModelCallStartEvent{
			Settings:      telemetrySettings,
			CallID:        callID,
			ModelProvider: opts.Model.Provider(),
			ModelID:       opts.Model.ModelID(),
			Prompt:        genOpts.Prompt,
			Tools:         genOpts.Tools,
		})

		// Call the model with step context
		genResult, err := opts.Model.DoGenerate(stepCtx, genOpts)
		if err != nil {
			if opts.Timeout != nil {
				if opts.Timeout.HasPerStep() && stepCtx.Err() != nil {
					err = wrapTimeoutError(TimeoutReasonStep, err)
				} else if opts.Timeout.HasTotal() && ctx.Err() != nil {
					err = wrapTimeoutError(TimeoutReasonTotal, err)
				}
			}
			return nil, fmt.Errorf("generation failed at step %d: %w", stepNum, err)
		}

		modelCallUsage := telemetryUsageFromUsage(genResult.Usage)
		responseID := ""
		if meta := responseMetadataFromGenerateResult(opts.Model, genResult); meta != nil {
			responseID = meta.ID
		}
		telemetry.FireOnLanguageModelCallEnd(stepCtx, telemetry.LanguageModelCallEndEvent{
			Settings:      telemetrySettings,
			CallID:        callID,
			ModelProvider: opts.Model.Provider(),
			ModelID:       opts.Model.ModelID(),
			FinishReason:  string(genResult.FinishReason),
			Usage:         modelCallUsage,
			Content:       genResult.Content,
			ResponseID:    responseID,
		})

		// Extract sources, files, and reasoning from content parts
		var stepSources []types.SourceContent
		var stepFiles []types.GeneratedFileContent
		var stepReasoning []types.ReasoningContent
		for _, part := range genResult.Content {
			switch v := part.(type) {
			case types.SourceContent:
				stepSources = append(stepSources, v)
			case types.GeneratedFileContent:
				stepFiles = append(stepFiles, v)
			case types.ReasoningContent:
				stepReasoning = append(stepReasoning, v)
			}
		}
		stepReasoningText := buildReasoningText(stepReasoning)

		// Extract raw finish reason from RawResponse.
		var stepRawFinishReason string
		if genResult.RawResponse != nil {
			if respMap, ok := genResult.RawResponse.(map[string]interface{}); ok {
				if fr, ok := respMap["finish_reason"].(string); ok {
					stepRawFinishReason = fr
				}
			}
		}

		// Build response metadata for this step.
		stepResp := generateStepResponseFromGenerateResult(opts.Model, genResult)

		// Create step result
		stepResult := types.StepResult{
			CallID:          callID,
			StepNumber:      stepNum,
			Model:           types.StepModel{Provider: opts.Model.Provider(), ModelID: opts.Model.ModelID()},
			Text:            genResult.Text,
			Reasoning:       stepReasoning,
			ReasoningText:   stepReasoningText,
			Files:           stepFiles,
			ToolCalls:       genResult.ToolCalls,
			StaticToolCalls: filterStaticToolCalls(genResult.ToolCalls),
			DynamicToolCalls: filterDynamicToolCalls(genResult.ToolCalls),
			ToolResults:     []types.ToolResult{},
			FinishReason:    genResult.FinishReason,
			RawFinishReason: stepRawFinishReason,
			Usage:           genResult.Usage,
			Warnings:        genResult.Warnings,
			Sources:         stepSources,
			Request:         types.StepRequest{Body: genResult.RawRequest},
			Response: types.StepResponse{
				ID:        stepResp.ID,
				Timestamp: stepResp.Timestamp,
				ModelID:   stepResp.ModelID,
				Headers:   stepResp.Headers,
				Body:      stepResp.Body,
			},
			ProviderMetadata: genResult.ProviderMetadata,
			ToolsContext:     toolsContext,
			RuntimeContext:   runtimeContext,
		}

		// Update accumulated usage
		result.Usage = result.Usage.Add(genResult.Usage)

		hasUserApproval := false

		// Check if there are tool calls to execute
		if len(genResult.ToolCalls) > 0 && len(opts.Tools) > 0 {
			// Execute tools with context flow (v6.0) and structured callbacks (v6.1)
			toolCallbacks := toolCallEventCallbacks{
				callID:              callID,
				onStart:             opts.OnToolExecutionStart,
				onFinish:            opts.OnToolExecutionEnd,
				fallbackStart:       opts.OnToolCallStart,
				fallbackFinish:      opts.OnToolCallFinish,
				stepNum:             stepNum,
				modelProvider:       opts.Model.Provider(),
				modelID:             opts.Model.ModelID(),
				messages:            currentMessages,
				experimentalContext: runtimeContext,
				runtimeContext:      runtimeContext,
				toolsContext:        toolsContext,
				functionID:          cbFuncID,
				metadata:            cbMeta,
				timeout:             opts.Timeout,
				telemetrySettings:   telemetrySettings,
			}
			toolResults, err := executeTools(ctx, genResult.ToolCalls, opts.Tools, runtimeContext, toolsContext, opts.ToolApproval, &result.Usage, toolCallbacks)
			if err != nil {
				if opts.Timeout != nil && opts.Timeout.HasTotal() && ctx.Err() != nil {
					err = wrapTimeoutError(TimeoutReasonTotal, err)
				}
				return nil, fmt.Errorf("tool execution failed at step %d: %w", stepNum, err)
			}

			// Validate tool results (v6.0.57)
			// This ensures provider-executed tools have proper results
			if err := validateToolResults(toolResults); err != nil {
				return nil, fmt.Errorf("tool result validation failed at step %d: %w", stepNum, err)
			}

			stepResult.ToolResults = toolResults
			stepResult.StaticToolResults = filterStaticToolResults(toolResults)
			stepResult.DynamicToolResults = filterDynamicToolResults(toolResults)
			result.ToolResults = append(result.ToolResults, toolResults...)
			for _, tr := range toolResults {
				if tr.ApprovalStatus == types.ToolApprovalStatusUserApproval {
					hasUserApproval = true
					stepResult.FinishReason = types.FinishReasonUserApproval
				}
			}

			// Add assistant message with tool calls to history.
			// ToolCalls must be carried on the message so providers that require
			// a top-level tool_calls field (e.g. OpenAI) can emit it correctly.
			assistantMsg := providerutils.ConvertToResponseMessage(
				genResult.ToolCalls,
				[]types.ContentPart{types.TextContent{Text: genResult.Text}},
			)

			// Build response messages: assistant + tool results.
			stepResponseMsgs := providerutils.ConvertToResponseMessages(
				genResult.ToolCalls,
				assistantMsg.Content,
				toolResults,
			)
			currentMessages = append(currentMessages, stepResponseMsgs...)
			stepResult.ResponseMessages = stepResponseMsgs
			stepResult.Response.Messages = stepResponseMsgs
		} else {
			// No more tool calls, we're done
			result.Text = genResult.Text
			result.Reasoning = stepReasoning
			result.ReasoningText = stepReasoningText
			result.FinishReason = genResult.FinishReason
			result.RawFinishReason = stepRawFinishReason
			result.ToolCalls = genResult.ToolCalls
			result.StaticToolCalls = filterStaticToolCalls(genResult.ToolCalls)
			result.DynamicToolCalls = filterDynamicToolCalls(genResult.ToolCalls)
			result.Sources = stepSources
			result.Files = stepFiles
			result.ContextManagement = genResult.ContextManagement
			result.Warnings = append(result.Warnings, genResult.Warnings...)
			result.RawRequest = genResult.RawRequest
			result.RawResponse = genResult.RawResponse
			result.ResponseHeaders = genResult.ResponseHeaders
			result.ProviderMetadata = genResult.ProviderMetadata
			result.Request = types.StepRequest{Body: genResult.RawRequest}
			result.Response = types.StepResponse{
				ID:        stepResp.ID,
				Timestamp: stepResp.Timestamp,
				ModelID:   stepResp.ModelID,
				Headers:   stepResp.Headers,
				Body:      stepResp.Body,
			}

			// Build response messages for this (final) step.
			finalMsgs := providerutils.ConvertToResponseMessages(
				genResult.ToolCalls,
				[]types.ContentPart{types.TextContent{Text: genResult.Text}},
				nil,
			)
			stepResult.ResponseMessages = finalMsgs
			stepResult.Response.Messages = finalMsgs
			result.Response.Messages = finalMsgs

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

		// Deferred provider tool tracking (mirrors TS SDK pendingDeferredToolCalls).
		// Scan the current step's tool calls: if a provider tool with SupportsDeferredResults
		// did not return its result inline in this response, register it as pending so the
		// step loop continues even when FinishReason is not ToolCalls.
		// Note: we check tool.ProviderExecuted on the definition (not call.ProviderExecuted)
		// because some providers (e.g. Anthropic) do not set ProviderExecuted on ToolCalls.
		for _, call := range genResult.ToolCalls {
			tool := toolsByName[call.ToolName]
			if tool == nil || !tool.ProviderExecuted || !tool.SupportsDeferredResults {
				continue
			}
			hasResult := false
			for _, part := range genResult.Content {
				if tr, ok := part.(types.ToolResultContent); ok && tr.ToolCallID == call.ID {
					hasResult = true
					break
				}
			}
			if !hasResult {
				pendingDeferredToolCalls[call.ID] = call.ToolName
			}
		}
		// Remove entries resolved by tool-result parts in the current response.
		for _, part := range genResult.Content {
			if tr, ok := part.(types.ToolResultContent); ok {
				delete(pendingDeferredToolCalls, tr.ToolCallID)
			}
		}

		// Add step to results
		result.Steps = append(result.Steps, stepResult)

		// Call step finish callback (v6.0: with user context)
		if opts.OnStepFinish != nil {
			opts.OnStepFinish(ctx, stepResult, runtimeContext)
		}

		// Emit structured OnStepFinishEvent.
		Notify(ctx, OnStepFinishEvent{
			CallID:             callID,
			StepNumber:         stepResult.StepNumber,
			Model:              stepResult.Model,
			ModelProvider:      opts.Model.Provider(),
			ModelID:            opts.Model.ModelID(),
			Text:               stepResult.Text,
			Reasoning:          stepResult.Reasoning,
			ReasoningText:      stepResult.ReasoningText,
			ToolCalls:          stepResult.ToolCalls,
			StaticToolCalls:    stepResult.StaticToolCalls,
			DynamicToolCalls:   stepResult.DynamicToolCalls,
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
			ResponseHeaders:    genResult.ResponseHeaders,
			Request:            GenerateStepRequest{Body: genResult.RawRequest},
			Response: GenerateStepResponse{
				ID:       stepResult.Response.ID,
				Headers:  stepResult.Response.Headers,
				Messages: stepResult.ResponseMessages,
				Body:     genResult.RawResponse,
			},
			ExperimentalContext: runtimeContext,
			RuntimeContext:      runtimeContext,
			ToolsContext:        toolsContext,
		}, opts.OnStepFinishEvent)

		// Fire step-finish telemetry — OTel implementation ends the child step span.
		{
			stepTelUsage := telemetry.TelemetryUsage{
				InputTokens:  genResult.Usage.InputTokens,
				OutputTokens: genResult.Usage.OutputTokens,
				TotalTokens:  genResult.Usage.TotalTokens,
			}
			if genResult.Usage.InputDetails != nil {
				stepTelUsage.NoCacheInputTokens = genResult.Usage.InputDetails.NoCacheTokens
				stepTelUsage.CacheReadInputTokens = genResult.Usage.InputDetails.CacheReadTokens
				stepTelUsage.CacheCreationInputTokens = genResult.Usage.InputDetails.CacheWriteTokens
			}
			if genResult.Usage.OutputDetails != nil {
				stepTelUsage.OutputTextTokens = genResult.Usage.OutputDetails.TextTokens
				stepTelUsage.ReasoningTokens = genResult.Usage.OutputDetails.ReasoningTokens
			}
			var stepTelFiles []types.GeneratedFileContent
			var stepTelReasoning strings.Builder
			for _, part := range genResult.Content {
				switch v := part.(type) {
				case types.GeneratedFileContent:
					stepTelFiles = append(stepTelFiles, v)
				case types.ReasoningContent:
					if v.Text != "" {
						if stepTelReasoning.Len() > 0 {
							stepTelReasoning.WriteByte('\n')
						}
						stepTelReasoning.WriteString(v.Text)
					}
				}
			}
			telemetry.FireOnStepFinish(stepCtx, telemetry.TelemetryStepFinishEvent{
				StepNumber:       stepNum,
				FinishReason:     string(genResult.FinishReason),
				Usage:            stepTelUsage,
				Text:             genResult.Text,
				Reasoning:        stepTelReasoning.String(),
				ToolCalls:        genResult.ToolCalls,
				Files:            stepTelFiles,
				ProviderMetadata: genResult.ProviderMetadata,
				Settings:         telemetrySettings,
				RuntimeContext:   telemetryRuntimeContext(telemetrySettings, runtimeContext),
				ToolsContext:     telemetryToolsContext(telemetrySettings, toolsContext),
			})
		}

		if hasUserApproval {
			result.Text = stepResult.Text
			result.FinishReason = types.FinishReasonUserApproval
			break
		}

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

		// Check if we should continue.
		// Continue when there are local tool calls in the turn OR when a provider
		// tool with SupportsDeferredResults has not yet delivered its result.
		hasLocalToolCalls := false
		for _, call := range genResult.ToolCalls {
			tool := toolsByName[call.ToolName]
			if tool != nil && !tool.ProviderExecuted {
				hasLocalToolCalls = true
				break
			}
		}
		hasPendingDeferred := len(pendingDeferredToolCalls) > 0
		if !hasLocalToolCalls && !hasPendingDeferred {
			break
		}
	}

	// Fire OnFinish — integrations record output attributes and end their spans.
	telUsage := telemetry.TelemetryUsage{
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		TotalTokens:  result.Usage.TotalTokens,
	}
	if result.Usage.InputDetails != nil {
		telUsage.NoCacheInputTokens = result.Usage.InputDetails.NoCacheTokens
		telUsage.CacheReadInputTokens = result.Usage.InputDetails.CacheReadTokens
		telUsage.CacheCreationInputTokens = result.Usage.InputDetails.CacheWriteTokens
	}
	if result.Usage.OutputDetails != nil {
		telUsage.OutputTextTokens = result.Usage.OutputDetails.TextTokens
		telUsage.ReasoningTokens = result.Usage.OutputDetails.ReasoningTokens
	}
	telemetry.FireOnFinish(ctx, telemetry.TelemetryFinishEvent{
		FinishReason:   string(result.FinishReason),
		Usage:          telUsage,
		ModelProvider:  opts.Model.Provider(),
		ModelID:        opts.Model.ModelID(),
		Text:           result.Text,
		Files:          result.Files,
		Settings:       telemetrySettings,
		RuntimeContext: telemetryRuntimeContext(telemetrySettings, runtimeContext),
		ToolsContext:   telemetryToolsContext(telemetrySettings, toolsContext),
	})

	// Call finish callback (v6.0: with user context)
	if opts.OnFinish != nil {
		opts.OnFinish(ctx, result, runtimeContext)
	}

	// Populate TotalUsage: Usage accumulates across all steps so they are equal.
	result.TotalUsage = result.Usage

	// Emit structured OnFinishEvent.
	// Usage = last step's usage; TotalUsage = sum across all steps.
	var finishUsage types.Usage
	var finishRawReason string
	var lastModel types.StepModel
	var lastStepNum int
	var lastReasoning []types.ReasoningContent
	var lastReasoningText string
	var lastStaticCalls, lastDynamicCalls []types.ToolCall
	var lastStaticResults, lastDynamicResults []types.ToolResult
	if len(result.Steps) > 0 {
		last := result.Steps[len(result.Steps)-1]
		finishUsage = last.Usage
		finishRawReason = last.RawFinishReason
		lastModel = last.Model
		lastStepNum = last.StepNumber
		lastReasoning = last.Reasoning
		lastReasoningText = last.ReasoningText
		lastStaticCalls = last.StaticToolCalls
		lastDynamicCalls = last.DynamicToolCalls
		lastStaticResults = last.StaticToolResults
		lastDynamicResults = last.DynamicToolResults
	}
	Notify(ctx, OnFinishEvent{
		CallID:             callID,
		StepNumber:         lastStepNum,
		Model:              lastModel,
		ModelProvider:      opts.Model.Provider(),
		ModelID:            opts.Model.ModelID(),
		Text:               result.Text,
		Reasoning:          lastReasoning,
		ReasoningText:      lastReasoningText,
		ToolCalls:          result.ToolCalls,
		StaticToolCalls:    lastStaticCalls,
		DynamicToolCalls:   lastDynamicCalls,
		ToolResults:        result.ToolResults,
		StaticToolResults:  lastStaticResults,
		DynamicToolResults: lastDynamicResults,
		FinishReason:       result.FinishReason,
		RawFinishReason:    finishRawReason,
		Usage:              finishUsage,
		Steps:              result.Steps,
		TotalUsage:         result.Usage,
		Warnings:           result.Warnings,
		Sources:            result.Sources,
		Files:              result.Files,
		ProviderMetadata:   result.ProviderMetadata,
		ResponseHeaders:    result.ResponseHeaders,
		Request:            GenerateStepRequest{Body: result.RawRequest},
		Response: GenerateStepResponse{
			ID:       result.Response.ID,
			Headers:  result.ResponseHeaders,
			Messages: result.Response.Messages,
			Body:     result.RawResponse,
		},
		ExperimentalContext: runtimeContext,
		RuntimeContext:      runtimeContext,
		ToolsContext:        toolsContext,
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
	callID              string
	onStart             func(ctx context.Context, e OnToolCallStartEvent)
	onFinish            func(ctx context.Context, e OnToolCallFinishEvent)
	fallbackStart       func(ctx context.Context, e OnToolCallStartEvent)
	fallbackFinish      func(ctx context.Context, e OnToolCallFinishEvent)
	stepNum             int
	modelProvider       string
	modelID             string
	messages            []types.Message
	experimentalContext interface{}
	runtimeContext      interface{}
	toolsContext        map[string]interface{}
	functionID          string
	metadata            map[string]any
	timeout             *TimeoutConfig
	telemetrySettings   *TelemetrySettings
}

// executeTools executes a list of tool calls
// Updated in v6.0 to pass ToolExecutionOptions with ToolCallID and UserContext
// Updated in v6.0.57 to handle provider-executed (deferrable) tools
// Updated in v6.1 to fire structured OnToolCallStart/Finish events.
func executeTools(ctx context.Context, toolCalls []types.ToolCall, availableTools []types.Tool, runtimeContext interface{}, toolsContext map[string]interface{}, toolApproval interface{}, usage *types.Usage, callbacks toolCallEventCallbacks) ([]types.ToolResult, error) {
	results := make([]types.ToolResult, len(toolCalls))
	if toolsContext == nil {
		toolsContext = map[string]interface{}{}
	}

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

		// Check if this is a provider-executed tool.
		// ProviderExecuted is set to true on types.Tool by each provider tool constructor
		// (e.g., web_search_20260209, web_fetch_20260209, code_execution, tool_search_bm25).
		providerExecuted := tool.ProviderExecuted

		providerMetadata := mergeProviderMetadataMaps(call.ProviderMetadata, tool.ProviderMetadata)
		if providerExecuted {
			// Provider-executed tool: result will come from provider in next response
			// We don't execute locally, just mark as pending
			results[i] = types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Input:            call.Arguments,
				Result:           nil,
				Error:            nil,
				ProviderExecuted: true,
				ProviderMetadata: providerMetadata,
			}
		} else {
			approval := resolveToolApproval(ctx, call, availableTools, callbacks.messages, runtimeContext, toolsContext, toolApproval)
			switch approval.Status {
			case types.ToolApprovalStatusDenied:
				reason := approval.Reason
				if reason == nil {
					reason = strPtr(fmt.Sprintf("Tool call to %s was denied by ToolApproval policy.", call.ToolName))
				}
				results[i] = types.ToolResult{
					ToolCallID:       call.ID,
					ToolName:         call.ToolName,
					Input:            call.Arguments,
					Result:           types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: *reason},
					ApprovalStatus:   types.ToolApprovalStatusDenied,
					ApprovalReason:   reason,
					ProviderMetadata: providerMetadata,
				}
				continue
			case types.ToolApprovalStatusUserApproval:
				results[i] = types.ToolResult{
					ToolCallID:       call.ID,
					ToolName:         call.ToolName,
					Input:            call.Arguments,
					Result:           map[string]interface{}{"type": "tool-approval-request", "approvalId": call.ID, "toolCall": call},
					ApprovalStatus:   types.ToolApprovalStatusUserApproval,
					ProviderMetadata: providerMetadata,
				}
				continue
			}

			toolContext, err := validateToolContextFor(tool, call.ToolName, toolsContext[call.ToolName])
			if err != nil {
				results[i] = types.ToolResult{
					ToolCallID:       call.ID,
					ToolName:         call.ToolName,
					Input:            call.Arguments,
					Error:            err,
					ProviderMetadata: providerMetadata,
				}
				continue
			}

			// Emit OnToolCallStartEvent before execution.
			startEvent := OnToolCallStartEvent{
				CallID:              callbacks.callID,
				ToolCallID:          call.ID,
				ToolName:            call.ToolName,
				Args:                call.Arguments,
				StepNumber:          callbacks.stepNum,
				ModelProvider:       callbacks.modelProvider,
				ModelID:             callbacks.modelID,
				Messages:            callbacks.messages,
				ExperimentalContext: callbacks.experimentalContext,
				RuntimeContext:      callbacks.runtimeContext,
				ToolsContext:        callbacks.toolsContext,
			}
			Notify(ctx, startEvent, callbacks.onStart, callbacks.fallbackStart)

			// Fire telemetry OnToolCallStart — integrations may inject a child span.
			toolCtx := telemetry.FireOnToolCallStart(ctx, telemetry.TelemetryToolCallStartEvent{
				Settings:    callbacks.telemetrySettings,
				ToolCallID:  call.ID,
				ToolName:    call.ToolName,
				Args:        call.Arguments,
				ToolContext: telemetryToolContext(callbacks.telemetrySettings, call.ToolName, toolContext),
			})

			// Locally-executed tool: execute now, wrapped by telemetry integrations
			// so they can create nested spans.
			execOptions := types.ToolExecutionOptions{
				ToolCallID:     call.ID,
				UserContext:    runtimeContext,
				RuntimeContext: runtimeContext,
				ToolContext:    toolContext,
				Usage:          usage,
				Metadata:       make(map[string]interface{}),
			}

			// Apply per-tool timeout if configured.
			execCtx := toolCtx
			execCancel := func() {}
			if toolTimeout := callbacks.timeout.GetToolTimeout(call.ToolName); toolTimeout != nil {
				execCtx, execCancel = context.WithTimeout(toolCtx, *toolTimeout)
			}

			startTime := now()
			toolResult, toolErr := telemetry.FireExecuteToolWithSettings(
				execCtx,
				callbacks.telemetrySettings,
				call.ToolName,
				call.Arguments,
				func(execCtx context.Context, args map[string]interface{}) (interface{}, error) {
					return tool.Execute(execCtx, args, execOptions)
				},
			)
			durationMs := now() - startTime
			timedOut := execCtx.Err() != nil
			execCancel() // release timeout resources immediately after execution
			if callbacks.timeout != nil && callbacks.timeout.GetToolTimeout(call.ToolName) != nil && timedOut {
				if toolErr == nil {
					toolErr = context.DeadlineExceeded
				}
				toolErr = wrapTimeoutError(TimeoutReasonTool, toolErr)
			}

			results[i] = types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Input:            call.Arguments,
				Result:           toolResult,
				Error:            toolErr,
				ProviderExecuted: false,
				ProviderMetadata: providerMetadata,
			}

			// Fire telemetry OnToolCallFinish so integrations can record errors.
			telemetry.FireOnToolCallFinish(toolCtx, telemetry.TelemetryToolCallFinishEvent{
				Settings:    callbacks.telemetrySettings,
				ToolCallID:  call.ID,
				ToolName:    call.ToolName,
				Args:        call.Arguments,
				Result:      toolResult,
				Error:       toolErr,
				DurationMs:  durationMs,
				ToolContext: telemetryToolContext(callbacks.telemetrySettings, call.ToolName, toolContext),
			})

			// Emit OnToolCallFinishEvent after execution, whether it succeeded or failed.
			finishEvent := OnToolCallFinishEvent{
				CallID:              callbacks.callID,
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
				RuntimeContext:      callbacks.runtimeContext,
				ToolsContext:        callbacks.toolsContext,
			}
			Notify(ctx, finishEvent, callbacks.onFinish, callbacks.fallbackFinish)
		}
	}

	return results, nil
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
	return functionID, metadata
}

func telemetryUsageFromUsage(usage types.Usage) telemetry.TelemetryUsage {
	out := telemetry.TelemetryUsage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
	}
	if usage.InputDetails != nil {
		out.NoCacheInputTokens = usage.InputDetails.NoCacheTokens
		out.CacheReadInputTokens = usage.InputDetails.CacheReadTokens
		out.CacheCreationInputTokens = usage.InputDetails.CacheWriteTokens
	}
	if usage.OutputDetails != nil {
		out.OutputTextTokens = usage.OutputDetails.TextTokens
		out.ReasoningTokens = usage.OutputDetails.ReasoningTokens
	}
	return out
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

// buildReasoningText concatenates the text from reasoning content parts.
func buildReasoningText(parts []types.ReasoningContent) string {
	var sb strings.Builder
	for _, p := range parts {
		if p.Text != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// filterStaticToolCalls returns tool calls where Dynamic is false.
func filterStaticToolCalls(calls []types.ToolCall) []types.ToolCall {
	var out []types.ToolCall
	for _, c := range calls {
		if !c.Dynamic {
			out = append(out, c)
		}
	}
	return out
}

// filterDynamicToolCalls returns tool calls where Dynamic is true.
func filterDynamicToolCalls(calls []types.ToolCall) []types.ToolCall {
	var out []types.ToolCall
	for _, c := range calls {
		if c.Dynamic {
			out = append(out, c)
		}
	}
	return out
}

// filterStaticToolResults returns tool results where Dynamic is false.
func filterStaticToolResults(results []types.ToolResult) []types.ToolResult {
	var out []types.ToolResult
	for _, r := range results {
		if !r.Dynamic {
			out = append(out, r)
		}
	}
	return out
}

// filterDynamicToolResults returns tool results where Dynamic is true.
func filterDynamicToolResults(results []types.ToolResult) []types.ToolResult {
	var out []types.ToolResult
	for _, r := range results {
		if r.Dynamic {
			out = append(out, r)
		}
	}
	return out
}
