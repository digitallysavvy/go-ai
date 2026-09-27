package deepagents

import "github.com/digitallysavvy/go-ai/pkg/harness/bridge"

// ThinkingConfig controls Anthropic extended thinking for the Deep Agents
// model. Mirrors TS `DeepAgentsThinkingConfig`. Exactly one of the three
// shapes applies, selected by Type.
type ThinkingConfig struct {
	// Type is "adaptive", "enabled" or "disabled".
	Type string `json:"type"`
	// Display applies to "adaptive" and "enabled": "summarized" or "omitted".
	Display string `json:"display,omitempty"`
	// BudgetTokens applies to "enabled" only.
	BudgetTokens int `json:"budget_tokens,omitempty"`
}

// Thinking type discriminators.
const (
	ThinkingTypeAdaptive = "adaptive"
	ThinkingTypeEnabled  = "enabled"
	ThinkingTypeDisabled = "disabled"
)

// StartMessage is the deepagents inbound `start` frame: the shared
// harness-v1 base plus adapter-specific fields. Mirrors TS
// `startMessageSchema` (`deepagents-bridge-protocol.ts`).
type StartMessage struct {
	bridge.StartBase

	MCPServers map[string]any `json:"mcpServers,omitempty"`
	// Instructions is appended to Deep Agents's native system prompt.
	Instructions string          `json:"instructions,omitempty"`
	Thinking     *ThinkingConfig `json:"thinking,omitempty"`
	Effort       string          `json:"effort,omitempty"`
	// SkillsPaths are the in-backend skills source dirs ($HOME and
	// <workDir>), passed to createDeepAgent({ skills }).
	SkillsPaths    []string          `json:"skillsPaths,omitempty"`
	SkillsChanged  bool              `json:"skillsChanged,omitempty"`
	RecursionLimit *int              `json:"recursionLimit,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
}
