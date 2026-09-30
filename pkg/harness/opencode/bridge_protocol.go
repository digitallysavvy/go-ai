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
}
