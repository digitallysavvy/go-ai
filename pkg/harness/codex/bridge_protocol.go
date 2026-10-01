package codex

import "github.com/digitallysavvy/go-ai/pkg/harness/bridge"

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
}
