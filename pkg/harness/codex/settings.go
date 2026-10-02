// Package codex is the Go host-side port of TS `@ai-sdk/harness-codex`
// (`createCodex`), pinned to ai@7.0.127.
//
// It implements harness.Harness/Session/PromptControl by launching the
// embedded TS bridge (pkg/harness/bridges, WG6) as `node bridge.mjs` inside
// the sandbox (pkg/harness/bridge, WG3) and translating the harness-v1 bridge
// wire protocol to harness.StreamPart Emit calls.
//
// As of TS commit d17ead78cd (ai@7.0.118), the bridge itself no longer runs
// `codex exec`/the Codex SDK: it drives Codex's `app-server` (JSON-RPC over
// stdio) directly, registers the caller's tools as app-server "dynamic
// tools", and receives granular `item/started`/`item/completed`/
// `rawResponseItem/completed` notifications instead of a coarser SDK event
// stream. That protocol lives entirely inside the embedded bridge process
// (pkg/harness/bridges/codex/bridge.mjs) — this package never speaks
// JSON-RPC to Codex's app-server itself, only the unchanged harness-v1
// bridge wire protocol (`start`/`tool-result`/`text-delta`/...) to the
// bridge, so no host-side JSON-RPC client is needed. The host-visible
// changes from this migration are: (1) tools are now forwarded to the bridge
// via the `start` frame's `tools` field and registered as real app-server
// tools (no more prompt-text tool instructions — the CLI-relay shim
// (`harness-tool.mjs`) and its `composeToolUsageInstructions` prompt framing
// are gone, both TS-side and here); (2) `apply_patch`/`view_image` are now
// real model-callable built-in tools (builtin_tools.go); (3)
// `supportsBuiltinToolFiltering` is now true — app-server can filter
// bash/webSearch/view_image via config and apply_patch via a trusted hook,
// both bridge-internal, so DoStart forwards the caller's
// HarnessV1BuiltinToolFiltering on the `start` frame instead of rejecting
// it; (4) mid-turn steering (`submitUserMessage`) is now wired via
// `turn/steer`, unconditionally like TS's own `wireTurn` (unlike
// claude-code/opencode, this is not gated on a bridge-advertised hello
// capability).
//
// DoStart implements all three TS resume rungs, exactly as the claudecode
// package does: ATTACH reopens a socket to a still-running bridge using
// persisted coordinates; REPLAY respawns with `BRIDGE_REPLAY_FROM_DISK` when
// a continued (suspended) turn's on-disk event log ends in a finished turn;
// RERUN respawns and rehydrates the Codex thread via `resumeThreadId`
// otherwise.
//
// TS commit c0e991c ("fix Codex sometimes stopping after a single text
// response when using workflow-harness") is entirely a codex-harness.ts (this
// package's) fix, not a packages/workflow-harness/** one, despite the audit
// row filing it under a later work group: it defers sending the `start` frame
// by one JS macrotask so `doPromptTurn`'s own caller has a chance to finish
// wiring the returned PromptControl before an ultra-fast, tool-less Codex
// turn can settle. That race is specific to TS's synchronous
// return-then-caller-wires-async-generator shape. It does not exist here:
// wireTurn (this package's session.go) subscribes every bridge-event listener
// BEFORE the frame is sent, and the caller's `Emit` function is passed in as
// an already-functional parameter (not wired after the call returns — see
// pkg/harness/run_prompt.go's `runPrompt`, which builds `emit` and passes it
// into DoPromptTurn/DoContinueTurn), so there is no "caller still wiring"
// window for even a zero-latency bridge response to race. No Go port is
// needed; this is a concrete architectural reason; not a scope omission.
//
// Deferred versus TS (see the claudecode package doc for the shared
// rationale, and the Sep-23-2026 WG8 report):
//   - Native subscription credential discovery reads the same
//     `~/.codex/auth.json` file TS reads (subscription.go: `auth_mode ==
//     "chatgpt"`, `tokens.access_token`/`refresh_token`, OAuth refresh,
//     write-back). NOT ported: the OS-keyring storage mode (macOS Keychain /
//     Linux Secret Service / Windows Credential Manager, gated by a
//     `cli_auth_credentials_store` config.toml value TS also reads) — this
//     SDK's sandbox providers run Linux containers with no `secret-tool`
//     daemon in a headless sandbox, and TS defaults to the file store unless
//     that key is explicitly set. Also NOT ported: the `ChatGPT-Account-ID`
//     request header TS derives from the credential's `account_id` — it
//     requires extending the request-transformation machinery beyond a
//     single credential-header swap, and no MUST-CHECK row calls for it;
//     forwarding just `CODEX_API_KEY`/`OPENAI_BASE_URL` is enough for the
//     ChatGPT backend to authenticate the request.
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
