package opencode

import "github.com/digitallysavvy/go-ai/pkg/harness/bridge"

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
}

// builtinToolNames is the ordered key set of TS `OPENCODE_BUILTIN_TOOLS`
// (TS `openCodeBuiltinToolNameSchema`).
var builtinToolNames = []string{
	"askUserQuestions", "read", "write", "edit", "bash", "glob", "grep",
	"ls", "webfetch", "skill", "todowrite", "agent",
}
