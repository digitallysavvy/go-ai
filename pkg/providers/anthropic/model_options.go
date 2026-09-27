package anthropic

// StructuredOutputMode controls how JSON structured output is generated.
// Different Anthropic models support different strategies for producing
// structured JSON output, and this option lets you explicitly choose
// which strategy to use.
type StructuredOutputMode string

const (
	// StructuredOutputAuto uses outputFormat for models that support it
	// (claude-*-4-6, claude-*-4-5, claude-opus-4-1), and falls back to
	// jsonTool for older models. This is the default.
	StructuredOutputAuto StructuredOutputMode = "auto"

	// StructuredOutputFormat always uses output_config.format.
	// Only use this when the model is known to support native structured output.
	StructuredOutputFormat StructuredOutputMode = "outputFormat"

	// StructuredOutputJSONTool always creates a synthetic 'json' tool.
	// Works on all Claude models regardless of native structured output support.
	StructuredOutputJSONTool StructuredOutputMode = "jsonTool"
)

// Effort controls the reasoning effort level for supported models.
// Higher effort values produce more thorough reasoning at the cost of speed.
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
)

// TaskBudget configures Anthropic's advisory task-level token budget.
// It is serialized as output_config.task_budget.
type TaskBudget struct {
	Type      string `json:"type"`
	Total     int    `json:"total"`
	Remaining *int   `json:"remaining,omitempty"`
}

// CacheControlOption configures explicit ephemeral prompt caching for a request.
// Use this to mark the request for Anthropic's ephemeral caching (distinct from
// AutomaticCaching which uses {type: "auto"}).
type CacheControlOption struct {
	// Type must be "ephemeral"
	Type string `json:"type"`
	// TTL is the cache time-to-live: "5m" (default) or "1h" (Claude 4.5+ only)
	TTL string `json:"ttl,omitempty"`
}

// ThinkingType represents the type of thinking configuration
type ThinkingType string

const (
	// ThinkingTypeAdaptive enables adaptive thinking (Opus 4.6+)
	// Claude dynamically adjusts reasoning effort based on the task complexity.
	ThinkingTypeAdaptive ThinkingType = "adaptive"

	// ThinkingTypeEnabled enables extended thinking with optional budget (pre-Opus 4.6)
	// Responses include thinking content blocks showing Claude's reasoning process.
	ThinkingTypeEnabled ThinkingType = "enabled"

	// ThinkingTypeDisabled disables thinking
	ThinkingTypeDisabled ThinkingType = "disabled"
)

// Speed represents the inference speed mode
type Speed string

const (
	// SpeedFast enables fast mode for 2.5x faster output token speeds
	// Only supported with claude-opus-4-6
	SpeedFast Speed = "fast"

	// SpeedStandard uses standard inference speed (default)
	SpeedStandard Speed = "standard"
)

// FallbackConfig configures one Anthropic server-side fallback attempt.
type FallbackConfig struct {
	Model        string                 `json:"model"`
	MaxTokens    *int                   `json:"max_tokens,omitempty"`
	Thinking     map[string]interface{} `json:"thinking,omitempty"`
	OutputConfig map[string]interface{} `json:"output_config,omitempty"`
	Speed        Speed                  `json:"speed,omitempty"`
}

// ThinkingConfig configures Claude's extended thinking capabilities
type ThinkingConfig struct {
	// Type specifies the thinking mode
	Type ThinkingType `json:"type"`

	// BudgetTokens specifies the maximum tokens for thinking (only for "enabled" type)
	// Requires a minimum of 1,024 tokens and counts towards the max_tokens limit.
	// Optional for "enabled" type, not used for "adaptive" type.
	BudgetTokens *int `json:"budgetTokens,omitempty"`

	// Display controls how thinking is returned for "adaptive" thinking:
	// ThinkingDisplayOmitted, ThinkingDisplaySummarized or
	// ThinkingDisplayUpdates (adds the thinking-display-updates beta).
	// Ignored for other thinking types.
	Display ThinkingDisplay `json:"display,omitempty"`

	// BlockBinding configures preserved-thinking block binding
	// (thinking.block_binding). It may be set with Type "adaptive" or alone
	// (empty Type) for binding-only recovery requests. Adds the
	// thinking-binding-controls beta.
	BlockBinding *ThinkingBlockBinding `json:"blockBinding,omitempty"`
}

