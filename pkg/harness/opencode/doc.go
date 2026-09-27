// Package opencode is the Go host-side port of the TypeScript
// `@ai-sdk/harness-opencode` adapter (pinned to ai@7.0.113): it wraps
// `@opencode-ai/sdk`'s `opencode serve` HTTP+SSE server, which runs inside
// the embedded in-sandbox Node bridge (pkg/harness/bridges.OpenCode). The Go
// host never runs the vendor SDK: it spawns the unchanged bridge, speaks the
// harness-v1 bridge wire protocol over a WebSocket (pkg/harness/bridge), and
// translates frames to/from pkg/harness's harness.Harness/Session/
// PromptControl surface.
//
// Native OpenCode subscription auth (subscription.go, ported from TS
// opencode-subscription.ts, including the GitLab AI Gateway direct-access
// case) is fully implemented: unlike the OS-keychain-backed native
// subscription readers other bridge adapters use, OpenCode's own store is a
// plain JSON file (`~/.local/share/opencode/auth.json`, or
// `OPENCODE_AUTH_CONTENT` verbatim for host processes that supply it
// directly) plus HTTP OAuth-refresh and, for GitLab, one extra HTTP call —
// no OS keychain or CLI is ever involved, so nothing here needed to be
// deferred.
//
// See state/parity/sep_23_2026/harness.md WG9 and TS
// packages/harness-opencode/src/{opencode-harness,opencode-auth,
// opencode-subscription}.ts.
package opencode
