package anthropic

// ContextManagementEdit represents a single context management edit configuration.
// The Anthropic API supports different types of edits for managing conversation context:
// - clear_tool_uses_20250919: Removes old tool calls and results
// - clear_thinking_20251015: Removes extended thinking blocks
// - compact_20260112: Compacts (summarizes) the conversation
type ContextManagementEdit interface {
	contextManagementEdit()
}

// ContextManagement configures automatic conversation context management.
// This feature helps prevent context window overflows by automatically managing
// message history through various edit strategies.
//
// Requires beta headers:
//   - "context-management-2025-06-27" for clear_tool_uses and clear_thinking
//   - "compact-2026-01-12" for compact edits
type ContextManagement struct {
	Edits []ContextManagementEdit `json:"edits"`
}

// ClearToolUsesEdit removes old tool calls and their results from conversation history.
// This is useful for long agentic workflows where intermediate tool calls aren't needed.
type ClearToolUsesEdit struct {
	Type string `json:"type"` // Always "clear_tool_uses_20250919"

	// Trigger specifies when this edit should be applied
	Trigger *ClearToolUsesTrigger `json:"trigger,omitempty"`

	// Keep specifies how many recent tool uses to preserve
	Keep *ClearToolUsesKeep `json:"keep,omitempty"`

	// ClearAtLeast specifies minimum tokens to clear when triggered
	ClearAtLeast *ClearToolUsesClearAtLeast `json:"clearAtLeast,omitempty"`

	// ClearToolInputs controls whether to clear tool input content (default: true)
	ClearToolInputs *bool `json:"clearToolInputs,omitempty"`

	// ExcludeTools lists tool names that should never be cleared
	ExcludeTools []string `json:"excludeTools,omitempty"`
}

// ClearToolUsesTrigger defines when to trigger tool use clearing
type ClearToolUsesTrigger struct {
	Type  string `json:"type"`  // "input_tokens" or "tool_uses"
	Value int    `json:"value"` // Threshold value
}

// ClearToolUsesKeep defines how many tool uses to keep
type ClearToolUsesKeep struct {
	Type  string `json:"type"`  // "tool_uses"
	Value int    `json:"value"` // Number of tool uses to keep
}

// ClearToolUsesClearAtLeast defines minimum tokens to clear
type ClearToolUsesClearAtLeast struct {
	Type  string `json:"type"`  // "input_tokens"
	Value int    `json:"value"` // Minimum tokens to clear
}

func (e *ClearToolUsesEdit) contextManagementEdit() {}

// ClearThinkingEdit removes extended thinking blocks from conversation history.
// This is useful for conversations with complex reasoning where the thinking
// process doesn't need to be preserved.
type ClearThinkingEdit struct {
	Type string `json:"type"` // Always "clear_thinking_20251015"

	// Keep specifies which thinking turns to preserve
	Keep ClearThinkingKeep `json:"keep,omitempty"`
}

// ClearThinkingKeep defines which thinking turns to preserve
type ClearThinkingKeep interface {
	clearThinkingKeep()
}

// KeepAllThinking preserves all thinking turns
type KeepAllThinking struct {
	Value string `json:"-"` // "all" (not serialized, handled specially)
}

func (k *KeepAllThinking) clearThinkingKeep() {}

// KeepRecentThinkingTurns preserves a specific number of recent thinking turns
type KeepRecentThinkingTurns struct {
	Type  string `json:"type"`  // "thinking_turns"
	Value int    `json:"value"` // Number of turns to keep
}

func (k *KeepRecentThinkingTurns) clearThinkingKeep() {}

func (e *ClearThinkingEdit) contextManagementEdit() {}

// CompactEdit compacts (summarizes) the conversation history.
// This is the most aggressive form of context management, where Claude
// summarizes older parts of the conversation into a condensed form.
type CompactEdit struct {
	Type string `json:"type"` // Always "compact_20260112"

	// Trigger specifies when compaction should occur
	Trigger *CompactTrigger `json:"trigger,omitempty"`

	// PauseAfterCompaction pauses generation after compaction to allow
	// the client to inspect the compacted history
	PauseAfterCompaction *bool `json:"pauseAfterCompaction,omitempty"`

	// Instructions provide custom guidance for how to compact the conversation
	Instructions *string `json:"instructions,omitempty"`
}

// CompactTrigger defines when to trigger compaction
type CompactTrigger struct {
	Type  string `json:"type"`  // "input_tokens"
	Value int    `json:"value"` // Token threshold
}

