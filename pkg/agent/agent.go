package agent

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Agent represents an autonomous agent that can use tools to accomplish tasks
type Agent interface {
	// Execute runs the agent with the given prompt and returns the final result
	Execute(ctx context.Context, prompt string) (*AgentResult, error)

	// ExecuteWithMessages runs the agent with a message history
	ExecuteWithMessages(ctx context.Context, messages []types.Message) (*AgentResult, error)
}

// AgentResult contains the result of an agent execution
type AgentResult struct {
	// Final text output
	Text string

	// All steps taken by the agent
	Steps []types.StepResult

	// All tool results from agent execution
	ToolResults []types.ToolResult

	// Subagent delegations during execution
	Delegations []SubagentDelegation

	// Final finish reason
	FinishReason types.FinishReason

	// StopReason is the reason string from the StopCondition that stopped the loop.
	// Empty if the agent ended naturally or was not using custom stop conditions.
	StopReason string

	// Total usage across all steps
	Usage types.Usage

	// Warnings from any step
	Warnings []types.Warning
}

// AgentAction represents an action the agent has decided to take
// This is typically a tool call that the agent wants to execute
type AgentAction struct {
	// The tool being called
	ToolCall types.ToolCall

	// The step number when this action was decided
	StepNumber int

	// Optional: reasoning or thought process behind the action
	Reasoning string

	// Run tracking (v6.0.61+)
	// RunID is a unique identifier for this agent execution chain
	// Use this to correlate all actions and events from a single agent run
	RunID string

	// ParentRunID is the ID of the parent run if this is a subagent or nested execution
	// Empty for top-level agent runs
	ParentRunID string

	// Tags are user-defined labels for categorizing and filtering agent runs
	// Examples: ["production", "experiment", "debug"], ["user:123", "session:abc"]
	Tags []string
}

// AgentFinish represents the agent's final decision to complete execution
// This is called when the agent has a final answer and no more tools to call
type AgentFinish struct {
	// The final output text from the agent
	Output string

	// The step number when the agent finished
	StepNumber int

	// The finish reason
	FinishReason types.FinishReason

	// Optional: any additional metadata about the completion
	Metadata map[string]interface{}

	// Run tracking (v6.0.61+)
	// RunID is a unique identifier for this agent execution chain
	// Use this to correlate all actions and events from a single agent run
	RunID string

	// ParentRunID is the ID of the parent run if this is a subagent or nested execution
	// Empty for top-level agent runs
	ParentRunID string

	// Tags are user-defined labels for categorizing and filtering agent runs
	// Examples: ["production", "experiment", "debug"], ["user:123", "session:abc"]
	Tags []string
}

