package deepagents

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// TS deepagents-harness.ts never sets BRIDGE_REPLAY_FROM_DISK: a respawned
// bridge resumes through `--resume true` only, so stale events of the previous
// process are not re-delivered into the next turn.
func TestDoStart_ResumeRespawnPassesResumeFlagNotDiskReplay(t *testing.T) {
	srv := newServer(t, "tok", func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h := CreateDeepAgents(Settings{MintBridgeToken: func(string) string { return "tok" }})
	resume, err := harness.NewResumeSessionState(HarnessID, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SessionWorkDir: "/w", SandboxSession: sandbox, ResumeFrom: resume,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	if !strings.HasSuffix(sandbox.spawnCommands[0], " --resume true") {
		t.Fatalf("command = %q", sandbox.spawnCommands[0])
	}
	if _, ok := sandbox.spawnEnvs[0]["BRIDGE_REPLAY_FROM_DISK"]; ok {
		t.Fatalf("env sets BRIDGE_REPLAY_FROM_DISK: %v", sandbox.spawnEnvs[0])
	}
}
