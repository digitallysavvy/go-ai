package deepagents

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// r88toolsSpawn starts a session against a fake sandbox and returns the
// first spawn command.
func r88toolsSpawn(t *testing.T, settings Settings) string {
	t.Helper()
	srv := bridgetest.NewServer(bridgetest.Options{AnyToken: true})
	t.Cleanup(srv.Close)
	sandbox := newFakeSandbox(srv)
	sess, err := CreateDeepAgents(settings).DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/deepagents-test-session",
		SandboxSession: sandbox,
	})
	if err == nil {
		t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	}
	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	if len(sandbox.spawnCommands) == 0 {
		t.Fatalf("expected a spawn (DoStart: %v)", err)
	}
	return sandbox.spawnCommands[0]
}

// TS e4a6112776 deepagents-harness.test.ts: the resynced bridge exits
// without --home-dir; the host passes the sandbox HOME right after
// --workdir (deepagents-harness.ts 434).
func TestR88toolsDeepAgentsSpawnPassesHomeDir(t *testing.T) {
	cmd := r88toolsSpawn(t, Settings{})
	want := "--workdir '/vercel/sandbox/deepagents-test-session' --home-dir '/home/vercel-sandbox' --bridge-state-dir "
	if !strings.Contains(cmd, want) {
		t.Fatalf("spawn command = %q, want it to contain %q", cmd, want)
	}
}

// TS bb8aa09ec8 "registers the native delete tool as a mutation".
func TestR88toolsDeepAgentsDeleteBuiltin(t *testing.T) {
	tool, ok := BuiltinTools()["delete"]
	if !ok {
		t.Fatal("BuiltinTools() lacks delete")
	}
	if tool.ToolUseKind != harness.BuiltinToolUseKindEdit {
		t.Fatalf("ToolUseKind = %q, want edit", tool.ToolUseKind)
	}
	if tool.NativeName != "" || tool.CommonName != "" {
		t.Fatalf("delete has native %q / common %q, want neither", tool.NativeName, tool.CommonName)
	}
	if tool.Name != "delete" || tool.Description != "Delete a file or directory" {
		t.Fatalf("tool = %q / %q", tool.Name, tool.Description)
	}
	schema, _ := tool.Parameters.(map[string]any)
	req, _ := schema["required"].([]string)
	props, _ := schema["properties"].(map[string]any)
	fp, _ := props["file_path"].(map[string]any)
	if len(req) != 1 || req[0] != "file_path" || fp["type"] != "string" || len(props) != 1 {
		t.Fatalf("schema = %#v", schema)
	}
}

// TS bb8aa09ec8 "forwards individual active skill directories as readable
// roots"; without skills the key is absent.
func TestR88toolsDeepAgentsReadableSkillRoots(t *testing.T) {
	for _, skills := range [][]harness.Skill{
		{{Name: "demo", Description: "Demo skill.", Content: "Read reference.md."}},
		nil,
	} {
		const token = "r88-skill-token"
		starts := make(chan map[string]any, 1)
		srv := newServer(t, token, func(turn *bridgetest.Turn, start map[string]any) {
			starts <- start
			turn.Emit(map[string]any{"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
				"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}}})
		})
		sess, err := CreateDeepAgents(Settings{MintBridgeToken: func(string) string { return token }}).DoStart(context.Background(), harness.StartOptions{
			SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/deepagents-test-session", SandboxSession: newFakeSandbox(srv),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
			TurnSettings: harness.TurnSettings{Skills: skills}, Prompt: harness.TextPrompt("Use demo."), Emit: func(harness.StreamPart) {},
		}); err != nil {
			t.Fatal(err)
		}
		start := <-starts
		roots, present := start["readableSkillRoots"]
		if skills == nil {
			if present {
				t.Fatalf("readableSkillRoots sent without skills: %v", roots)
			}
		} else if list, _ := roots.([]any); len(list) != 1 || list[0] != "/home/vercel-sandbox/.agents/skills/demo" {
			t.Fatalf("readableSkillRoots = %#v", roots)
		}
		_ = sess.DoDestroy(context.Background())
	}
}
