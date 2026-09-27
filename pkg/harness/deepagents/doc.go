// Package deepagents is the Go host-side port of the TypeScript
// `@ai-sdk/harness-deepagents` adapter (pinned to ai@7.0.113): it wraps the
// `deepagents` LangGraph JS agent, which runs inside the embedded in-sandbox
// Node bridge (pkg/harness/bridges.DeepAgents). The Go host never runs the
// vendor SDK: it spawns the unchanged bridge, speaks the harness-v1 bridge
// wire protocol over a WebSocket (pkg/harness/bridge), and translates frames
// to/from pkg/harness's harness.Harness/Session/PromptControl surface.
//
// See state/parity/sep_23_2026/harness.md WG10 and TS
// packages/harness-deepagents/src/deepagents-harness.ts.
package deepagents
