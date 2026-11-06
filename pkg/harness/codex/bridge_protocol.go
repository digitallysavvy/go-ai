package codex

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

// StartFrame is the codex `start` frame. Mirrors TS
// `codex-bridge-protocol.ts` `startMessageSchema`.
type StartFrame struct {
	bridge.StartBase
	Instructions    string            `json:"instructions,omitempty"`
	ReasoningEffort string            `json:"reasoningEffort,omitempty"`
	WebSearch       *bool             `json:"webSearch,omitempty"`
	CodexConfig     map[string]any    `json:"codexConfig,omitempty"`
	MCPServers      map[string]any    `json:"mcpServers,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	ResumeThreadID  string            `json:"resumeThreadId,omitempty"`
	RestartThread   bool              `json:"restartThread,omitempty"`

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
