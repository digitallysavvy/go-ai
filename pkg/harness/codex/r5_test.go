package codex_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/codex"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS: a replayed continuation (rerunContinue false) must not send a `start`.
func TestDoContinueTurn_ReplayDoesNotSendStart(t *testing.T) {
	var starts int32
	markers := make(chan map[string]any, 4)
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok",
		OnToolResult: func(f map[string]any) { markers <- f },
		OnFrame: func(ft string) {
			if ft == bridge.TypeStart {
				atomic.AddInt32(&starts, 1)
			}
		}})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)
	const bridgeStateDir = "/home/agent/.ai-sdk-harness/.agent-runs/s1/bridge"
	if err := sandbox.WriteTextFile(context.Background(), providerutils.SandboxWriteTextFileOptions{
		Path: bridgeStateDir + "/event-log.ndjson",
		Content: `{"seq":1,"type":"text-start"}` + "\n" +
			`{"seq":2,"type":"finish","finishReason":{"unified":"stop","raw":"stop"}}`,
	}); err != nil {
		t.Fatal(err)
	}
	h := codex.New(codex.Settings{StartupTimeout: 2 * time.Second, MintBridgeToken: func(string) string { return "tok" }})
	state, _ := harness.NewContinueTurnState(codex.HarnessID, map[string]any{
		"bridge": map[string]any{"port": 9999, "token": "tok", "lastSeenEventId": 2},
	})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir", ContinueFrom: state,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	c, err := sess.DoContinueTurn(context.Background(), harness.ContinueTurnOptions{Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
		t.Fatalf("turn settled early: %v", c.Err())
	case <-time.After(300 * time.Millisecond):
	}
	if sandbox.SpawnCount() != 1 {
		t.Fatalf("SpawnCount = %d, want 1 (attach fails, replay respawn)", sandbox.SpawnCount())
	}
	settle(t, c, markers)
	if got := atomic.LoadInt32(&starts); got != 0 {
		t.Fatalf("replayed continuation sent %d start frames, want 0", got)
	}
}

// TS: `start` carries the observability debug config.
func TestDoPromptTurn_ForwardsDebugConfig(t *testing.T) {
	captured := make(chan map[string]any, 1)
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(turn *bridgetest.Turn, start map[string]any) {
		captured <- start
		turn.Emit(finishFrame())
	}})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)
	h := codex.New(codex.Settings{StartupTimeout: 2 * time.Second, MintBridgeToken: func(string) string { return "tok" }})
	enabled := true
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir",
		Observability: &harness.Observability{Debug: &harness.DebugConfig{Enabled: &enabled, Level: harness.DebugLevelDebug}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	c, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, c)
	dbg, _ := (<-captured)["debug"].(map[string]any)
	if dbg["enabled"] != true || dbg["level"] != "debug" {
		t.Fatalf("start.debug = %v", dbg)
	}
}
