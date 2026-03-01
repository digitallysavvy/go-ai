package ai

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// OnStartEvent is emitted once when GenerateText or StreamText begins,
// before any LLM call is made.
type OnStartEvent struct {
	// Model provider and ID
	ModelProvider string
	ModelID       string

	// Input configuration
	System   string
	Prompt   string
	Messages []types.Message
	Tools    []types.Tool

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
type OnStepStartEvent struct {
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
type OnToolCallStartEvent struct {
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

	// User-defined context flowing through the generation lifecycle.
	ExperimentalContext interface{}

	// Telemetry / observability
	FunctionID string
	Metadata   map[string]any
}

// OnFinishEvent is emitted once when the entire GenerateText or StreamText
// call completes (all steps finished).
type OnFinishEvent struct {
	// Text is the final generated text
	Text string

	// ToolCalls aggregated across all steps
	ToolCalls []types.ToolCall

	// ToolResults aggregated across all steps
	ToolResults []types.ToolResult

	// FinishReason of the last step
	FinishReason types.FinishReason

	// Steps contains the full result of every step
	Steps []types.StepResult

	// TotalUsage is the sum of token usage across all steps
	TotalUsage types.Usage

	// Warnings aggregated across all steps
	Warnings []types.Warning

	// User-defined context in its final state after all steps.
	ExperimentalContext interface{}

	// Telemetry / observability
	FunctionID string
	Metadata   map[string]any
}
