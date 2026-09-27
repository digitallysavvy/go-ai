package ai

import (
	"context"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// GenerateStepRequest contains additional information about the request sent to
// the provider for a single generation step.
// Mirrors LanguageModelRequestMetadata in the TypeScript SDK.
type GenerateStepRequest struct {
	// Body is the raw request body sent to the provider API (for debugging).
	Body interface{} `json:"body,omitempty"`
}

// GenerateStepResponse contains additional information about the response
// received from the provider for a single generation step.
// Mirrors LanguageModelResponseMetadata & {messages, body} in the TypeScript SDK.
type GenerateStepResponse struct {
	// ID is the provider-assigned response identifier when available.
	// For streaming paths, this may be a generated fallback value.
	ID string `json:"id,omitempty"`

	// Timestamp is when the provider started generating the response.
	// Zero value means it was not available.
	Timestamp time.Time `json:"timestamp,omitempty"`

	// ModelID is the model that handled the request, when available.
	ModelID string `json:"modelId,omitempty"`

	// Headers are the raw HTTP response headers from the provider.
	Headers map[string]string `json:"headers,omitempty"`

	// Messages are the response messages generated in this step
	// (assistant message + any tool messages).
	Messages []types.Message `json:"messages,omitempty"`

	// Body is the raw response body from the provider (for debugging).
	Body interface{} `json:"body,omitempty"`
}

// OnStartEvent is emitted once when GenerateText or StreamText begins,
// before any LLM call is made.
//
// Cancellation is handled via the ctx parameter — pass ctx to any operations
// that should respect cancellation.
type OnStartEvent struct {
	// CallID uniquely identifies this generation call. Use it to correlate
	// OnStart, OnStepStart, OnStepFinish, and OnFinish events for the same call.
	CallID string

	// OperationID identifies the operation type: "ai.generateText" or "ai.streamText".
	OperationID string

	// Provider is the provider identifier (TS provider).
	Provider string

	// ModelProvider is the provider identifier.
	//
	// Deprecated: use Provider.
	ModelProvider string
	ModelID       string

	// Instructions are the standardized text instructions (TS instructions).
	Instructions string

	// InstructionMessages are the instructions when they were given as
	// system messages (TS instructions: SystemModelMessage[]).
	InstructionMessages []types.Message

	// System is the system prompt.
	//
	// Deprecated: use Instructions.
	System   string
	Prompt   string
	Messages []types.Message
	Tools    []types.Tool

	// ActiveTools limits which tool names are available in this generation.
	ActiveTools []string

	// ToolOrder is the configured order in which tools are sent to the provider.
	ToolOrder []string

	// Tool choice strategy for this generation.
	ToolChoice types.ToolChoice

	// Timeout is the configured timeout, if any.
	Timeout *TimeoutConfig

	// Reasoning is the configured reasoning level, if any.
	Reasoning *types.ReasoningLevel

	// MaxRetries is the maximum number of retries for failed requests.
	MaxRetries int

	// Headers are the additional HTTP headers sent with requests.
	Headers map[string]string

	// Output is the output specification for structured outputs, if configured.
	Output interface{}

	// Additional provider-specific options.
	ProviderOptions map[string]interface{}

	// Generation parameters
	Temperature      *float64
	MaxTokens        *int
	TopP             *float64
	TopK             *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	StopSequences    []string
	Seed             *int

	// User-defined context flowing through the generation lifecycle.
	// Deprecated: set via GenerateTextOptions.RuntimeContext.
	ExperimentalContext interface{}

	// RuntimeContext is the user-defined context flowing through the generation lifecycle.
	RuntimeContext interface{}

	// ToolsContext is the per-tool context map passed through tool approval and execution.
	ToolsContext map[string]interface{}
}

// OnStepStartEvent is emitted at the beginning of each LLM step (before
// calling the provider). StepNumber is 0-indexed.
//
// Cancellation is handled via the ctx parameter — pass ctx to any operations
// that should respect cancellation.
type OnStepStartEvent struct {
	// CallID correlates this event with the other events for this call.
	CallID string

	// StepNumber is 0-indexed
	StepNumber int

	// Provider is the provider identifier for this step (TS provider).
	Provider string

	// ModelProvider is the provider identifier for this step.
	//
	// Deprecated: use Provider.
	ModelProvider string
	ModelID       string

	// Instructions in effect for this step (TS instructions).
	Instructions string

	// InstructionMessages are the step instructions when given as system
	// messages.
	InstructionMessages []types.Message

	// System prompt in effect for this step
	//
	// Deprecated: use Instructions.
	System string

	// Messages being sent to the model for this step
	Messages []types.Message

	// Tools available in this step
	Tools []types.Tool

	// ToolChoice in effect for this step.
	ToolChoice types.ToolChoice

	// ActiveTools limits which tool names are available in this step.
	ActiveTools []string

	// ToolOrder in effect for this step.
	ToolOrder []string

	// ProviderOptions in effect for this step.
	ProviderOptions map[string]interface{}

	// Output is the output specification for structured outputs, if configured.
	Output interface{}

	// Steps contains results from all completed steps before this one. Empty for the first step.
	Steps []types.StepResult

	// PreviousSteps is an alias for Steps kept for backward compatibility.
	// Deprecated: use Steps instead.
	PreviousSteps []types.StepResult

	// User-defined context flowing through the generation lifecycle.
	ExperimentalContext interface{}
	RuntimeContext      interface{}
	ToolsContext        map[string]interface{}
}

// OnToolCallStartEvent is emitted just before a tool's Execute function is
// invoked. It fires once per tool call.
//
// Cancellation is handled via the ctx parameter — pass ctx to any operations
// that should respect cancellation.
type OnToolCallStartEvent struct {
	// CallID correlates this event with the generation call that triggered it.
	CallID string

	// ToolCallID is the unique ID assigned to this specific call
	ToolCallID string

	// ToolName is the name of the tool being invoked
	ToolName string

	// ToolCall is the tool call being executed (TS toolCall).
	ToolCall types.ToolCall

	// ToolContext is the validated per-tool context (TS toolContext).
	ToolContext interface{}

	// Args contains the arguments the model passed to the tool
	//
	// Deprecated: use ToolCall.Arguments. Excluded from JSON so a serialized
	// event matches the TypeScript event shape exactly.
	Args map[string]any `json:"-"`

	// StepNumber is the 0-indexed step in which this tool call occurs.
	//
	// Deprecated: removed from the TypeScript event; correlate with step
	// events instead. Excluded from JSON.
	StepNumber int `json:"-"`

	// ModelProvider and ModelID identify the step model.
	//
	// Deprecated: removed from the TypeScript event. Excluded from JSON.
	ModelProvider string `json:"-"`
	ModelID       string `json:"-"`

	// Messages that were sent to the model to initiate the response that
	// contained the tool call.
	Messages []types.Message

	// User-defined context flowing through the generation lifecycle.
	ExperimentalContext interface{}
	RuntimeContext      interface{}
	ToolsContext        map[string]interface{}
}

// OnToolCallFinishEvent is emitted after a tool's Execute function returns,
// whether it succeeded or failed.
//
// Exactly one of Result or Error will be non-nil on each event:
//   - Result != nil → tool executed successfully
//   - Error != nil  → tool execution failed
type OnToolCallFinishEvent struct {
	// CallID correlates this event with the generation call that triggered it.
	CallID string

	// ToolCallID is the unique ID assigned to this specific call
	ToolCallID string

	// ToolName is the name of the tool that was invoked
	ToolName string

	// ToolCall is the executed tool call (TS toolCall).
	ToolCall types.ToolCall

	// ToolContext is the validated per-tool context (TS toolContext).
	ToolContext interface{}

	// ToolOutput is the tool result or tool error (TS toolOutput). Exactly
	// one of ToolOutput.Result / ToolOutput.Error is meaningful.
	ToolOutput types.ToolResult

	// ToolExecutionMs is the execution time of the tool in milliseconds.
	ToolExecutionMs int64

	// Args contains the arguments the model passed to the tool
	//
	// Deprecated: use ToolCall.Arguments. Excluded from JSON so a serialized
	// event matches the TypeScript event shape exactly.
	Args map[string]any `json:"-"`

	// Result is the tool's return value on success (nil on failure)
	//
	// Deprecated: use ToolOutput.Result. Excluded from JSON.
	Result any `json:"-"`

	// Error is non-nil when the tool execution failed (nil on success)
	//
	// Deprecated: use ToolOutput.Error. Excluded from JSON (a raw error value
	// generally isn't JSON-serializable anyway).
	Error error `json:"-"`

	// DurationMs is the wall-clock execution time of the tool in milliseconds
	//
	// Deprecated: use ToolExecutionMs. Excluded from JSON.
	DurationMs int64 `json:"-"`

	// StepNumber is the 0-indexed step in which this tool call occurred.
	//
	// Deprecated: removed from the TypeScript event. Excluded from JSON.
	StepNumber int `json:"-"`

	// ModelProvider and ModelID identify the step model.
	//
	// Deprecated: removed from the TypeScript event. Excluded from JSON.
	ModelProvider string `json:"-"`
	ModelID       string `json:"-"`

	// Messages available at tool execution time (full conversation context)
	Messages []types.Message

	// User-defined context flowing through the generation lifecycle.
	ExperimentalContext interface{}
	RuntimeContext      interface{}
	ToolsContext        map[string]interface{}
}

// OnStepFinishEvent is emitted at the end of each LLM step, after tool
// results (if any) have been collected. It carries the full step result.
type OnStepFinishEvent struct {
	// CallID correlates this event with the other events for this call.
	CallID string

	// StepNumber is 0-indexed
	StepNumber int

	// Model identifies the provider and model that produced this step.
	Model types.StepModel

	// ModelProvider is the provider name. Deprecated: use Model.Provider.
	ModelProvider string
	// ModelID is the model identifier. Deprecated: use Model.ModelID.
	ModelID string

	// Text produced by the model in this step
	Text string

	// Reasoning holds reasoning/thinking content parts from this step.
	Reasoning []types.ReasoningContent

	// ReasoningText is the concatenated reasoning text from this step.
	ReasoningText string

	// ToolCalls made by the model in this step
	ToolCalls []types.ToolCall

	// StaticToolCalls are tool calls from non-dynamic (typed) tools.
	StaticToolCalls []types.ToolCall

	// DynamicToolCalls are tool calls from dynamically registered tools.
	DynamicToolCalls []types.ToolCall

	// ToolResults collected for this step
	ToolResults []types.ToolResult

	// StaticToolResults are results from non-dynamic (typed) tools.
	StaticToolResults []types.ToolResult

	// DynamicToolResults are results from dynamically registered tools.
	DynamicToolResults []types.ToolResult

	// FinishReason explains why the step ended
	FinishReason types.FinishReason

	// Usage reports token consumption for this step
	Usage types.Usage

	// Warnings emitted by the provider during this step
	Warnings []types.Warning

	// RawFinishReason is the raw finish reason string from the provider.
	RawFinishReason string

	// Sources contains citation or grounding references from this step.
	Sources []types.SourceContent

	// Files contains model-generated output files from this step.
	Files []types.GeneratedFileContent

	// ProviderMetadata holds provider-specific metadata for this step.
	ProviderMetadata map[string]interface{}

	// ResponseHeaders are the raw HTTP response headers.
	// Deprecated: use Response.Headers instead.
	ResponseHeaders map[string]string

	// Request contains additional information about the request sent to the provider.
	Request GenerateStepRequest

	// Response contains additional information about the response from the provider.
	Response GenerateStepResponse

	// User-defined context flowing through the generation lifecycle.
	ExperimentalContext interface{}
	RuntimeContext      interface{}
	ToolsContext        map[string]interface{}
}

// OnFinishEvent is emitted once when the entire GenerateText or StreamText
// call completes (all steps finished).
type OnFinishEvent struct {
	// CallID correlates this event with the other events for this call.
	CallID string

	// StepNumber is the step number of the final step.
	StepNumber int

	// Model identifies the provider and model that produced the final step.
	Model types.StepModel

	// ModelProvider is the provider name. Deprecated: use Model.Provider.
	ModelProvider string
	// ModelID is the model identifier. Deprecated: use Model.ModelID.
	ModelID string

	// Text is the final generated text
	Text string

	// Reasoning holds reasoning/thinking content parts from the final step.
	Reasoning []types.ReasoningContent

	// ReasoningText is the concatenated reasoning text from the final step.
	ReasoningText string

	// ToolCalls from the final step
	ToolCalls []types.ToolCall

	// StaticToolCalls are tool calls from non-dynamic (typed) tools in the final step.
	StaticToolCalls []types.ToolCall

	// DynamicToolCalls are tool calls from dynamically registered tools in the final step.
	DynamicToolCalls []types.ToolCall

	// ToolResults from the final step
	ToolResults []types.ToolResult

	// StaticToolResults are results from non-dynamic (typed) tools in the final step.
	StaticToolResults []types.ToolResult

	// DynamicToolResults are results from dynamically registered tools in the final step.
	DynamicToolResults []types.ToolResult

	// FinishReason of the last step
	FinishReason types.FinishReason

	// Usage is the token usage of the final (last) step.
	// For single-step generation, Usage == TotalUsage.
	Usage types.Usage

	// Steps contains the full result of every step.
	Steps []types.StepResult

	// TotalUsage is the sum of token usage across all steps.
	TotalUsage types.Usage

	// RawFinishReason is the raw finish reason string from the provider.
	RawFinishReason string

	// Sources contains citation or grounding references from the final step.
	Sources []types.SourceContent

	// Files contains model-generated output files from all steps.
	Files []types.GeneratedFileContent

	// ProviderMetadata holds provider-specific metadata from the final step.
	ProviderMetadata map[string]interface{}

	// Warnings aggregated across all steps
	Warnings []types.Warning

	// ResponseHeaders are the raw HTTP response headers from the last provider call.
	// Deprecated: use Response.Headers instead.
	ResponseHeaders map[string]string

	// Request contains additional information about the last request sent.
	Request GenerateStepRequest

	// Response contains additional information about the last provider response.
	Response GenerateStepResponse

	// User-defined context in its final state after all steps.
	ExperimentalContext interface{}
	RuntimeContext      interface{}
	ToolsContext        map[string]interface{}
}

// Canonical event type name aliases. The deprecated On* names remain for backward compatibility.

// GenerateTextStartEvent is the canonical name for OnStartEvent.
type GenerateTextStartEvent = OnStartEvent

// GenerateTextStepStartEvent is the canonical name for OnStepStartEvent.
type GenerateTextStepStartEvent = OnStepStartEvent

// GenerateTextStepEndEvent is the canonical name for OnStepFinishEvent.
type GenerateTextStepEndEvent = OnStepFinishEvent

// OnStepEndEvent is the canonical name for OnStepFinishEvent.
type OnStepEndEvent = OnStepFinishEvent

// GenerateTextEndEvent is the canonical name for OnFinishEvent.
type GenerateTextEndEvent = OnFinishEvent

// ToolExecutionStartEvent is the canonical name for OnToolCallStartEvent.
type ToolExecutionStartEvent = OnToolCallStartEvent

// ToolExecutionEndEvent is the canonical name for OnToolCallFinishEvent.
type ToolExecutionEndEvent = OnToolCallFinishEvent

// LanguageModelCallPerformance contains performance metrics for a single
// provider model call.
type LanguageModelCallPerformance = telemetry.LanguageModelCallPerformance

// LanguageModelCallStartEvent is passed to OnLanguageModelCallStart
// immediately before each provider model call begins. Unlike OnStepStart, it
// only represents model invocation work. Mirrors TS LanguageModelCallStartEvent.
type LanguageModelCallStartEvent struct {
	// CallID correlates this event with the other events of the call.
	CallID string

	// Provider and ModelID identify the model that is called.
	Provider string
	ModelID  string

	// Instructions are the step's text instructions.
	Instructions string

	// InstructionMessages are the step's instructions when given as system
	// messages.
	InstructionMessages []types.Message

	// Messages are the step's input messages.
	Messages []types.Message

	// Tools are the prepared tool definitions sent to the model.
	Tools []types.Tool

	// Call settings.
	MaxOutputTokens  *int
	Temperature      *float64
	TopP             *float64
	TopK             *int
	PresencePenalty  *float64
	FrequencyPenalty *float64
	StopSequences    []string
	Seed             *int
	Reasoning        *types.ReasoningLevel
}

// LanguageModelCallEndEvent is passed to OnLanguageModelCallEnd after the
// model response has been normalized and parsed, but before any client-side
// tool execution begins. Mirrors TS LanguageModelCallEndEvent.
type LanguageModelCallEndEvent struct {
	// CallID correlates this event with the other events of the call.
	CallID string

	// Provider is the provider identifier.
	Provider string

	// ModelID is the model that produced the response (the response model ID
	// when the provider reports one, otherwise the requested model).
	ModelID string

	// FinishReason is the unified finish reason of the model call.
	FinishReason types.FinishReason

	// Usage is the token usage reported by the model call.
	Usage types.Usage

	// Content are the (parsed) content parts produced by the model call.
	Content []types.ContentPart

	// ResponseID is the provider-returned response id.
	ResponseID string

	// ProviderMetadata is optional provider-specific metadata.
	ProviderMetadata map[string]interface{}

	// Performance contains performance metrics for the model call.
	Performance LanguageModelCallPerformance
}

// OnLanguageModelCallStartCallback is the callback type for
// OnLanguageModelCallStart.
type OnLanguageModelCallStartCallback = func(ctx context.Context, e LanguageModelCallStartEvent)

// OnLanguageModelCallEndCallback is the callback type for
// OnLanguageModelCallEnd.
type OnLanguageModelCallEndCallback = func(ctx context.Context, e LanguageModelCallEndEvent)

func firstLMCallStart(stable, experimental OnLanguageModelCallStartCallback) OnLanguageModelCallStartCallback {
	if stable != nil {
		return stable
	}
	return experimental
}

func firstLMCallEnd(stable, experimental OnLanguageModelCallEndCallback) OnLanguageModelCallEndCallback {
	if stable != nil {
		return stable
	}
	return experimental
}

// GenerateTextAbortEvent is fired when generation is aborted by context
// cancellation or a timeout, exposing the call ID, the completed steps so
// far, and the abort reason. Mirrors TS generate-text-events.ts's onAbort
// event (audit row a8e8ad0 / WG5).
type GenerateTextAbortEvent struct {
	// CallID correlates this event with the other events for this call.
	CallID string

	// Steps contains every step that completed before the abort.
	Steps []types.StepResult

	// Reason is why the call was aborted: context.Cause(ctx) when available,
	// else ctx.Err().
	Reason error
}

// OnAbortCallback is the callback type for OnAbortEvent.
type OnAbortCallback = func(ctx context.Context, e GenerateTextAbortEvent)

// firstOnAbort returns stable if non-nil, else it adapts the deprecated
// OnAbort(ctx, steps) callback (if set) into the stable shape.
func firstOnAbort(stable OnAbortCallback, deprecated func(ctx context.Context, steps []types.StepResult)) OnAbortCallback {
	if stable != nil {
		return stable
	}
	if deprecated == nil {
		return nil
	}
	return func(ctx context.Context, e GenerateTextAbortEvent) {
		deprecated(ctx, e.Steps)
	}
}
