package agent

import (
	"context"

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

	// Final finish reason
	FinishReason types.FinishReason

	// Total usage across all steps
	Usage types.Usage

	// Warnings from any step
	Warnings []types.Warning
}

// AgentConfig contains configuration for an agent
type AgentConfig struct {
	// Model to use for the agent
	Model provider.LanguageModel

	// System prompt for the agent
	System string

	// Tools available to the agent
	Tools []types.Tool

	// Maximum number of steps (iterations) the agent can take
	MaxSteps int

	// Temperature for generation
	Temperature *float64

	// MaxTokens per generation
	MaxTokens *int

	// Callbacks
	OnStepStart  func(stepNum int)
	OnStepFinish func(step types.StepResult)
	OnToolCall   func(toolCall types.ToolCall)
	OnToolResult func(toolResult types.ToolResult)
	OnFinish     func(result *AgentResult)

	// ToolApprovalRequired determines if tools require approval before execution
	ToolApprovalRequired bool

	// ToolApprover is called when a tool needs approval (if ToolApprovalRequired is true)
	// Should return true to approve, false to reject
	ToolApprover func(toolCall types.ToolCall) bool
}

// DefaultAgentConfig returns a config with sensible defaults
func DefaultAgentConfig() AgentConfig {
	maxSteps := 10
	return AgentConfig{
		MaxSteps:             maxSteps,
		ToolApprovalRequired: false,
	}
}
