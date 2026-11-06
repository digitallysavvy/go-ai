package claudecode_test

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
)

func finishFrame() map[string]any {
	return map[string]any{
		"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
		"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
	}
}

func startHarnessWithServer(t *testing.T, srv *bridgetest.Server) harness.Session {
	t.Helper()
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)
	h, err := claudecode.New(claudecode.Settings{
		MintBridgeToken: func(string) string { return "tok" },
		StartupTimeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir",
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	return sess
}

// Review round 18 fix 3 (TS createBridgeErrorHandler): a bridge `error` frame
// is logged as "[harness:claude-code:error session=<id>] bridge emitted an
// error frame: ...". Before the fix no handler was wired and the frame was
// silently dropped from the log.
func TestBridgeErrorFrameIsLogged(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	restored := false
	restore := func() {
		if !restored {
			os.Stderr = orig
			_ = w.Close()
			restored = true
		}
	}
	t.Cleanup(restore)
	captured := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		captured <- string(b)
	}()

	srv := bridgetest.NewServer(bridgetest.Options{
		Token: "tok",
		OnStart: func(turn *bridgetest.Turn, _ map[string]any) {
			turn.Emit(map[string]any{"type": "error", "error": map[string]any{"name": "Error", "message": "bridge exploded"}})
			turn.Emit(finishFrame())
		},
	})
	sess := startHarnessWithServer(t, srv)
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"),
		Emit:   func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("turn never settled")
	}
	restore()
	var out string
	select {
	case out = <-captured:
	case <-time.After(2 * time.Second):
		t.Fatal("stderr capture did not finish")
	}
	if !strings.Contains(out, "[harness:claude-code:error session=s1] bridge emitted an error frame") || !strings.Contains(out, "bridge exploded") {
		t.Fatalf("bridge error frame was not logged; stderr = %q", out)
	}
}
