package anthropic

// ContextManagement configures automatic history cleanup for long conversations.
// This is a beta feature that requires the "context-management-2025-01-22" beta header.
//
// Anthropic's context management automatically removes or truncates content from
// conversation history to prevent hitting context window limits while maintaining
// conversation flow.
type ContextManagement struct {
	// Strategies for context cleanup.
	// Valid values:
	//   - "clear_tool_uses": Removes old tool call/result pairs, keeping only final results
	//   - "clear_thinking": Removes extended thinking blocks from previous turns
	//
	// Multiple strategies can be combined for maximum context savings.
	Strategies []string `json:"strategies,omitempty"`
}

// ContextManagementResponse contains the results of automatic context cleanup
// applied by Anthropic's API. These statistics help you understand how much
// content was removed from the conversation history.
type ContextManagementResponse struct {
	// MessagesDeleted is the number of complete messages removed from history
	MessagesDeleted int `json:"messages_deleted"`

	// MessagesTruncated is the number of messages that were partially truncated
	MessagesTruncated int `json:"messages_truncated"`

	// ToolUsesCleared is the number of tool use/result pairs that were removed
	// (when using "clear_tool_uses" strategy)
	ToolUsesCleared int `json:"tool_uses_cleared"`

	// ThinkingBlocksCleared is the number of thinking blocks that were removed
	// (when using "clear_thinking" strategy)
	ThinkingBlocksCleared int `json:"thinking_blocks_cleared"`
}

// Strategy constants for context management
const (
	// StrategyClearToolUses removes old tool call/result pairs from history.
	// Use this for long conversations with many tool calls.
	// Keeps final tool results, removes intermediate calls.
	// Best for: Agentic workflows, multi-turn tool use
	StrategyClearToolUses = "clear_tool_uses"

	// StrategyClearThinking removes extended thinking blocks from history.
	// Use this for tasks with extended reasoning.
	// Keeps final conclusions, removes thinking process.
	// Best for: Complex analysis, long-form reasoning
	StrategyClearThinking = "clear_thinking"
)

// BetaHeaderContextManagement is the beta header value required for context management
const BetaHeaderContextManagement = "context-management-2025-01-22"
