package opencode

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

// Start operation discriminators.
const (
	OperationPrompt  = "prompt"
	OperationCompact = "compact"
)

// StartMessage is the OpenCode inbound `start` frame: the shared harness-v1
// base plus adapter-specific fields. Mirrors TS `startMessageSchema`
// (`opencode-bridge-protocol.ts`).
type StartMessage struct {
	bridge.StartBase

	Operation       string            `json:"operation,omitempty"`
	Provider        string            `json:"provider,omitempty"`
	Variant         string            `json:"variant,omitempty"`
	Instructions    string            `json:"instructions,omitempty"`
	SkillsChanged   bool              `json:"skillsChanged,omitempty"`
	ResumeSessionID string            `json:"resumeSessionId,omitempty"`
	OpenCodeConfig  map[string]any    `json:"openCodeConfig,omitempty"`
	MCPServers      map[string]any    `json:"mcpServers,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	// InactiveBuiltinToolNames lists the OpenCode built-in tools the bridge
	// must disable for this session (TS #23342). It replaces the shared
	// `builtinToolFiltering` field, which OpenCode's start frame omits: the
	// embedded StartBase.BuiltinToolFiltering is never set for OpenCode.
	// A nil pointer means no filtering; a pointer to an empty slice means
	// filtering is active but nothing is disabled (sent as `[]`, like TS).
	InactiveBuiltinToolNames *[]string `json:"inactiveBuiltinToolNames,omitempty"`

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

// builtinToolNames is the ordered key set of TS `OPENCODE_BUILTIN_TOOLS`
// (TS `openCodeBuiltinToolNameSchema`).
var builtinToolNames = []string{
	"askUserQuestions", "read", "write", "edit", "bash", "glob", "grep",
	"ls", "webfetch", "skill", "todowrite", "agent",
}