func (e *CompactEdit) contextManagementEdit() {}

// ContextManagementResponse represents the result of context management
// operations applied by the API. This appears in the API response at the
// root level (preferred) or in the usage block (legacy).
type ContextManagementResponse struct {
	AppliedEdits []AppliedEdit `json:"applied_edits"`
}

// AppliedEdit represents a single context management edit that was applied
type AppliedEdit interface {
	appliedEdit()
}

// AppliedClearToolUsesEdit indicates that tool uses were cleared
type AppliedClearToolUsesEdit struct {
	Type               string `json:"type"`                 // "clear_tool_uses_20250919"
	ClearedToolUses    int    `json:"cleared_tool_uses"`    // Number of tool uses cleared
	ClearedInputTokens int    `json:"cleared_input_tokens"` // Tokens cleared
}

func (e *AppliedClearToolUsesEdit) appliedEdit() {}

// AppliedClearThinkingEdit indicates that thinking blocks were cleared
type AppliedClearThinkingEdit struct {
	Type                 string `json:"type"`                   // "clear_thinking_20251015"
	ClearedThinkingTurns int    `json:"cleared_thinking_turns"` // Number of thinking turns cleared
	ClearedInputTokens   int    `json:"cleared_input_tokens"`   // Tokens cleared
}

func (e *AppliedClearThinkingEdit) appliedEdit() {}

// AppliedCompactEdit indicates that compaction occurred
type AppliedCompactEdit struct {
	Type string `json:"type"` // "compact_20260112"
}

func (e *AppliedCompactEdit) appliedEdit() {}

// Beta header constants
const (
	// BetaHeaderContextManagement is required for clear_tool_uses and clear_thinking edits
	BetaHeaderContextManagement = "context-management-2025-06-27"

	// BetaHeaderCompact is required for compact edits
	BetaHeaderCompact = "compact-2026-01-12"

	// BetaHeaderFastMode is required for fast mode (Opus 4.6)
	BetaHeaderFastMode = "fast-mode-2026-02-01"

	// BetaHeaderPromptCaching is required for automatic prompt caching
	BetaHeaderPromptCaching = "prompt-caching-2024-07-31"

	// BetaHeaderCodeExecution is required for the code execution tool (2026-01-20).
	// It is automatically injected when the code execution tool is present in the tool list.
	BetaHeaderCodeExecution = "code-execution-20260120"

	// BetaHeaderFineGrainedToolStreaming is obsolete and retained only for
	// source compatibility with older callers.
	//
	// Deprecated: the provider no longer injects this header.
	BetaHeaderFineGrainedToolStreaming = "fine-grained-tool-streaming-2025-05-14"

	// BetaHeaderTaskBudgets is required when TaskBudget is set.
	BetaHeaderTaskBudgets = "task-budgets-2026-03-13"

	// BetaHeaderMCPClient is required for native MCP server support.
	// It is automatically injected when MCPServers is non-empty.
	BetaHeaderMCPClient = "mcp-client-2025-04-04"

	// BetaHeaderCodeExecution20250825 is required for the container skills system (2025-08-25 version).
	// It is automatically injected when container skills are present.
	// Distinct from BetaHeaderCodeExecution (code-execution-20260120) for the newer standalone tool.
	BetaHeaderCodeExecution20250825 = "code-execution-2025-08-25"

	// BetaHeaderSkills is required for the agent container skills system.
	// It is automatically injected when container skills are present.
	BetaHeaderSkills = "skills-2025-10-02"

	// BetaHeaderFilesAPI is required for the agent container skills system.
	// It is automatically injected when container skills are present.
	BetaHeaderFilesAPI = "files-api-2025-04-14"

	// BetaHeaderWebTools20260209 is required for web_search_20260209 and web_fetch_20260209.
	// Automatically injected when either tool is present in the tool list.
	BetaHeaderWebTools20260209 = "code-execution-web-tools-2026-02-09"

	// BetaHeaderWebFetch20250910 is required for web_fetch_20250910.
	// Automatically injected when that tool is present in the tool list.
	BetaHeaderWebFetch20250910 = "web-fetch-2025-09-10"

	// BetaHeaderComputerUse20241022 is required for computer_20241022, bash_20241022,
	// and text_editor_20241022. Automatically injected when any of those tools are present.
	BetaHeaderComputerUse20241022 = "computer-use-2024-10-22"

	// BetaHeaderComputerUse20250124 is required for computer_20250124, bash_20250124,
	// text_editor_20250124, and text_editor_20250429. Automatically injected when any are present.
	BetaHeaderComputerUse20250124 = "computer-use-2025-01-24"

	// BetaHeaderComputerUse20251124 is required for computer_20251124.
	// Automatically injected when that tool is present in the tool list.
	BetaHeaderComputerUse20251124 = "computer-use-2025-11-24"

	// BetaHeaderCodeExecution20250522 is required for code_execution_20250522.
	// Automatically injected when that tool is present in the tool list.
	BetaHeaderCodeExecution20250522 = "code-execution-2025-05-22"

	// BetaHeaderAdvancedToolUse is required when tools use AllowedCallers or InputExamples.
	// Automatically injected when any tool in the list has either field set.
	BetaHeaderAdvancedToolUse = "advanced-tool-use-2025-11-20"

	// BetaHeaderAdvisorTool is required for advisor_20260301.
	// Automatically injected when that tool is present in the tool list.
	BetaHeaderAdvisorTool = "advisor-tool-2026-03-01"

	// BetaHeaderServerSideFallback is required when fallbacks are configured.
	BetaHeaderServerSideFallback = "server-side-fallback-2026-06-01"
)

