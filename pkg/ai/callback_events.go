package ai

import (
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

	// Model provider and ID
	ModelProvider string
	ModelID       string

	// Input configuration
	System   string
	Prompt   string
	Messages []types.Message
	Tools    []types.Tool

	// ActiveTools limits which tool names are available in this generation.
	ActiveTools []string

	// Tool choice strategy for this generation.
	ToolChoice types.ToolChoice

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

	// Model provider and ID for this step
	ModelProvider string
	ModelID       string

	// System prompt in effect for this step
	System string

	// Messages being sent to the model for this step
	Messages []types.Message

	// Tools available in this step
	Tools []types.Tool

	// ActiveTools limits which tool names are available in this step.
	ActiveTools []string

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

	// Args contains the arguments the model passed to the tool
	Args map[string]any

	// StepNumber is the 0-indexed step in which this tool call occurs
	StepNumber int

	// Model provider and ID for the step that produced this tool call
	ModelProvider string
	ModelID       string

	// Messages available at tool execution time (full conversation context)
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

	// Args contains the arguments the model passed to the tool
	Args map[string]any

	// Result is the tool's return value on success (nil on failure)
	Result any

	// Error is non-nil when the tool execution failed (nil on success)
	Error error

	// DurationMs is the wall-clock execution time of the tool in milliseconds
	DurationMs int64

	// StepNumber is the 0-indexed step in which this tool call occurred
	StepNumber int

	// Model provider and ID for the step that produced this tool call
	ModelProvider string
	ModelID       string

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

// GenerateTextEndEvent is the canonical name for OnFinishEvent.
type GenerateTextEndEvent = OnFinishEvent
