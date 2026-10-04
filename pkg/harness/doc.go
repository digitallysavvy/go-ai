// Package harness runs coding agents such as Claude Code and Codex from Go.
// A harness adapter wraps one of these agent runtimes. The adapter starts the
// runtime inside a sandbox, and Agent turns it into an agent you drive with
// the same Generate and Stream calls as agent.ToolLoopAgent.
//
// Three pieces fit together:
//
//   - A Harness adapter, for example claudecode.New or codex.New from the
//     subpackages of this package. It knows how to start one runtime.
//   - A sandbox, for example sandbox/local (runs on the host in a per-session
//     directory) or sandbox/vercel (runs in a Vercel sandbox).
//   - An Agent, created with NewAgent, which merges the runtime's built-in
//     tools with your own tools and manages sessions.
//
// A minimal setup with Claude Code and the local sandbox:
//
//	cc, err := claudecode.New(claudecode.Settings{MaxTurns: 30})
//	if err != nil {
//		log.Fatal(err)
//	}
//	coder, err := harness.NewAgent(harness.AgentSettings{
//		Harness:        cc,
//		Model:          os.Getenv("CLAUDE_CODE_MODEL"),
//		Sandbox:        local.NewProvider(local.Options{RootDir: "sandboxes", Ports: []int{4319}}),
//		PermissionMode: harness.PermissionModeAllowAll,
//		Instructions:   "Fix the failing test. Keep changes minimal.",
//	})
//
// Stream a turn with coder.Stream, as in the Shipyard demo at
// https://github.com/digitallysavvy/go-ai-shipyard. The demo streams the
// activity of the coding agent into a chat UI as data parts.
//
// This package also holds the harness-v1 specification types that adapter
// authors implement (Harness, Session, PromptControl), the stream-part
// vocabulary, the sandbox abstraction, bootstrap recipes and the harness error
// types. Subpackages provide the adapters (claudecode, codex, cursor, opencode,
// githubcopilot, grokbuild, deepagents, acp) and bridges.
//
// Reference: https://goaisdk.com/docs/reference/ai/harness-sandbox-vercel.
//
// Provenance: this package is the Go port of @ai-sdk/harness from the Vercel AI
// SDK for TypeScript (pinned to ai@7.0.113). All JSON shapes are
// wire-compatible with the TypeScript schemas, so a Go host can talk to the
// unchanged TypeScript in-sandbox bridge and resume sessions created by a
// TypeScript host, and the reverse. TypeScript prefixes spec types with
// HarnessV1; in Go the package name is the namespace, so harness.Session is
// HarnessV1Session. Consumer-facing aliases live in agent_types.go.
package harness