// AgentConfig contains configuration for an agent
type AgentConfig struct {
	// ========================================================================
	// Identity (v6.0.41 - NEW)
	// ========================================================================

	// ID is a unique identifier for the agent
	// Useful for tracking and logging which agent performed actions
	ID string

	// Version is the version of the agent (e.g., "agent-v1", "1.0.0")
	// Useful for versioning agent behavior and configurations
	Version string

	// ========================================================================
	// Core Configuration
	// ========================================================================

	// Model to use for the agent
	Model provider.LanguageModel

	// System prompt for the agent
	System string

	// Tools available to the agent
	Tools []types.Tool

	// Skills are reusable agent behaviors
	// Skills can be registered and executed by the agent
	Skills *SkillRegistry

	// Subagents are specialized agents that can be delegated to
	// The main agent can delegate tasks to subagents for specialized processing
	Subagents *SubagentRegistry

	// Maximum number of steps (iterations) the agent can take.
	// If both MaxSteps and StopWhen are set, StopWhen takes precedence.
	MaxSteps int

	// StopWhen defines conditions that terminate the agent's tool-calling loop.
	// Conditions are evaluated OR -- first non-empty string stops the loop.
	// If neither StopWhen nor MaxSteps is set, defaults to []ai.StopCondition{ai.StepCountIs(1)}.
	StopWhen []ai.StopCondition

	// Temperature for generation
	Temperature *float64

	// MaxTokens per generation
	MaxTokens *int

	// Timeout provides granular timeout controls
	// Supports total timeout, per-step timeout, and per-chunk timeout
	Timeout *ai.TimeoutConfig

	// ========================================================================
	// Dynamic Configuration (v6.0.41 - NEW)
	// ========================================================================

	// PrepareCall is called before each generation step
	// Allows dynamic modification of the agent's configuration per step
	// This is useful for:
	// - Adjusting system prompts based on context
	// - Changing temperature/parameters dynamically
	// - Modifying available tools based on state
	// - Adding contextual information
	//
	// Example:
	//   PrepareCall: func(ctx context.Context, config PrepareCallConfig) PrepareCallConfig {
	//       // Adjust system prompt based on step number
	//       if config.StepNumber > 5 {
	//           config.System = "Be more concise."
	//       }
	//       return config
	//   }
	PrepareCall func(ctx context.Context, config PrepareCallConfig) PrepareCallConfig

	// ========================================================================
	// Experimental Features (v6.0.41 - NEW)
	// ========================================================================

	// ExperimentalContext is user-defined context that flows through all
	// structured callback events (OnStartEvent, OnStepStartEvent, etc.).
	// It is passed as-is to every event — useful for correlating events with
	// application-level state (e.g. request IDs, session objects).
	ExperimentalContext interface{}

	// ExperimentalDownload enables file download support in agents
	// When enabled, agents can download files from URLs and process them
	ExperimentalDownload bool

	// ========================================================================
	// Callbacks
	// ========================================================================

	// Legacy/Basic Callbacks
	OnStepStart  func(stepNum int)
	OnStepFinish func(step types.StepResult)
	OnToolCall   func(toolCall types.ToolCall)
	OnToolResult func(toolResult types.ToolResult)
	OnFinish     func(result *AgentResult)

	// ========================================================================
	// Structured Event Callbacks (v6.1 - P0-3)
	// These callbacks receive typed event structs and are panic-safe.
	// They fire in addition to (not instead of) the legacy callbacks above.
	// They are merged with per-call callbacks via mergeCallbacks.
	// ========================================================================

	// OnStart is called once when agent execution begins.
	OnStart func(ctx context.Context, e ai.OnStartEvent)

	// OnStepStart is called at the beginning of each LLM step.
	OnStepStartEvent func(ctx context.Context, e ai.OnStepStartEvent)

	// OnToolCallStart is called just before each tool's Execute function runs.
	OnToolCallStart func(ctx context.Context, e ai.OnToolCallStartEvent)

	// OnToolCallFinish is called after each tool's Execute function returns.
	OnToolCallFinish func(ctx context.Context, e ai.OnToolCallFinishEvent)

	// OnStepFinishEvent is called at the end of each LLM step.
	OnStepFinishEvent func(ctx context.Context, e ai.OnStepFinishEvent)

	// OnFinishEvent is called once when agent execution completes.
	OnFinishEvent func(ctx context.Context, e ai.OnFinishEvent)

	// LangChain/LangGraph-Style Callbacks (v6.0.60+)
	// These callbacks provide more granular control over agent execution
	// and align with LangChain's callback system for better interoperability

	// OnChainStart is called when the agent begins execution
	// Use this to initialize resources, start timers, or log the beginning of a chain
	OnChainStart func(input string, messages []types.Message)

	// OnChainEnd is called when the agent completes successfully
	// Use this to clean up resources, log completion, or process final results
	OnChainEnd func(result *AgentResult)

	// OnChainError is called when the agent encounters an error during execution
	// Use this for error logging, cleanup, or triggering fallback mechanisms
	OnChainError func(err error)

	// OnAgentAction is called when the agent decides to take an action (e.g., call a tool)
	// This is invoked before tool execution begins
	// Use this to log agent decisions or implement custom action approval logic
	OnAgentAction func(action AgentAction)

	// OnAgentFinish is called when the agent reaches a final answer (no more tool calls)
	// This differs from OnChainEnd as it's called when the agent decides it's done,
	// even if more processing remains
	OnAgentFinish func(finish AgentFinish)

	// OnToolStart is called immediately before a tool begins execution
	// Use this to log tool invocations or start performance timers
	OnToolStart func(toolCall types.ToolCall)

	// OnToolEnd is called after a tool successfully completes execution
	// Use this to log successful tool completions or process tool outputs
	OnToolEnd func(toolResult types.ToolResult)

	// OnToolError is called when a tool execution fails
	// Use this for error logging, retry logic, or fallback mechanisms
	OnToolError func(toolCall types.ToolCall, err error)

	// ========================================================================
	// Tool Approval
	// ========================================================================

	// ToolApprovalRequired determines if tools require approval before execution
	ToolApprovalRequired bool

	// ToolApprover is called when a tool needs approval (if ToolApprovalRequired is true)
	// Should return true to approve, false to reject
	ToolApprover func(toolCall types.ToolCall) bool
}

// PrepareCallConfig contains configuration that can be modified before each call
type PrepareCallConfig struct {
	// StepNumber is the current step number
	StepNumber int

	// System prompt for this call
	System string

	// Messages for this call
	Messages []types.Message

	// Tools available for this call
	Tools []types.Tool

	// Temperature for this call
	Temperature *float64

	// MaxTokens for this call
	MaxTokens *int

	// AccumulatedUsage is the total usage so far
	AccumulatedUsage types.Usage

	// CustomData allows passing custom data between PrepareCall invocations
	CustomData interface{}
}

// DefaultAgentConfig returns a config with sensible defaults
func DefaultAgentConfig() AgentConfig {
	maxSteps := 10
	return AgentConfig{
		MaxSteps:             maxSteps,
		ToolApprovalRequired: false,
	}
}
