// Package claudecode is the Go host-side port of TS `@ai-sdk/harness-claude-code`
// (`createClaudeCode`), pinned to ai@7.0.113.
//
// It implements harness.Harness/Session/PromptControl by launching the
// embedded, unchanged TS bridge (pkg/harness/bridges, WG6) as `node
// bridge.mjs` inside the sandbox (pkg/harness/bridge, WG3) and translating the
// harness-v1 bridge wire protocol to harness.StreamPart Emit calls.
//
// Deferred versus TS (see the Sep-23-2026 WG7 report for the full rationale):
//   - The "attach to a still-running bridge from persisted coordinates" and
//     "replay a respawned bridge's on-disk event log" rungs are not ported.
//     Every DoStart spawns a fresh bridge process; a resume/continue rehydrates
//     the underlying Claude conversation via `resumeSessionId`/`continue`
//     instead of reattaching to the live process. This is lossy across a
//     mid-turn suspend (in-flight work is recomputed) but never lossy of
//     already-persisted turns, matching TS's own documented "rerun" fallback
//     path — just taken unconditionally rather than only when attach fails.
//   - Native subscription credential discovery (OS keychain / Secret Service /
//     Credential Manager reading, OAuth refresh) is not ported. Only
//     environment-variable and AI Gateway authentication are supported.
//   - The experimental mid-turn `submitUserMessage` (steering) acknowledgement
//     channel is not wired.
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