// Helper functions for creating edit configurations

// NewClearToolUsesEdit creates a basic clear_tool_uses edit with default settings
func NewClearToolUsesEdit() *ClearToolUsesEdit {
	return &ClearToolUsesEdit{
		Type: "clear_tool_uses_20250919",
	}
}

// WithInputTokensTrigger sets an input token threshold trigger
func (e *ClearToolUsesEdit) WithInputTokensTrigger(tokens int) *ClearToolUsesEdit {
	e.Trigger = &ClearToolUsesTrigger{
		Type:  "input_tokens",
		Value: tokens,
	}
	return e
}

// WithToolUsesTrigger sets a tool uses count trigger
func (e *ClearToolUsesEdit) WithToolUsesTrigger(count int) *ClearToolUsesEdit {
	e.Trigger = &ClearToolUsesTrigger{
		Type:  "tool_uses",
		Value: count,
	}
	return e
}

// WithKeepToolUses sets how many tool uses to keep
func (e *ClearToolUsesEdit) WithKeepToolUses(count int) *ClearToolUsesEdit {
	e.Keep = &ClearToolUsesKeep{
		Type:  "tool_uses",
		Value: count,
	}
	return e
}

// WithClearAtLeast sets minimum tokens to clear
func (e *ClearToolUsesEdit) WithClearAtLeast(tokens int) *ClearToolUsesEdit {
	e.ClearAtLeast = &ClearToolUsesClearAtLeast{
		Type:  "input_tokens",
		Value: tokens,
	}
	return e
}

// WithExcludeTools sets tools that should not be cleared
func (e *ClearToolUsesEdit) WithExcludeTools(tools ...string) *ClearToolUsesEdit {
	e.ExcludeTools = tools
	return e
}

// NewClearThinkingEdit creates a basic clear_thinking edit with default settings
func NewClearThinkingEdit() *ClearThinkingEdit {
	return &ClearThinkingEdit{
		Type: "clear_thinking_20251015",
	}
}

// WithKeepAll preserves all thinking turns
func (e *ClearThinkingEdit) WithKeepAll() *ClearThinkingEdit {
	e.Keep = &KeepAllThinking{Value: "all"}
	return e
}

// WithKeepRecentTurns preserves a specific number of recent thinking turns
func (e *ClearThinkingEdit) WithKeepRecentTurns(count int) *ClearThinkingEdit {
	e.Keep = &KeepRecentThinkingTurns{
		Type:  "thinking_turns",
		Value: count,
	}
	return e
}

// NewCompactEdit creates a basic compact edit with default settings
func NewCompactEdit() *CompactEdit {
	return &CompactEdit{
		Type: "compact_20260112",
	}
}

// WithTrigger sets when compaction should occur
func (e *CompactEdit) WithTrigger(tokens int) *CompactEdit {
	e.Trigger = &CompactTrigger{
		Type:  "input_tokens",
		Value: tokens,
	}
	return e
}

// WithPauseAfterCompaction enables pausing after compaction
func (e *CompactEdit) WithPauseAfterCompaction(pause bool) *CompactEdit {
	e.PauseAfterCompaction = &pause
	return e
}

// WithInstructions provides custom compaction instructions
func (e *CompactEdit) WithInstructions(instructions string) *CompactEdit {
	e.Instructions = &instructions
	return e
}
