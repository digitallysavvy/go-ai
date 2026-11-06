package deepagents

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

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

// MarshalJSON emits only the fields valid for Type, like the TS discriminated
// union: "enabled" always carries budget_tokens (required by the bridge schema,
// even when 0), "disabled" carries nothing else.
func (c ThinkingConfig) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": c.Type}
	switch c.Type {
	case ThinkingTypeDisabled:
	case ThinkingTypeEnabled:
		m["budget_tokens"] = c.BudgetTokens
		if c.Display != "" {
			m["display"] = c.Display
		}
	default:
		if c.Display != "" {
			m["display"] = c.Display
		}
	}
	return json.Marshal(m)
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

	// PromptContent carries text plus inline images. When nil the prompt is
	// the plain string StartBase.Prompt. Mirrors TS `harnessPromptContentSchema`.
	PromptContent *harnessutil.PromptContent `json:"-"`
}

// MarshalJSON encodes PromptContent (when set) as the `prompt` field, in place
// of the plain-string StartBase.Prompt.
func (f StartMessage) MarshalJSON() ([]byte, error) {
	type plain StartMessage
	if f.PromptContent == nil {
		return json.Marshal(plain(f))
	}
	return json.Marshal(struct {
		plain
		Prompt harnessutil.PromptContent `json:"prompt"`
	}{plain(f), *f.PromptContent})
}
