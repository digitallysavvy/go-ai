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

	// Total usage across all steps
	Usage types.Usage

	// Warnings from any step
	Warnings []types.Warning
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

	// Maximum number of steps (iterations) the agent can take
	MaxSteps int

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

	// ExperimentalDownload enables file download support in agents
	// When enabled, agents can download files from URLs and process them
	ExperimentalDownload bool

	// ========================================================================
	// Callbacks
	// ========================================================================

	OnStepStart  func(stepNum int)
	OnStepFinish func(step types.StepResult)
	OnToolCall   func(toolCall types.ToolCall)
	OnToolResult func(toolResult types.ToolResult)
	OnFinish     func(result *AgentResult)

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
