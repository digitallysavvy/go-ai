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
	Body interface{}
}

// GenerateStepResponse contains additional information about the response
// received from the provider for a single generation step.
// Mirrors LanguageModelResponseMetadata & {messages, body} in the TypeScript SDK.
type GenerateStepResponse struct {
	// ID is the provider-assigned response identifier when available.
	// For streaming paths, this may be a generated fallback value.
	ID string

	// Timestamp is when the provider started generating the response.
	// Zero value means it was not available.
	Timestamp time.Time

	// ModelID is the model that handled the request, when available.
	ModelID string

	// Headers are the raw HTTP response headers from the provider.
	Headers map[string]string

	// Messages are the response messages generated in this step
	// (assistant message + any tool messages).
	Messages []types.Message

	// Body is the raw response body from the provider (for debugging).
	Body interface{}
}

// OnStartEvent is emitted once when GenerateText or StreamText begins,
// before any LLM call is made.
//
// Note on cancellation: the TypeScript SDK includes an abortSignal field on this
// event. In Go, the ctx parameter passed to the callback serves this role —
// pass ctx to any operations that should respect cancellation.
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

	// Tool choice strategy for this generation.
	ToolChoice types.ToolChoice

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
	// Set via GenerateTextOptions.ExperimentalContext.
	ExperimentalContext interface{}

	// Telemetry / observability
	FunctionID string
	Metadata   map[string]any
}

// OnStepStartEvent is emitted at the beginning of each LLM step (before
// calling the provider). StepNumber is 1-indexed.
//
// Note on cancellation: the TypeScript SDK includes an abortSignal field on this
// event. In Go, the ctx parameter passed to the callback serves this role —
// pass ctx to any operations that should respect cancellation.
type OnStepStartEvent struct {
	// CallID correlates this event with the other events for this call.
	CallID string

	// StepNumber is 1-indexed
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

	// PreviousSteps contains results from all completed steps before this one.
	// Empty for the first step.
	PreviousSteps []types.StepResult

	// User-defined context flowing through the generation lifecycle.
	ExperimentalContext interface{}

	// Telemetry / observability
	FunctionID string
	Metadata   map[string]any
}

// OnToolCallStartEvent is emitted just before a tool's Execute function is
// invoked. It fires once per tool call.
//
// Note on cancellation: the TypeScript SDK includes an abortSignal field on this
// event. In Go, the ctx parameter passed to the callback serves this role —
// pass ctx to any operations that should respect cancellation.
type OnToolCallStartEvent struct {
	// CallID correlates this event with the generation call that triggered it.
	CallID string

	// ToolCallID is the unique ID assigned to this specific call
	ToolCallID string

	// ToolName is the name of the tool being invoked
	ToolName string

	// Args contains the arguments the model passed to the tool
	Args map[string]any

	// StepNumber is the 1-indexed step in which this tool call occurs
	StepNumber int

	// Model provider and ID for the step that produced this tool call
	ModelProvider string
	ModelID       string

	// Messages available at tool execution time (full conversation context)
	Messages []types.Message

	// User-defined context flowing through the generation lifecycle.
	ExperimentalContext interface{}

	// Telemetry / observability
	FunctionID string
	Metadata   map[string]any
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

	// StepNumber is the 1-indexed step in which this tool call occurred
	StepNumber int

	// Model provider and ID for the step that produced this tool call
	ModelProvider string
	ModelID       string

	// Messages available at tool execution time (full conversation context)
	Messages []types.Message

	// User-defined context flowing through the generation lifecycle.
	ExperimentalContext interface{}

	// Telemetry / observability
	FunctionID string
	Metadata   map[string]any
}