// ThinkingDisplay controls how adaptive thinking content is returned.
type ThinkingDisplay string

const (
	ThinkingDisplayOmitted    ThinkingDisplay = "omitted"
	ThinkingDisplaySummarized ThinkingDisplay = "summarized"
	ThinkingDisplayUpdates    ThinkingDisplay = "updates"
)

// ThinkingBlockBinding configures how the API treats replayed thinking blocks
// whose prefix does not match. Serialized as
// thinking.block_binding.prefix_mismatch_behavior.
type ThinkingBlockBinding struct {
	// PrefixMismatchBehavior is "error" or "drop_block".
	PrefixMismatchBehavior string `json:"prefixMismatchBehavior"`
}

// Safeguard configures an Anthropic safeguard classifier. Serialized as
// safeguards[{type, classifier_context?}] and adds the
// dangerous-tool-use-2026-09-03 beta.
type Safeguard struct {
	// Type is "dangerous_tool_use".
	Type string `json:"type"`

	// ClassifierContext is optional context passed to the classifier.
	ClassifierContext map[string]interface{} `json:"classifierContext,omitempty"`
}

// CompactionOption requests an on-demand summary of the supplied conversation.
// Serialized as the request-level "compaction" field and adds the
// "compact-2026-09-04" beta header automatically. Mutually exclusive with
// ContextManagement: setting both returns an error from DoGenerate/DoStream.
type CompactionOption struct {
	// Type must be "summarize".
	Type string `json:"type"`

	// Instructions optionally steers what the on-demand summary should preserve.
	Instructions string `json:"instructions,omitempty"`
}

