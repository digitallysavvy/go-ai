package anthropic

// Effort controls the reasoning effort level for supported models.
// Higher effort values produce more thorough reasoning at the cost of speed.
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortMax    Effort = "max"
)

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

// ThinkingConfig configures Claude's extended thinking capabilities
type ThinkingConfig struct {
	// Type specifies the thinking mode
	Type ThinkingType `json:"type"`

	// BudgetTokens specifies the maximum tokens for thinking (only for "enabled" type)
	// Requires a minimum of 1,024 tokens and counts towards the max_tokens limit.
	// Optional for "enabled" type, not used for "adaptive" type.
	BudgetTokens *int `json:"budget_tokens,omitempty"`
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
	ContextManagement *ContextManagement `json:"context_management,omitempty"`

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
	CacheControl *CacheControlOption `json:"cache_control_option,omitempty"`

	// Effort controls the model's reasoning effort level.
	// Supported values: EffortLow, EffortMedium, EffortHigh, EffortMax.
	// Requires the "effort-2025-11-24" beta header (injected automatically).
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       Effort: anthropic.EffortHigh,
	//   }
	Effort Effort `json:"effort,omitempty"`

	// ToolStreaming controls whether fine-grained tool streaming is enabled.
	// When nil or true (default), the "fine-grained-tool-streaming-2025-05-14"
	// beta header is added on streaming requests, enabling incremental tool call events.
	// Set to a false pointer to disable.
	//
	// Example (disable):
	//   disabled := false
	//   options := anthropic.ModelOptions{ToolStreaming: &disabled}
	ToolStreaming *bool `json:"tool_streaming,omitempty"`

	// DisableParallelToolUse prevents the model from calling multiple tools in a
	// single response. When true, adds {disable_parallel_tool_use: true} to the
	// tool_choice object sent to the API.
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       DisableParallelToolUse: true,
	//   }
	DisableParallelToolUse bool `json:"disable_parallel_tool_use,omitempty"`

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
	MCPServers []MCPServerConfig `json:"mcp_servers,omitempty"`

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
	AuthorizationToken string `json:"authorization_token,omitempty"`
	// ToolConfiguration optionally restricts which tools from this server are available
	ToolConfiguration *MCPToolConfiguration `json:"tool_configuration,omitempty"`
}

// MCPToolConfiguration controls which tools from an MCP server are exposed to the model.
type MCPToolConfiguration struct {
	// AllowedTools restricts which tool names are available from this server
	AllowedTools []string `json:"allowed_tools,omitempty"`
	// Enabled controls whether tools from this server are active
	Enabled *bool `json:"enabled,omitempty"`
}

// ContainerSkill configures a skill within an agent container.
type ContainerSkill struct {
	// Type is "anthropic" for built-in skills or "custom" for custom skills
	Type string `json:"type"`
	// SkillID is the identifier of the skill
	SkillID string `json:"skill_id"`
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