// OnStepFinishEvent is emitted at the end of each LLM step, after tool
// results (if any) have been collected. It carries the full step result.
type OnStepFinishEvent struct {
	// CallID correlates this event with the other events for this call.
	CallID string

	// StepNumber is 1-indexed
	StepNumber int

	// Model provider and ID for this step
	ModelProvider string
	ModelID       string

	// Text produced by the model in this step
	Text string

	// ToolCalls made by the model in this step
	ToolCalls []types.ToolCall

	// ToolResults collected for this step
	ToolResults []types.ToolResult

	// FinishReason explains why the step ended
	FinishReason types.FinishReason

	// Usage reports token consumption for this step
	Usage types.Usage

	// Warnings emitted by the provider during this step
	Warnings []types.Warning

	// RawFinishReason is the raw finish reason string from the provider.
	// Mirrors StepResult.rawFinishReason in the TypeScript SDK.
	RawFinishReason string

	// Sources contains citation or grounding references from this step.
	// Mirrors StepResult.sources in the TypeScript SDK.
	Sources []types.SourceContent

	// Files contains model-generated output files from this step.
	// Mirrors StepResult.files in the TypeScript SDK.
	Files []types.GeneratedFileContent

	// ProviderMetadata holds provider-specific metadata for this step.
	// Mirrors StepResult.providerMetadata in the TypeScript SDK.
	ProviderMetadata map[string]interface{}

	// ResponseHeaders are the raw HTTP response headers from the provider for
	// this step. Mirrors StepResult.response.headers in the TypeScript SDK.
	// Nil for non-HTTP providers or when the provider did not emit headers.
	// Deprecated: use Response.Headers instead.
	ResponseHeaders map[string]string

	// Request contains additional information about the request sent to the provider.
	// Mirrors StepResult.request in the TypeScript SDK.
	Request GenerateStepRequest

	// Response contains additional information about the response from the provider.
	// Mirrors StepResult.response in the TypeScript SDK.
	Response GenerateStepResponse

	// User-defined context flowing through the generation lifecycle.
	ExperimentalContext interface{}

	// Telemetry / observability
	FunctionID string
	Metadata   map[string]any
}

// OnFinishEvent is emitted once when the entire GenerateText or StreamText
// call completes (all steps finished).
type OnFinishEvent struct {
	// CallID correlates this event with the other events for this call.
	CallID string

	// Text is the final generated text
	Text string

	// ToolCalls aggregated across all steps
	ToolCalls []types.ToolCall

	// ToolResults aggregated across all steps
	ToolResults []types.ToolResult

	// FinishReason of the last step
	FinishReason types.FinishReason

	// Usage is the token usage of the final (last) step.
	// Mirrors OnFinishEvent.usage (inherited from StepResult) in the TypeScript SDK.
	// For single-step generation, Usage == TotalUsage.
	Usage types.Usage

	// Steps contains the full result of every step
	Steps []types.StepResult

	// TotalUsage is the sum of token usage across all steps
	TotalUsage types.Usage

	// RawFinishReason is the raw finish reason string from the provider.
	// Mirrors OnFinishEvent.rawFinishReason in the TypeScript SDK.
	RawFinishReason string

	// Sources contains citation or grounding references from the final step.
	// Mirrors OnFinishEvent.sources in the TypeScript SDK.
	Sources []types.SourceContent

	// Files contains model-generated output files from the final step.
	// Mirrors OnFinishEvent.files in the TypeScript SDK.
	Files []types.GeneratedFileContent

	// ProviderMetadata holds provider-specific metadata from the final step.
	// Mirrors OnFinishEvent.providerMetadata in the TypeScript SDK.
	ProviderMetadata map[string]interface{}

	// Warnings aggregated across all steps
	Warnings []types.Warning

	// ResponseHeaders are the raw HTTP response headers from the last provider
	// call. Mirrors OnFinishEvent.response.headers in the TypeScript SDK.
	// Nil for non-HTTP providers or when the provider did not emit headers.
	// Deprecated: use Response.Headers instead.
	ResponseHeaders map[string]string

	// Request contains additional information about the last request sent.
	// Mirrors OnFinishEvent.request in the TypeScript SDK.
	Request GenerateStepRequest

	// Response contains additional information about the last provider response.
	// Mirrors OnFinishEvent.response in the TypeScript SDK.
	Response GenerateStepResponse

	// User-defined context in its final state after all steps.
	ExperimentalContext interface{}

	// Telemetry / observability
	FunctionID string
	Metadata   map[string]any
}
