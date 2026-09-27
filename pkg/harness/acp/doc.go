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
// Process-loss recovery: when a live attach to a persisted bridge
// coordinate fails (or none was persisted), this port implements the same
// three tiers as TS's `ACPRespawnStrategy`/`restoreColdACPSession`
// (acp-v1-harness.ts): disk-replay (a continued turn whose event log ends
// in a terminal `finish`, replayed from disk on a respawned bridge),
// lossy-rerun (a continued turn resumed from persisted turn-start
// configuration and the native ACP session id, config-fingerprint
// validated), and cold-restore (a plain resume of a session with no
// in-flight turn, restoring the native ACP session by id via a
// prompt-less "start" carrying `recoveryMode: cold-restore`, resolved
// through the bridge's `acp-session-restored` raw frame). See harness.go
// (acpRespawnStrategy, DoStart's isResume block), turn_start_config.go
// (validateTurnStartConfig/validateColdSessionConfiguration), and
// session.go (restoreColdACPSession, DoContinueTurn's lossyRerun branch).
//
// See state/parity/sep_23_2026/harness.md WG11 and TS
// packages/harness-acp/src/{acp-harness,acp-auth,acp-tool-call}.ts and
// packages/harness-acp/src/v1/*.ts.
package acp
