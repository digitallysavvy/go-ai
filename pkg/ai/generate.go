package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	retryutil "github.com/digitallysavvy/go-ai/pkg/internal/retry"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// now returns the current time in milliseconds since Unix epoch.
// Used for measuring tool call execution duration.
func now() int64 {
	return time.Now().UnixMilli()
}

func gatewayMaxRetries(model provider.LanguageModel, maxRetries *int) int {
	if model == nil || model.Provider() != "gateway" {
		return 0
	}
	if maxRetries == nil {
		return 2
	}
	return *maxRetries
}

func validateMaxRetries(maxRetries *int) error {
	if maxRetries != nil && *maxRetries < 0 {
		return &providererrors.InvalidArgumentError{
			Field:   "maxRetries",
			Message: "maxRetries must be >= 0",
		}
	}
	return nil
}

func preparedMaxRetries(maxRetries *int) int {
	if maxRetries == nil {
		return 2
	}
	return *maxRetries
}

func isGatewayCallRetryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var gatewayErr gatewayerrors.GatewayError
	if errors.As(err, &gatewayErr) {
		return gatewayErr.IsRetryable()
	}
	var providerErr *providererrors.ProviderError
	if errors.As(err, &providerErr) {
		status := providerErr.StatusCode
		return status == 408 || status == 409 || status == 429 || status >= 500
	}
	return false
}

func doGenerateWithGatewayRetry(ctx context.Context, model provider.LanguageModel, opts *provider.GenerateOptions, maxRetries *int) (*types.GenerateResult, error) {
	retries := gatewayMaxRetries(model, maxRetries)
	if retries <= 0 {
		return model.DoGenerate(ctx, opts)
	}

	var result *types.GenerateResult
	err := retryutil.Do(ctx, retryutil.Config{
		MaxRetries:   retries,
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2,
		Jitter:       false,
		ShouldRetry:  isGatewayCallRetryable,
	}, func(retryCtx context.Context) error {
		var err error
		result, err = model.DoGenerate(retryCtx, opts)
		return err
	})
	return result, err
}

