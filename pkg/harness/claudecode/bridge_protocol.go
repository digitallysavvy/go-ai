package claudecode

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

// ThinkingConfig controls extended-thinking behavior. Mirrors TS
// `ClaudeCodeThinkingConfig`.
type ThinkingConfig struct {
	// Type is "adaptive" (default), "enabled" or "disabled".
	Type string `json:"type"`
	// Display is "summarized" (default) or "omitted"; ignored when Type is
	// "disabled".
	Display string `json:"display,omitempty"`
}

// MarshalJSON emits only the fields valid for Type, matching the TS
// discriminated union.
func (c ThinkingConfig) MarshalJSON() ([]byte, error) {
	if c.Type == "disabled" {
		return []byte(`{"type":"disabled"}`), nil
	}
	typ := c.Type
	if typ == "" {
		typ = "adaptive"
	}
	if c.Display == "" {
		return json.Marshal(map[string]any{"type": typ})
	}
	return json.Marshal(map[string]any{"type": typ, "display": c.Display})
}

// StartFrame is the claude-code `start` frame. Mirrors TS
// `claude-code-bridge-protocol.ts` `startMessageSchema`.
type StartFrame struct {
	bridge.StartBase
	Instructions           string            `json:"instructions,omitempty"`
	Thinking               *ThinkingConfig   `json:"thinking,omitempty"`
	Effort                 string            `json:"effort,omitempty"`
	MaxTurns               *int              `json:"maxTurns,omitempty"`
	AgentProgressSummaries bool              `json:"agentProgressSummaries,omitempty"`
	ForwardSubagentText    bool              `json:"forwardSubagentText,omitempty"`
	Env                    map[string]string `json:"env,omitempty"`
	Skills                 []string          `json:"skills,omitempty"`
	MCPServers             map[string]any    `json:"mcpServers,omitempty"`
	Continue               bool              `json:"continue,omitempty"`
	ResumeSessionID        string            `json:"resumeSessionId,omitempty"`

	// PromptContent carries text plus inline images. When nil the prompt is
	// the plain string StartBase.Prompt. Mirrors TS `harnessPromptContentSchema`.
	PromptContent *harnessutil.PromptContent `json:"-"`
}

// MarshalJSON encodes PromptContent (when set) as the `prompt` field, in place
// of the plain-string StartBase.Prompt.
func (f StartFrame) MarshalJSON() ([]byte, error) {
	type plain StartFrame
	if f.PromptContent == nil {
		return json.Marshal(plain(f))
	}
	return json.Marshal(struct {
		plain
		Prompt harnessutil.PromptContent `json:"prompt"`
	}{plain(f), *f.PromptContent})
}
