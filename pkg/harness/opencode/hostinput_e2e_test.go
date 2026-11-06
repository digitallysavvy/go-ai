package opencode

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A bridge that waits on host input emits a `finish-step` (raw "host-input")
// and then goes quiet: no `finish`, no close. The host must pause there
// instead of hanging until the context ends (TS #23260).
func TestAgent_PausesAtHostInputFinishStep(t *testing.T) {
	srv := newServer(t, "tok", func(turn *bridgetest.Turn, _ map[string]any) {
		turn.Emit(map[string]any{"type": "tool-call", "toolCallId": "c1", "toolName": "lookup", "input": `{}`})
		turn.Emit(map[string]any{
			"type": "finish-step", "finishReason": map[string]any{"unified": "tool-calls", "raw": "host-input"},
			"usage":           map[string]any{"inputTokens": map[string]any{"total": 3}, "outputTokens": map[string]any{"total": 0}},
			"harnessMetadata": map[string]any{"opencode": map[string]any{"inferredStep": true}},
		})
	})
	h := mustOpenCode(t)
	a, err := harness.NewAgent(harness.AgentSettings{
		Harness:   h,
		UserTools: map[string]types.Tool{"lookup": {Name: "lookup", Parameters: map[string]interface{}{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := a.CreateSession(ctx, harness.CreateSessionOptions{SessionID: "s1", SandboxSession: newFakeSandbox(srv)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Destroy(context.Background()) })

	res, err := a.Stream(ctx, agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: sess}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("turn hung until the context ended")
	}
	steps := res.Steps()
	if len(steps) != 1 || steps[0].FinishReason != types.FinishReasonToolCalls || len(steps[0].ToolCalls) != 1 {
		t.Fatalf("steps = %+v", steps)
	}
}

func mustOpenCode(t *testing.T) harness.Harness {
	t.Helper()
	h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return "tok" }, StartupTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return h
}
