// Package opencode is the Go host-side port of the TypeScript
// `@ai-sdk/harness-opencode` adapter (pinned to ai@7.0.113): it wraps
// `@opencode-ai/sdk`'s `opencode serve` HTTP+SSE server, which runs inside
// the embedded in-sandbox Node bridge (pkg/harness/bridges.OpenCode). The Go
// host never runs the vendor SDK: it spawns the unchanged bridge, speaks the
// harness-v1 bridge wire protocol over a WebSocket (pkg/harness/bridge), and
// translates frames to/from pkg/harness's harness.Harness/Session/
// PromptControl surface.
//
// Deferred (see the final implementation report): OpenCode's adapter-native
// subscription auth (`opencode-subscription.ts`, including the GitLab AI
// Gateway special case) reads OS keychains and mints its own short-lived
// access tokens; this port supports only environment-variable-based
// authentication (direct provider credentials and AI Gateway). Passing an
// explicit `auth` environment or a direct API key works exactly as in TS.
//
// See state/parity/sep_23_2026/harness.md WG9 and TS
// packages/harness-opencode/src/opencode-harness.ts.
package opencode