// ModelOptions contains optional configuration for Anthropic language models.
// These options can be passed when creating a model instance to configure
// provider-specific features.
type ModelOptions struct {
	// ContextManagement enables automatic cleanup of conversation history
	// to prevent context window overflow in long conversations.
	//
	// This is a beta feature that requires Claude 4.5+ models.
	// When enabled, Anthropic will automatically remove or truncate old
	// content based on the configured strategies.
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       ContextManagement: &anthropic.ContextManagement{
	//           Strategies: []string{anthropic.StrategyClearToolUses},
	//       },
	//   }
	//
	// See ContextManagement for available strategies.
	ContextManagement *ContextManagement `json:"contextManagement,omitempty"`

	// Compaction requests an on-demand summary of the supplied conversation.
	// Mutually exclusive with ContextManagement (setting both returns an
	// error). Adds the "compact-2026-09-04" beta header automatically.
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       Compaction: &anthropic.CompactionOption{Type: "summarize"},
	//   }
	Compaction *CompactionOption `json:"compaction,omitempty"`

	// Thinking configures Claude's extended thinking capabilities.
	//
	// When enabled, responses include thinking content blocks showing
	// Claude's reasoning process before the final answer.
	//
	// For Opus 4.6 and newer models, use ThinkingTypeAdaptive:
	//   options := anthropic.ModelOptions{
	//       Thinking: &anthropic.ThinkingConfig{
	//           Type: anthropic.ThinkingTypeAdaptive,
	//       },
	//   }
	//
	// For models before Opus 4.6, use ThinkingTypeEnabled with optional budget:
	//   budget := 5000
	//   options := anthropic.ModelOptions{
	//       Thinking: &anthropic.ThinkingConfig{
	//           Type: anthropic.ThinkingTypeEnabled,
	//           BudgetTokens: &budget,
	//       },
	//   }
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// Speed configures the inference speed mode.
	//
	// Fast mode provides 2.5x faster output token speeds but is only
	// supported with claude-opus-4-6.
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       Speed: anthropic.SpeedFast,
	//   }
	Speed Speed `json:"speed,omitempty"`

	// AutomaticCaching enables Anthropic's automatic prompt caching feature.
	//
	// When enabled, the API automatically identifies and caches reusable prompt
	// segments without requiring explicit cache_control markers in individual
	// messages. This requires the "prompt-caching-2024-07-31" beta header.
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       AutomaticCaching: true,
	//   }
	//
	// See https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching for details.
	AutomaticCaching bool `json:"automatic_caching,omitempty"`

	// CacheControl configures explicit ephemeral prompt caching.
	// Mutually exclusive with AutomaticCaching; CacheControl takes precedence if both are set.
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       CacheControl: &anthropic.CacheControlOption{Type: "ephemeral", TTL: "5m"},
	//   }
	CacheControl *CacheControlOption `json:"cacheControl,omitempty"`

	// Effort controls the model's reasoning effort level.
	// Supported values: EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax.
	// Sent as output_config.effort (no beta header is required).
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       Effort: anthropic.EffortHigh,
	//   }
	Effort Effort `json:"effort,omitempty"`

	// TaskBudget informs the model of the total token budget available for the
	// current task. This is advisory only; it does not enforce a hard limit.
	// Requires the "task-budgets-2026-03-13" beta header (injected automatically).
	TaskBudget *TaskBudget `json:"taskBudget,omitempty"`

	// InferenceGeo controls where Anthropic inference may run for this request.
	// Supported values match the TypeScript SDK: "us" or "global".
	InferenceGeo string `json:"inferenceGeo,omitempty"`

	// Fallbacks configures Anthropic server-side fallback attempts.
	Fallbacks []FallbackConfig `json:"fallbacks,omitempty"`

	// FallbacksDefault sends fallbacks: "default" to use Anthropic's default
	// server-side fallback chain (beta server-side-fallback-2026-07-01). It
	// takes precedence over Fallbacks.
	FallbacksDefault bool `json:"fallbacksDefault,omitempty"`

	// ServiceTier selects the Anthropic service tier: "auto" or
	// "standard_only". Serialized as service_tier.
	ServiceTier string `json:"serviceTier,omitempty"`

	// AnthropicBeta lists additional anthropic-beta flags to send.
	AnthropicBeta []string `json:"anthropicBeta,omitempty"`

	// Safeguards configures safeguard classifiers (for example
	// dangerous_tool_use). Classifier verdicts are returned in
	// providerMetadata.anthropic.safeguardResults.
	Safeguards []Safeguard `json:"safeguards,omitempty"`

	// ToolStreaming controls whether fine-grained tool streaming is enabled.
	// When nil or true, streaming requests set eager_input_streaming: true on
	// function tools that do not set ToolOptions.EagerInputStreaming. Set it
	// to false to disable that default.
	//
	// Example (disable):
	//   disabled := false
	//   options := anthropic.ModelOptions{ToolStreaming: &disabled}
	ToolStreaming *bool `json:"toolStreaming,omitempty"`

	// DisableParallelToolUse prevents the model from calling multiple tools in a
	// single response. When true, adds {disable_parallel_tool_use: true} to the
	// tool_choice object sent to the API. An explicit false is ignored (with a
	// warning) when the JSON response tool is used for structured output.
	//
	// Example:
	//   disable := true
	//   options := anthropic.ModelOptions{
	//       DisableParallelToolUse: &disable,
	//   }
	DisableParallelToolUse *bool `json:"disableParallelToolUse,omitempty"`

	// MCPServers configures remote MCP servers for native server-side tool invocation.
	// The Anthropic API connects to these MCP servers directly, exposing their tools
	// to the model without the caller having to proxy individual tool calls.
	// Requires the "mcp-client-2025-04-04" beta header (injected automatically).
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       MCPServers: []anthropic.MCPServerConfig{
	//           {Type: "url", Name: "my-server", URL: "https://mcp.example.com/sse"},
	//       },
	//   }
	MCPServers []MCPServerConfig `json:"mcpServers,omitempty"`

	// Container configures an Anthropic agent container for code execution and skills.
	// When Skills are provided, the code-execution-2025-08-25, skills-2025-10-02, and
	// files-api-2025-04-14 beta headers are automatically injected.
	//
	// Example (full config with skills):
	//   options := anthropic.ModelOptions{
	//       Container: &anthropic.ContainerConfig{
	//           Skills: []anthropic.ContainerSkill{
	//               {Type: "anthropic", SkillID: "web_search"},
	//           },
	//       },
	//   }
	Container *ContainerConfig `json:"container,omitempty"`

	// ContainerID is a shorthand for specifying a plain container ID string.
	// When set, the container body field is sent as a plain string value.
	// Mutually exclusive with Container; ContainerID takes precedence if both are set.
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       ContainerID: "container-abc123",
	//   }
	ContainerID string `json:"container_id,omitempty"`

	// StructuredOutputMode controls how ResponseFormat is sent to the API.
	// Default (empty/"auto"): uses output_config.format for models that support it
	// (claude-*-4-6, claude-*-4-5), falls back to a synthetic json tool for
	// models that don't (e.g. claude-sonnet-4-20250514, claude-3-7-sonnet).
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       StructuredOutputMode: anthropic.StructuredOutputJSONTool,
	//   }
	StructuredOutputMode StructuredOutputMode `json:"structuredOutputMode,omitempty"`

	// SendReasoning controls whether ReasoningContent (thinking) blocks from message
	// history are included when sending messages to the Anthropic API.
	//
	// Default (nil or true): reasoning blocks with a valid Signature are included
	// in outgoing requests — required when the receiving model supports extended
	// thinking and the blocks were originally produced by that model.
	//
	// Set to false when routing a conversation to a model that does not support
	// thinking input (e.g. an older Claude model or one without thinking enabled).
	// This strips all ReasoningContent parts from outgoing message history before
	// the API call, preventing errors caused by unexpected thinking content.
	//
	// Example (disable when switching to non-thinking model):
	//   disabled := false
	//   options := anthropic.ModelOptions{SendReasoning: &disabled}
	SendReasoning *bool `json:"sendReasoning,omitempty"`

	// Metadata to include with the request (TS anthropicLanguageModelOptions.metadata).
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       Metadata: &anthropic.Metadata{UserID: "user-123"},
	//   }
	Metadata *Metadata `json:"metadata,omitempty"`
}

// Metadata carries request metadata. Currently only UserID (an external
// identifier for the user associated with the request) is supported,
// matching TS anthropicLanguageModelOptions.metadata.
type Metadata struct {
	// UserID is an external identifier for the user associated with the
	// request. Should be a UUID, hash value, or other opaque identifier.
	// Must not contain PII (name, email, phone number, etc.).
	UserID string `json:"userId,omitempty"`
}

// MCPServerConfig configures a remote MCP server for the Anthropic API to connect to.
// The API handles discovery and invocation of tools from this server server-side.
type MCPServerConfig struct {
	// Type must be "url"
	Type string `json:"type"`
	// Name is a unique identifier for this server within the request
	Name string `json:"name"`
	// URL is the HTTP(S) endpoint of the MCP server
	URL string `json:"url"`
	// AuthorizationToken is an optional bearer token for authentication
	AuthorizationToken string `json:"authorizationToken,omitempty"`
	// ToolConfiguration optionally restricts which tools from this server are available
	ToolConfiguration *MCPToolConfiguration `json:"toolConfiguration,omitempty"`
}

// MCPToolConfiguration controls which tools from an MCP server are exposed to the model.
type MCPToolConfiguration struct {
	// AllowedTools restricts which tool names are available from this server
	AllowedTools []string `json:"allowedTools,omitempty"`
	// Enabled controls whether tools from this server are active
	Enabled *bool `json:"enabled,omitempty"`
}

// ContainerSkill configures a skill within an agent container.
type ContainerSkill struct {
	// Type is "anthropic" for built-in skills or "custom" for custom skills
	Type string `json:"type"`
	// SkillID is the identifier of the skill
	SkillID string `json:"skillId"`
	// Version is the optional skill version
	Version string `json:"version,omitempty"`
}

// ContainerConfig configures the agent container for a request.
// Use ContainerID (string shorthand) for a plain container ID, or ContainerConfig for
// full configuration including skills.
type ContainerConfig struct {
	// ID is an optional existing container ID to reuse
	ID string `json:"id,omitempty"`
	// Skills are the capability bundles to load into the container
	Skills []ContainerSkill `json:"skills,omitempty"`
}