func doStreamWithGatewayRetry(ctx context.Context, model provider.LanguageModel, opts *provider.GenerateOptions, maxRetries *int) (provider.TextStream, error) {
	retries := gatewayMaxRetries(model, maxRetries)
	if retries <= 0 {
		return model.DoStream(ctx, opts)
	}

	var stream provider.TextStream
	err := retryutil.Do(ctx, retryutil.Config{
		MaxRetries:   retries,
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2,
		Jitter:       false,
		ShouldRetry:  isGatewayCallRetryable,
	}, func(retryCtx context.Context) error {
		var err error
		stream, err = model.DoStream(retryCtx, opts)
		return err
	})
	return stream, err
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
	// Instructions is a TypeScript-compatible alias for System.
	// When set, Instructions takes precedence over System.
	Instructions *string

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
	Headers          map[string]string

	// Tools available for the model to call
	Tools      []types.Tool
	ToolChoice types.ToolChoice

	// ToolOrder controls the order tools are sent to providers. Listed tools are
	// sent first in listed order; unlisted tools follow alphabetically.
	ToolOrder []string

	// ActiveTools restricts the tools available for this generation.
	ActiveTools []string

	// ToolApproval configures approval handling for tool execution.
	ToolApproval types.ToolApprovalConfig

	// ExperimentalToolApprovalSecret signs server-issued approval requests so
	// resumed approval responses can be verified before execution.
	ExperimentalToolApprovalSecret []byte

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

	// SensitiveRuntimeContext omits runtime context from telemetry payloads.
	SensitiveRuntimeContext bool

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

	// Include controls which large request/response details are retained in
	// step results. Defaults match the TypeScript SDK: request/response bodies
	// and request messages are excluded unless explicitly enabled.
	Include *IncludeOptions

	// ExperimentalInclude is a deprecated alias for Include.
	ExperimentalInclude *IncludeOptions

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

	// MaxRetries controls transient provider call retries. For Gateway models,
	// nil uses the TypeScript SDK default of 2 retries; set to 0 to disable.
	MaxRetries *int

	// ExperimentalSandbox is passed through to tool execution. PrepareStep can
	// override it for an individual step.
	ExperimentalSandbox interface{}

	// ExperimentalRefineToolInput refines parsed tool inputs by tool name before
	// approval, callbacks, telemetry, execution, and response messages.
	ExperimentalRefineToolInput map[string]ToolInputRefiner

	// ExperimentalDownload downloads remote file URLs that need inline data
	// before sending tool-result messages back to the model. Defaults to
	// DefaultDownload, matching the TypeScript SDK's default download behavior.
	ExperimentalDownload DownloadFunction

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

	// Internal contains test-only ID generators matching the TypeScript
	// _internal option.
	Internal *InternalOptions

	// ========================================================================
	// Callbacks (Updated signatures in v6.0)
	// ========================================================================

	// PrepareStep is called before each generation step
	// Allows modification of options before the next step
	// Receives the user context if ExperimentalContext is set
	PrepareStep func(ctx context.Context, step PrepareStepOptions) PrepareStepOptions

	// OnStepEnd is called after each generation step completes.
	OnStepEnd func(ctx context.Context, step types.StepResult, userContext interface{})

	// OnStepFinish is called after each generation step completes.
	//
	// Deprecated: use OnStepEnd.
	OnStepFinish func(ctx context.Context, step types.StepResult, userContext interface{})

	// OnEnd is called when generation completes.
	// Receives the user context if RuntimeContext/ExperimentalContext is set.
	OnEnd func(ctx context.Context, result *GenerateTextResult, userContext interface{})

	// OnFinish is called when generation completes.
	// Receives the user context if RuntimeContext/ExperimentalContext is set.
	//
	// Deprecated: use OnEnd.
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

	// OnStepEndEvent is called at the end of each LLM step with a typed event.
	OnStepEndEvent func(ctx context.Context, e OnStepFinishEvent)

	// OnStepFinishEvent is called at the end of each LLM step with a typed
	// event.
	//
	// Deprecated: use OnStepEndEvent.
	OnStepFinishEvent func(ctx context.Context, e OnStepFinishEvent)

	// OnEndEvent is called once when the entire generation completes with a
	// typed event.
	OnEndEvent func(ctx context.Context, e OnFinishEvent)

	// OnFinishEvent is called once when the entire generation completes with a
	// typed OnFinishEvent. Use OnEndEvent for new code.
	//
	// Deprecated: use OnEndEvent.
	OnFinishEvent func(ctx context.Context, e OnFinishEvent)

	// OnAbort is called when generation is aborted by context cancellation or
	// deadline before normal completion.
	OnAbort func(ctx context.Context, steps []types.StepResult)
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

func effectiveSystem(system string, instructions *string) string {
	if instructions != nil {
		return *instructions
	}
	return system
}

// PrepareStepOptions contains options that can be modified before each step
type PrepareStepOptions struct {
	// Model for the next step.
	Model provider.LanguageModel

	// System is the effective instructions/system prompt for the next step.
	System string

	// Instructions is a TypeScript-compatible alias for System.
	Instructions *string

	// InitialInstructions are the instructions provided to GenerateText.
	InitialInstructions *string

	// Messages for the next step
	Messages []types.Message

	// InitialMessages are the standardized messages initially provided.
	InitialMessages []types.Message

	// ResponseMessages are accumulated assistant/tool messages from previous steps.
	ResponseMessages []types.Message

	// User context (from ExperimentalContext)
	UserContext interface{}

	RuntimeContext interface{}
	ToolsContext   map[string]interface{}

	// Current step number
	StepNumber int

	// Steps contains completed step results before this step.
	Steps []types.StepResult

	// Tools and tool choice for this step.
	Tools       []types.Tool
	ToolChoice  types.ToolChoice
	ActiveTools []string
	ToolOrder   []string

	// ProviderOptions are provider-specific options for this step.
	ProviderOptions map[string]interface{}

	// ExperimentalSandbox overrides the sandbox for this step only.
	ExperimentalSandbox interface{}

	// Accumulated usage so far
	AccumulatedUsage types.Usage
}

// GenerateTextResult contains the result of text generation.
type GenerateTextResult struct {
	// Content contains all generated content parts from all steps in order.
	Content []types.ContentPart `json:"content"`

	// Generated text content
	Text string `json:"text"`

	// Reasoning holds the reasoning/thinking content from the final step.
	Reasoning []types.ReasoningContent `json:"reasoning"`

	// ReasoningText is the concatenated reasoning text from the final step.
	ReasoningText string `json:"reasoningText,omitempty"`

	// Output contains the parsed output when a WithOutput option was provided.
	// Type-assert to the concrete type, e.g.: recipe := result.Output.(Recipe)
	// Nil when no Output option was set.
	Output any `json:"output,omitempty"`

	// Tool calls made across all steps.
	ToolCalls []types.ToolCall `json:"toolCalls"`

	// StaticToolCalls are tool calls from non-dynamic (typed) tools across all steps.
	StaticToolCalls []types.ToolCall `json:"staticToolCalls"`

	// DynamicToolCalls are tool calls from dynamically registered tools across all steps.
	DynamicToolCalls []types.ToolCall `json:"dynamicToolCalls"`

	// Tool results from all steps.
	ToolResults []types.ToolResult `json:"toolResults"`

	// StaticToolResults are results from non-dynamic (typed) tools across all steps.
	StaticToolResults []types.ToolResult `json:"staticToolResults"`

	// DynamicToolResults are results from dynamically registered tools across all steps.
	DynamicToolResults []types.ToolResult `json:"dynamicToolResults"`

	// Steps taken during generation (for multi-step tool calling)
	Steps []types.StepResult `json:"steps"`

	// FinalStep is the last step. It is a shortcut for Steps[len(Steps)-1].
	FinalStep types.StepResult `json:"finalStep"`

	// Reason why generation finished
	FinishReason types.FinishReason `json:"finishReason"`

	// RawFinishReason is the raw finish reason string from the provider.
	RawFinishReason string `json:"rawFinishReason,omitempty"`

	// StopReason is the reason string from the StopCondition that stopped the loop.
	// Empty if the loop ended naturally (model stopped calling tools).
	StopReason string `json:"stopReason,omitempty"`

	// Token usage information (last step)
	Usage types.Usage `json:"usage"`

	// Context management information (Anthropic-specific)
	ContextManagement interface{} `json:"contextManagement,omitempty"`

	// Warnings from all provider calls.
	Warnings []types.Warning `json:"warnings,omitempty"`

	// ProviderMetadata holds provider-specific metadata from the last generation step.
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`

	// Sources contains citation or grounding references from all steps.
	Sources []types.SourceContent `json:"sources,omitempty"`

	// Files contains model-generated output files (e.g. images, audio) from all steps.
	Files []types.GeneratedFileContent `json:"files,omitempty"`

	// TotalUsage is the sum of token usage across all steps.
	// For single-step generation, TotalUsage == Usage.
	TotalUsage types.Usage `json:"totalUsage"`

	// ResponseMessages contains response messages generated across all steps.
	ResponseMessages []types.Message `json:"responseMessages"`

	// Request contains metadata about the last request sent to the provider.
	Request types.StepRequest `json:"request"`

	// Response contains metadata about the last response from the provider.
	Response types.StepResponse `json:"response"`

	// Raw request/response (for debugging). Deprecated: use Request.Body and Response.Body.
	RawRequest  interface{} `json:"rawRequest,omitempty"`
	RawResponse interface{} `json:"rawResponse,omitempty"`

	// ResponseHeaders are the raw HTTP response headers from the provider.
	// Deprecated: use Response.Headers instead.
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`
}

// GenerateText performs non-streaming text generation with optional tool calling
func GenerateText(ctx context.Context, opts GenerateTextOptions) (result *GenerateTextResult, err error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}
	telemetrySettings := effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)
	runtimeContext := effectiveRuntimeContext(opts.RuntimeContext, opts.ExperimentalContext)
	system := effectiveSystem(opts.System, opts.Instructions)
	include := effectiveInclude(opts.Include, opts.ExperimentalInclude, false)
	if opts.Include == nil && opts.ExperimentalInclude == nil && opts.ExperimentalRetention != nil {
		include.RequestBody = opts.ExperimentalRetention.ShouldRetainRequestBody()
		include.ResponseBody = opts.ExperimentalRetention.ShouldRetainResponseBody()
	}
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
		telSystem = system
	}
	ctx = telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
		OperationType:  "ai.generateText",
		ModelProvider:  opts.Model.Provider(),
		ModelID:        opts.Model.ModelID(),
		Settings:       telemetrySettings,
		Prompt:         telPrompt,
		System:         telSystem,
		RuntimeContext: telemetryRuntimeContextWithSensitivity(telemetrySettings, runtimeContext, opts.SensitiveRuntimeContext),
		ToolsContext:   telemetryToolsContext(telemetrySettings, toolsContext),
	})
	callID := ""

	// Ensure telemetry is always closed — OnError ends the span on failure,
	// OnFinish ends it on success.
	defer func() {
		if err != nil {
			if isAbortErr(ctx, err) {
				var abortSteps []types.StepResult
				if result != nil {
					abortSteps = append([]types.StepResult(nil), result.Steps...)
				}
				if opts.OnAbort != nil {
					opts.OnAbort(ctx, abortSteps)
				}
				telemetry.FireOnAbort(ctx, telemetry.TelemetryAbortEvent{Settings: telemetrySettings, CallID: callID, Reason: err, Steps: abortSteps})
				return
			}
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
	prompt := buildPrompt(opts.Prompt, opts.Messages, system)
	allowSystem := allowSystemMessages(opts.AllowSystemMessages, opts.AllowSystemInMessages)
	prompt, err = promptutils.NormalizePrompt(prompt, allowSystem)
	if err != nil {
		return nil, err
	}
	// Extract telemetry info once for all callback events
	cbFuncID, cbMeta := telemetryCallbackInfo(telemetrySettings)
	generateID := internalGenerateID(opts.Internal)
	callID = internalGenerateCallID(opts.Internal)()

	// Emit OnStartEvent.
	onStepEnd := opts.OnStepEnd
	if onStepEnd == nil {
		onStepEnd = opts.OnStepFinish
	}
	onStepEndEvent := opts.OnStepEndEvent
	if onStepEndEvent == nil {
		onStepEndEvent = opts.OnStepFinishEvent
	}
	onEnd := opts.OnEnd
	if onEnd == nil {
		onEnd = opts.OnFinish
	}
	onEndEvent := opts.OnEndEvent
	if onEndEvent == nil {
		onEndEvent = opts.OnFinishEvent
	}
	Notify(ctx, OnStartEvent{
		CallID:              callID,
		OperationID:         "ai.generateText",
		ModelProvider:       opts.Model.Provider(),
		ModelID:             opts.Model.ModelID(),
		System:              system,
		Prompt:              opts.Prompt,
		Messages:            opts.Messages,
		Tools:               opts.Tools,
		ToolChoice:          opts.ToolChoice,
		Output:              opts.Output,
		ProviderOptions:     opts.ProviderOptions,
		Headers:             opts.Headers,
		Temperature:         opts.Temperature,
		MaxTokens:           opts.MaxTokens,
		TopP:                opts.TopP,
		TopK:                opts.TopK,
		FrequencyPenalty:    opts.FrequencyPenalty,
		PresencePenalty:     opts.PresencePenalty,
		StopSequences:       opts.StopSequences,
		Seed:                opts.Seed,
		MaxRetries:          preparedMaxRetries(opts.MaxRetries),
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
	initialMessages := append([]types.Message(nil), prompt.Messages...)
	initialInstructions := system
	instructionsForNextStep := system

	// pendingDeferredToolCalls tracks provider tools whose results will arrive in
	// a subsequent response (SupportsDeferredResults=true). Key = toolCallID, value = toolName.
	pendingDeferredToolCalls := make(map[string]string)

	// Execute generation loop (for tool calling)
	for stepNum := 1; stepNum <= maxSteps; stepNum++ {
		stepStart := time.Now()
		stepIndex := stepNum - 1
		stepModel := opts.Model
		stepSystem := instructionsForNextStep
		stepMessages := currentMessages
		stepSandbox := opts.ExperimentalSandbox
		stepTools := FilterActiveTools(opts.Tools, opts.ActiveTools)
		stepToolChoice := opts.ToolChoice
		stepToolOrder := opts.ToolOrder
		stepProviderOptions := opts.ProviderOptions

		accumulatedResponseMessages := responseMessagesFromSteps(result.Steps)
		if opts.PrepareStep != nil {
			prepared := opts.PrepareStep(ctx, PrepareStepOptions{
				Model:               stepModel,
				System:              stepSystem,
				Instructions:        &stepSystem,
				InitialInstructions: &initialInstructions,
				Messages:            append([]types.Message(nil), stepMessages...),
				InitialMessages:     append([]types.Message(nil), initialMessages...),
				ResponseMessages:    accumulatedResponseMessages,
				UserContext:         runtimeContext,
				RuntimeContext:      runtimeContext,
				ToolsContext:        toolsContext,
				StepNumber:          stepIndex,
				Steps:               append([]types.StepResult(nil), result.Steps...),
				Tools:               stepTools,
				ToolChoice:          stepToolChoice,
				ActiveTools:         opts.ActiveTools,
				ToolOrder:           stepToolOrder,
				ProviderOptions:     stepProviderOptions,
				ExperimentalSandbox: stepSandbox,
				AccumulatedUsage:    result.Usage,
			})
			if prepared.Model != nil {
				stepModel = prepared.Model
			}
			if prepared.Instructions != nil {
				stepSystem = *prepared.Instructions
			} else if prepared.System != "" {
				stepSystem = prepared.System
			}
			if prepared.Messages != nil {
				stepMessages = prepared.Messages
				currentMessages = prepared.Messages
			}
			if prepared.Tools != nil {
				stepTools = prepared.Tools
			}
			if prepared.ToolChoice.Type != "" {
				stepToolChoice = prepared.ToolChoice
			}
			if prepared.ToolOrder != nil {
				stepToolOrder = prepared.ToolOrder
			}
			if prepared.ProviderOptions != nil {
				stepProviderOptions = prepared.ProviderOptions
			}
			if prepared.RuntimeContext != nil {
				runtimeContext = prepared.RuntimeContext
			}
			if prepared.ToolsContext != nil {
				toolsContext = prepared.ToolsContext
			}
			if prepared.ExperimentalSandbox != nil {
				stepSandbox = prepared.ExperimentalSandbox
			}
		}
		stepTools = resolveStepTools(ctx, stepTools, toolsContext, stepSandbox)
		stepTools = orderStepTools(stepTools, stepToolOrder)
		instructionsForNextStep = stepSystem
		toolsByName := make(map[string]*types.Tool, len(stepTools))
		for i := range stepTools {
			toolsByName[stepTools[i].Name] = &stepTools[i]
		}

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
			StepNumber:          stepIndex,
			ModelProvider:       stepModel.Provider(),
			ModelID:             stepModel.ModelID(),
			System:              stepSystem,
			Messages:            stepMessages,
			Tools:               stepTools,
			ActiveTools:         opts.ActiveTools,
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

		stepPrompt, normErr := promptutils.NormalizePromptWithDownloadSupport(stepCtx, types.Prompt{
			Messages: stepMessages,
			System:   appendSandboxDescription(stepSystem, stepSandbox),
		}, allowSystem, effectiveDownload(opts.ExperimentalDownload), supportedURLChecker(stepModel))
		if normErr != nil {
			return nil, fmt.Errorf("prompt normalization failed at step %d: %w", stepNum, normErr)
		}

		// Build generate options
		genOpts := &provider.GenerateOptions{
			Prompt:                stepPrompt,
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
			Headers:               opts.Headers,
			Tools:                 stepTools,
			ToolChoice:            stepToolChoice,
			RuntimeContext:        runtimeContext,
			ToolsContext:          toolsContext,
			ResponseFormat:        responseFormat,
			Reasoning:             opts.Reasoning,
			SendReasoning:         opts.SendReasoning,
			ProviderOptions:       stepProviderOptions,
			Telemetry:             telemetrySettings,
		}

		// Fire step-start telemetry. OTel implementations create a child step span
		// and embed it in the returned context so OnStepFinish can end it.
		stepCtx = telemetry.FireOnStepStart(stepCtx, telemetry.TelemetryStepStartEvent{
			OperationType:  "ai.generateText",
			Settings:       telemetrySettings,
			StepNumber:     stepIndex,
			ModelProvider:  stepModel.Provider(),
			ModelID:        stepModel.ModelID(),
			RuntimeContext: telemetryRuntimeContextWithSensitivity(telemetrySettings, runtimeContext, opts.SensitiveRuntimeContext),
			ToolsContext:   telemetryToolsContext(telemetrySettings, toolsContext),
		})

		telemetry.FireOnLanguageModelCallStart(stepCtx, telemetry.LanguageModelCallStartEvent{
			Settings:      telemetrySettings,
			CallID:        callID,
			ModelProvider: stepModel.Provider(),
			ModelID:       stepModel.ModelID(),
			Prompt:        genOpts.Prompt,
			Tools:         genOpts.Tools,
		})

		// Call the model with step context
		modelCallStart := time.Now()
		genResult, err := doGenerateWithGatewayRetry(stepCtx, stepModel, genOpts, opts.MaxRetries)
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
		performance := stepPerformance(modelCallStart, genResult.Usage, nil, nil)
		responseID := ""
		if meta := responseMetadataFromGenerateResultWithID(stepModel, genResult, generateID); meta != nil {
			responseID = meta.ID
		}
		telemetry.FireOnLanguageModelCallEnd(stepCtx, telemetry.LanguageModelCallEndEvent{
			Settings:      telemetrySettings,
			CallID:        callID,
			ModelProvider: stepModel.Provider(),
			ModelID:       stepModel.ModelID(),
			FinishReason:  string(genResult.FinishReason),
			Usage:         modelCallUsage,
			Content:       genResult.Content,
			ResponseID:    responseID,
			Performance:   languageModelCallPerformance(performance),
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
		stepResp := generateStepResponseFromGenerateResultWithID(stepModel, genResult, generateID)
		requestBody := any(nil)
		responseBody := any(nil)
		if include.RequestBody {
			requestBody = genResult.RawRequest
		}
		if include.ResponseBody {
			responseBody = stepResp.Body
		}
		stepRequest := types.StepRequest{Body: requestBody}
		if include.RequestMessages {
			stepRequest.Messages = append([]types.Message(nil), stepMessages...)
		}

		// Create step result
		stepResult := types.StepResult{
			CallID:           callID,
			StepNumber:       stepIndex,
			Model:            types.StepModel{Provider: stepModel.Provider(), ModelID: stepModel.ModelID()},
			Content:          genResult.Content,
			Text:             genResult.Text,
			Reasoning:        stepReasoning,
			ReasoningText:    stepReasoningText,
			Files:            stepFiles,
			ToolCalls:        genResult.ToolCalls,
			StaticToolCalls:  filterStaticToolCalls(genResult.ToolCalls),
			DynamicToolCalls: filterDynamicToolCalls(genResult.ToolCalls),
			ToolResults:      []types.ToolResult{},
			FinishReason:     genResult.FinishReason,
			RawFinishReason:  stepRawFinishReason,
			Usage:            genResult.Usage,
			Performance:      performance,
			Warnings:         genResult.Warnings,
			Sources:          stepSources,
			Request:          stepRequest,
			Response: types.StepResponse{
				ID:        stepResp.ID,
				Timestamp: stepResp.Timestamp,
				ModelID:   stepResp.ModelID,
				Headers:   stepResp.Headers,
				Body:      responseBody,
			},
			ProviderMetadata: genResult.ProviderMetadata,
			ToolsContext:     toolsContext,
			RuntimeContext:   runtimeContext,
		}

		// Update accumulated usage
		result.Usage = result.Usage.Add(genResult.Usage)

		hasUserApproval := false

		// Check if there are tool calls to execute
		refinedToolCalls, refineErr := RefineToolCalls(ctx, genResult.ToolCalls, stepTools, opts.ExperimentalRefineToolInput, runtimeContext, toolsContext)
		if refineErr != nil {
			return nil, fmt.Errorf("tool input refinement failed at step %d: %w", stepNum, refineErr)
		}
		genResult.ToolCalls = enrichToolCallMetadata(refinedToolCalls, stepTools)
		stepResult.ToolCalls = genResult.ToolCalls
		stepResult.StaticToolCalls = filterStaticToolCalls(genResult.ToolCalls)
		stepResult.DynamicToolCalls = filterDynamicToolCalls(genResult.ToolCalls)
		stepResult.Content = generateResultContentParts(genResult)
		result.ToolCalls = append(result.ToolCalls, genResult.ToolCalls...)
		result.StaticToolCalls = filterStaticToolCalls(result.ToolCalls)
		result.DynamicToolCalls = filterDynamicToolCalls(result.ToolCalls)

		if len(genResult.ToolCalls) > 0 && len(stepTools) > 0 {
			// Execute tools with context flow (v6.0) and structured callbacks (v6.1)
			toolCallbacks := toolCallEventCallbacks{
				callID:              callID,
				onStart:             opts.OnToolExecutionStart,
				onFinish:            opts.OnToolExecutionEnd,
				fallbackStart:       opts.OnToolCallStart,
				fallbackFinish:      opts.OnToolCallFinish,
				stepNum:             stepIndex,
				modelProvider:       stepModel.Provider(),
				modelID:             stepModel.ModelID(),
				messages:            stepMessages,
				experimentalContext: runtimeContext,
				runtimeContext:      runtimeContext,
				toolsContext:        toolsContext,
				functionID:          cbFuncID,
				metadata:            cbMeta,
				timeout:             opts.Timeout,
				telemetrySettings:   telemetrySettings,
				experimentalSandbox: stepSandbox,
				toolExecutionMs:     map[string]int64{},
			}
			toolResults, err := executeTools(ctx, genResult.ToolCalls, stepTools, runtimeContext, toolsContext, opts.ToolApproval, &result.Usage, toolCallbacks)
			if err != nil {
				if opts.Timeout != nil && opts.Timeout.HasTotal() && ctx.Err() != nil {
					err = wrapTimeoutError(TimeoutReasonTotal, err)
				}
				var conversionErr *toolResultModelOutputError
				if errors.As(err, &conversionErr) {
					return nil, conversionErr.Unwrap()
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
			stepResult.Content = append(stepResult.Content, toolResultsToContentParts(toolResults, opts.ExperimentalToolApprovalSecret)...)
			stepResult.Performance = finishStepPerformance(stepResult.Performance, stepStart, toolCallbacks.toolExecutionMs)
			result.ToolResults = append(result.ToolResults, toolResults...)
			result.StaticToolResults = filterStaticToolResults(result.ToolResults)
			result.DynamicToolResults = filterDynamicToolResults(result.ToolResults)
			for _, tr := range toolResults {
				if tr.ApprovalStatus == types.ToolApprovalStatusUserApproval {
					hasUserApproval = true
					stepResult.FinishReason = types.FinishReasonUserApproval
				}
			}

			// Build response messages: assistant + tool results.
			stepResponseMsgs := providerutils.ConvertToResponseMessages(
				genResult.ToolCalls,
				stepResult.Content,
				toolResults,
			)
			currentMessages = append(currentMessages, stepResponseMsgs...)
			stepResult.ResponseMessages = stepResponseMsgs
			stepResult.Response.Messages = stepResponseMsgs
		} else {
			stepResult.Performance = finishStepPerformance(stepResult.Performance, stepStart, nil)
			// No more tool calls, we're done
			result.Text = genResult.Text
			result.Reasoning = stepReasoning
			result.ReasoningText = stepReasoningText
			result.FinishReason = genResult.FinishReason
			result.RawFinishReason = stepRawFinishReason
			result.ContextManagement = genResult.ContextManagement
			result.RawRequest = requestBody
			result.RawResponse = responseBody
			result.ResponseHeaders = genResult.ResponseHeaders
			result.ProviderMetadata = genResult.ProviderMetadata
			result.Request = stepRequest
			result.Response = types.StepResponse{
				ID:        stepResp.ID,
				Timestamp: stepResp.Timestamp,
				ModelID:   stepResp.ModelID,
				Headers:   stepResp.Headers,
				Body:      responseBody,
			}

			// Build response messages for this (final) step.
			finalMsgs := providerutils.ConvertToResponseMessages(
				genResult.ToolCalls,
				stepResult.Content,
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
		result.Content = append(result.Content, stepResult.Content...)
		result.Sources = append(result.Sources, stepResult.Sources...)
		result.Files = append(result.Files, stepResult.Files...)
		result.Warnings = append(result.Warnings, stepResult.Warnings...)
		result.ResponseMessages = responseMessagesFromSteps(result.Steps)
		result.FinalStep = stepResult

		// Call step finish callback (v6.0: with user context)
		if onStepEnd != nil {
			onStepEnd(ctx, stepResult, runtimeContext)
		}

		// Emit structured OnStepFinishEvent.
		Notify(ctx, OnStepFinishEvent{
			CallID:             callID,
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
		}, onStepEndEvent)

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
			telemetry.FireOnStepEnd(stepCtx, telemetry.TelemetryStepEndEvent{
				StepNumber:       stepIndex,
				FinishReason:     string(genResult.FinishReason),
				Usage:            stepTelUsage,
				Text:             genResult.Text,
				Reasoning:        stepTelReasoning.String(),
				ToolCalls:        genResult.ToolCalls,
				Files:            stepTelFiles,
				ProviderMetadata: genResult.ProviderMetadata,
				Settings:         telemetrySettings,
				RuntimeContext:   telemetryRuntimeContextWithSensitivity(telemetrySettings, runtimeContext, opts.SensitiveRuntimeContext),
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

	// Populate TotalUsage and final shortcuts before finish callbacks observe the result.
	result.TotalUsage = result.Usage
	if len(result.Steps) > 0 {
		result.FinalStep = result.Steps[len(result.Steps)-1]
		result.Request = result.FinalStep.Request
		result.Response = result.FinalStep.Response
		result.ResponseMessages = responseMessagesFromSteps(result.Steps)
		result.RawRequest = result.Request.Body
		result.RawResponse = result.Response.Body
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
		RuntimeContext: telemetryRuntimeContextWithSensitivity(telemetrySettings, runtimeContext, opts.SensitiveRuntimeContext),
		ToolsContext:   telemetryToolsContext(telemetrySettings, toolsContext),
	})

	// Call end callback (v6.0: with user context)
	if onEnd != nil {
		onEnd(ctx, result, runtimeContext)
	}

	// Emit structured OnFinishEvent.
	// Usage = last step's usage; TotalUsage = sum across all steps.
	var finishUsage types.Usage
	var finishRawReason string
	var lastModel types.StepModel
	var lastStepNum int
	var lastReasoning []types.ReasoningContent
	var lastReasoningText string
	var lastToolCalls []types.ToolCall
	var lastToolResults []types.ToolResult
	var lastStaticCalls, lastDynamicCalls []types.ToolCall
	var lastStaticResults, lastDynamicResults []types.ToolResult
	var lastSources []types.SourceContent
	if len(result.Steps) > 0 {
		last := result.Steps[len(result.Steps)-1]
		finishUsage = last.Usage
		finishRawReason = last.RawFinishReason
		lastModel = last.Model
		lastStepNum = last.StepNumber
		lastReasoning = last.Reasoning
		lastReasoningText = last.ReasoningText
		lastToolCalls = last.ToolCalls
		lastToolResults = last.ToolResults
		lastStaticCalls = last.StaticToolCalls
		lastDynamicCalls = last.DynamicToolCalls
		lastStaticResults = last.StaticToolResults
		lastDynamicResults = last.DynamicToolResults
		lastSources = last.Sources
	}
	Notify(ctx, OnFinishEvent{
		CallID:             callID,
		StepNumber:         lastStepNum,
		Model:              lastModel,
		ModelProvider:      lastModel.Provider,
		ModelID:            lastModel.ModelID,
		Text:               result.Text,
		Reasoning:          lastReasoning,
		ReasoningText:      lastReasoningText,
		ToolCalls:          lastToolCalls,
		StaticToolCalls:    lastStaticCalls,
		DynamicToolCalls:   lastDynamicCalls,
		ToolResults:        lastToolResults,
		StaticToolResults:  lastStaticResults,
		DynamicToolResults: lastDynamicResults,
		FinishReason:       result.FinishReason,
		RawFinishReason:    finishRawReason,
		Usage:              finishUsage,
		Steps:              result.Steps,
		TotalUsage:         result.Usage,
		Warnings:           result.Warnings,
		Sources:            lastSources,
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
	}, onEndEvent)

	// Apply retention settings (v6.0.60)
	// Exclude request/response bodies based on retention settings
	if opts.Include == nil && opts.ExperimentalInclude == nil && opts.ExperimentalRetention != nil {
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
	experimentalSandbox interface{}
	toolExecutionMs     map[string]int64
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

func responseMessagesFromSteps(steps []types.StepResult) []types.Message {
	var messages []types.Message
	for _, step := range steps {
		messages = append(messages, step.Response.Messages...)
	}
	return messages
}

func RefineToolCalls(ctx context.Context, calls []types.ToolCall, tools []types.Tool, refiners map[string]ToolInputRefiner, runtimeContext interface{}, toolsContext map[string]interface{}) ([]types.ToolCall, error) {
	if len(calls) == 0 || len(refiners) == 0 {
		return calls, nil
	}
	toolsByName := make(map[string]*types.Tool, len(tools))
	for i := range tools {
		toolsByName[tools[i].Name] = &tools[i]
	}
	refined := append([]types.ToolCall(nil), calls...)
	for i := range refined {
		if refined[i].Invalid {
			continue
		}
		refiner := refiners[refined[i].ToolName]
		if refiner == nil {
			continue
		}
		input, err := refiner(ctx, ToolInputRefinementOptions{
			ToolCall:       refined[i],
			Tool:           toolsByName[refined[i].ToolName],
			RuntimeContext: runtimeContext,
			ToolsContext:   toolsContext,
		})
		if err != nil {
			return nil, err
		}
		if input != nil {
			refined[i].Arguments = input
		}
	}
	return refined, nil
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
				Title:            call.Title,
				Input:            call.Arguments,
				Error:            toolErr,
				ProviderExecuted: false,
				ToolMetadata:     call.ToolMetadata,
			}
			continue
		}

		// Check if this is a provider-executed tool.
		// ProviderExecuted is set to true on types.Tool by each provider tool constructor
		// (e.g., web_search_20260209, web_fetch_20260209, code_execution, tool_search_bm25).
		providerExecuted := tool.ProviderExecuted

		providerMetadata := mergeProviderMetadataMaps(call.ProviderMetadata, tool.ProviderMetadata)
		approval, approvalErr := resolveToolApproval(ctx, call, availableTools, callbacks.messages, runtimeContext, toolsContext, toolApproval)
		approvalID := ""
		if approval.Status == types.ToolApprovalStatusUserApproval ||
			approval.Status == types.ToolApprovalStatusApproved ||
			approval.Status == types.ToolApprovalStatusDenied {
			approvalID = newCallID()
		}
		if approvalErr != nil {
			results[i] = types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Title:            call.Title,
				Input:            call.Arguments,
				Error:            approvalErr,
				ProviderExecuted: providerExecuted,
				ProviderMetadata: providerMetadata,
				ToolMetadata:     call.ToolMetadata,
			}
			continue
		}
		switch approval.Status {
		case types.ToolApprovalStatusDenied:
			reason := approval.Reason
			if reason == nil {
				reason = strPtr("Tool execution denied.")
			}
			results[i] = types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Title:            call.Title,
				Input:            call.Arguments,
				Result:           types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: *reason},
				ApprovalStatus:   types.ToolApprovalStatusDenied,
				ApprovalID:       approvalID,
				ApprovalReason:   reason,
				ProviderExecuted: providerExecuted,
				ProviderMetadata: providerMetadata,
				ToolMetadata:     call.ToolMetadata,
			}
			continue
		case types.ToolApprovalStatusUserApproval:
			results[i] = types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Title:            call.Title,
				Input:            call.Arguments,
				Result:           map[string]interface{}{"type": "tool-approval-request", "approvalId": approvalID, "toolCall": call},
				ApprovalStatus:   types.ToolApprovalStatusUserApproval,
				ApprovalID:       approvalID,
				ProviderExecuted: providerExecuted,
				ProviderMetadata: providerMetadata,
				ToolMetadata:     call.ToolMetadata,
			}
			continue
		}
		if providerExecuted {
			// Provider-executed tool: result will come from provider in next response
			// We don't execute locally, just mark as pending
			results[i] = types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Title:            call.Title,
				Input:            call.Arguments,
				Result:           nil,
				Error:            nil,
				ApprovalStatus:   approval.Status,
				ApprovalID:       approvalID,
				ApprovalReason:   approval.Reason,
				ProviderExecuted: true,
				ProviderMetadata: providerMetadata,
				ToolMetadata:     call.ToolMetadata,
			}
		} else {
			approvalStatus := approval.Status
			approvalReason := approval.Reason

			toolContext, err := validateToolContextFor(tool, call.ToolName, toolsContext[call.ToolName])
			if err != nil {
				results[i] = types.ToolResult{
					ToolCallID:       call.ID,
					ToolName:         call.ToolName,
					Title:            call.Title,
					Input:            call.Arguments,
					Error:            err,
					ProviderMetadata: providerMetadata,
					ToolMetadata:     call.ToolMetadata,
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
				ToolCallID:          call.ID,
				UserContext:         runtimeContext,
				RuntimeContext:      runtimeContext,
				ToolContext:         toolContext,
				Usage:               usage,
				Metadata:            make(map[string]interface{}),
				ToolMetadata:        call.ToolMetadata,
				ExperimentalSandbox: callbacks.experimentalSandbox,
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
			if callbacks.toolExecutionMs != nil {
				callbacks.toolExecutionMs[call.ID] = durationMs
			}
			timedOut := execCtx.Err() != nil
			execCancel() // release timeout resources immediately after execution
			if callbacks.timeout != nil && callbacks.timeout.GetToolTimeout(call.ToolName) != nil && timedOut {
				if toolErr == nil {
					toolErr = context.DeadlineExceeded
				}
				toolErr = wrapTimeoutError(TimeoutReasonTool, toolErr)
			}
			var modelOutput *types.ToolResultOutput
			if toolErr == nil && tool.ToModelOutput != nil {
				converted, convertErr := tool.ToModelOutput(ctx, types.ToModelOutputOptions{
					ToolCallID: call.ID,
					Input:      call.Arguments,
					Output:     toolResult,
					Result:     toolResult,
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
					Usage: usage,
				})
				if convertErr != nil {
					return results, &toolResultModelOutputError{err: convertErr}
				}
				modelOutput = converted
			}

			results[i] = types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Title:            call.Title,
				Input:            call.Arguments,
				Result:           toolResult,
				ModelOutput:      modelOutput,
				Error:            toolErr,
				ApprovalStatus:   approvalStatus,
				ApprovalID:       approvalID,
				ApprovalReason:   approvalReason,
				ProviderExecuted: false,
				ProviderMetadata: providerMetadata,
				ToolMetadata:     call.ToolMetadata,
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
