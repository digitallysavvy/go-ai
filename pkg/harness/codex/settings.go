// Package codex is the Go host-side port of TS `@ai-sdk/harness-codex`
// (`createCodex`), pinned to ai@7.0.113.
//
// It implements harness.Harness/Session/PromptControl by launching the
// embedded, unchanged TS bridge (pkg/harness/bridges, WG6) as `node
// bridge.mjs` inside the sandbox (pkg/harness/bridge, WG3) and translating the
// harness-v1 bridge wire protocol to harness.StreamPart Emit calls.
//
// Deferred versus TS (see the claudecode package doc for the shared
// rationale, and the Sep-23-2026 WG8 report):
//   - The "attach to a still-running bridge" / "replay a respawned bridge's
//     on-disk event log" rungs are not ported. Every DoStart spawns a fresh
//     bridge process; a resume/continue rehydrates the Codex thread via
//     `resumeThreadId` instead of reattaching to the live process.
//   - Native subscription credential discovery is not ported.
//   - The host-tool CLI relay's `composeToolUsageInstructions` prompt
//     framing IS ported (it is host-authored prompt text, not vendor code).
package codex

import (
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// Settings configures New. Mirrors TS `CodexHarnessSettings`.
type Settings struct {
	// Auth selects the authentication route: unset (auto-detect), "direct",
	// "ai-gateway", or an isolated environment via harness.AuthEnvironment.
	Auth harness.Authentication
	// CredentialForwarding customizes each credential value immediately
	// before it is forwarded into the sandbox.
	CredentialForwarding harness.CredentialForwarding
	// CodexConfig is additional configuration passed through to Codex
	// as-is (snake_case config.toml keys). Values managed by this adapter
	// take precedence over conflicting entries.
	CodexConfig map[string]any
	// MCPServers are additional MCP server definitions, keyed by name, in
	// Codex's native configuration format.
	MCPServers map[string]any
	// ReasoningEffort for reasoning-capable models: "low", "medium", "high",
	// "xhigh" or "max". Empty defers to the CLI's default.
	ReasoningEffort string
	// WebSearch, when true, allows the runtime to use live web search.
	WebSearch *bool
	// Port overrides the port the bridge binds inside the sandbox. Defaults
	// to the first port the sandbox exposes.
	Port int
	// PortEndpoint overrides the host endpoint used to reach the bridge.
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

// DefaultModel mirrors TS `DEFAULT_CODEX_MODEL`.
const DefaultModel = "gpt-5.5"
