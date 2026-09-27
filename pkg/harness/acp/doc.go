// Package acp is the Go host-side port of the TypeScript
// `@ai-sdk/harness-acp` v1 meta adapter (pinned to ai@7.0.113): it wraps any
// implementation of the Agent Client Protocol (agentclientprotocol.com), a
// documented, versioned JSON-RPC 2.0 protocol. CreateACP is generic over the
// concrete ACP-speaking CLI (Cursor, fx, GitHub Copilot, Grok Build, ...);
// those concrete thin-config wrappers are WG12, out of this package's scope.
//
// The vendor ACP client SDK never runs on the host: it runs inside the
// embedded in-sandbox Node bridge (pkg/harness/bridges.ACP), which drives the
// configured ACP implementation binary over stdio. The Go host spawns the
// unchanged bridge, speaks the harness-v1 (+ ACP extension) bridge wire
// protocol over a WebSocket (pkg/harness/bridge), and translates frames
// to/from pkg/harness's harness.Harness/Session/PromptControl surface.
//
// Deferred (see the final implementation report for exact TS source
// pointers):
//   - Process-loss recovery beyond a live reconnect: TS distinguishes
//     disk-replay / lossy-rerun / cold-restore recovery rungs when the
//     bridge process itself is gone. This port always attempts a live
//     attach (matching TS's own first attempt) and, on failure, starts a
//     fresh turn; it does not replay from an on-disk event log or restore a
//     cold ACP session id.
//   - `ACPAskUserQuestionsSettings.isNativeToolCall`'s exact interaction
//     with `isMcpToolCall` is simplified: a candidate is suppressed only
//     when `isMcpToolCall` returns true.
//
// See state/parity/sep_23_2026/harness.md WG11 and TS
// packages/harness-acp/src/{acp-harness,acp-auth,acp-tool-call}.ts and
// packages/harness-acp/src/v1/*.ts.
package acp
