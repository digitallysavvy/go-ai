package harness

// Consumer-facing aliases (TS `@ai-sdk/harness/agent` `HarnessAgent*` types,
// split from the `HarnessV1*` spec types in d77bed4). The spec types above are
// what adapters implement; these names are what HarnessAgent consumers use.
type (
	AgentAdapter             = Harness
	AgentAdapterSession      = Session
	AgentBuiltinTool         = BuiltinTool
	AgentBuiltinToolName     = BuiltinToolName
	AgentBuiltinToolUseKind  = BuiltinToolUseKind
	AgentStartOptions        = StartOptions
	AgentPrompt              = Prompt
	AgentPromptControl       = PromptControl
	AgentPromptTurnOptions   = PromptTurnOptions
	AgentContinueTurnOptions = ContinueTurnOptions
	AgentStreamPart          = StreamPart
	AgentToolSpec            = ToolSpec
	AgentLifecycleState      = LifecycleState
	AgentResumeSessionState  = ResumeSessionState
	AgentContinueTurnState   = ContinueTurnState
	AgentPendingToolApproval = PendingToolApproval
	AgentPendingToolResult   = PendingToolResult
	AgentSkill               = Skill
	AgentPermissionMode      = PermissionMode
)
