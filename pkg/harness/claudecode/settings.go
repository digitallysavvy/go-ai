// Package claudecode is the Go host-side port of TS `@ai-sdk/harness-claude-code`
// (`createClaudeCode`), pinned to ai@7.0.113.
//
// It implements harness.Harness/Session/PromptControl by launching the
// embedded, unchanged TS bridge (pkg/harness/bridges, WG6) as `node
// bridge.mjs` inside the sandbox (pkg/harness/bridge, WG3) and translating the
// harness-v1 bridge wire protocol to harness.StreamPart Emit calls.
//
// DoStart implements all three TS resume rungs: (1) ATTACH reopens a socket
// to a still-running bridge using persisted coordinates (no respawn, no
// fresh token); (2) REPLAY respawns the bridge with `BRIDGE_REPLAY_FROM_DISK`
// when a continued (suspended) turn's on-disk event log ends in a finished
// turn; (3) RERUN respawns and rehydrates the Claude conversation via
// `resumeSessionId`/`continue` otherwise. Mid-turn steering
// (`submitUserMessage`) is wired whenever the bridge advertises
// `experimental_userMessageResponses` on its hello.
//
// Deferred versus TS (see the Sep-23-2026 WG7 report for the full rationale):
//   - Native subscription credential discovery reads the same
//     `~/.claude/.credentials.json` file TS reads (subscription.go), including
//     OAuth refresh and write-back. The macOS Keychain fallback TS also has is
//     NOT ported: this SDK's sandbox providers (pkg/harness/sandbox/local, the
//     Vercel provider) run Linux containers, where `/usr/bin/security` does
//     not exist, and TS itself only takes that fallback when
//     `CLAUDE_CONFIG_DIR` is unset/default, so it is a secondary path even in
//     TS. Linux's Secret Service / Windows Credential Manager equivalents are
//     for the *host* Claude CLI config, not this adapter's discovery, so they
//     are not applicable here either.
package claudecode

import (
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// Settings configures New. Mirrors TS `ClaudeCodeHarnessSettings`.
type Settings struct {
	// Auth selects the authentication route: unset (auto-detect), "direct",
	// "ai-gateway", or an isolated environment via harness.AuthEnvironment.
	Auth harness.Authentication
	// CredentialForwarding customizes each credential value immediately
	// before it is forwarded into the sandbox.
	CredentialForwarding harness.CredentialForwarding
	// MCPServers are additional MCP server definitions, keyed by name, in the
	// Claude Agent SDK's native configuration format. The name "harness-tools"
	// is reserved.
	MCPServers map[string]any
	// MaxTurns caps how many internal turns the CLI can take before yielding
	// back to the caller. Zero means the CLI's default.
	MaxTurns int
	// AgentProgressSummaries enables periodic AI-generated progress
	// summaries for running subagents. The summaries are forwarded in raw
	// `task_progress` stream parts.
	AgentProgressSummaries bool
	// ForwardSubagentText forwards subagent text and thinking messages in
	// addition to tool activity. Subagent messages are exposed as raw
	// stream parts.
	ForwardSubagentText bool
	// Env are additional environment variables for the Claude Code process,
	// merged over the resolved authentication environment.
	Env map[string]string
	// Thinking controls extended-thinking behavior. Defaults to
	// {Type: "adaptive", Display: "summarized"}.
	Thinking *ThinkingConfig
	// Effort controls adaptive-thinking effort: "low", "medium", "high",
	// "xhigh" or "max". Empty uses the Claude Agent SDK default.
	Effort string
	// Port overrides the port the bridge binds inside the sandbox. Defaults
	// to the first port the sandbox exposes.
	Port int
	// PortEndpoint overrides the host endpoint used to reach the bridge.
	// Required together with Port for a sandbox that is not a
	// harness.NetworkSandboxSession.
	PortEndpoint *harness.PortEndpoint
	// StartupTimeout bounds how long to wait for the bridge to announce its
	// port. Defaults to 120s.
	StartupTimeout time.Duration
	// Reconnect tunes reconnection after an established bridge connection
	// drops.
	Reconnect bridge.ReconnectOptions
	// MintBridgeToken creates the bridge channel token. Defaults to a random
	// 32-byte hex token. Requires the sandbox to expose an ID.
	MintBridgeToken harness.MintBridgeTokenCallback
}
