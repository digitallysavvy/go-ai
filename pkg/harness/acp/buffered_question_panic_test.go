package acp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A result submitted before its question arrives is buffered; when the
// question then arrives and the adapter's ToNativeResponse panics on it (TS
// throws), the turn settles with that error instead of crashing the process.
func TestBufferedQuestionResultToNativeResponsePanicSettlesTurn(t *testing.T) {
	started := make(chan *bridgetest.Turn, 1)
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok",
		OnStart: func(turn *bridgetest.Turn, _ map[string]any) { started <- turn }})
	t.Cleanup(srv.Close)
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return "tok" }
		s.HarnessID = "test-acp"
		s.AskUserQuestions = &AskUserQuestionsSettings{
			RequestMethod: "test/ask",
			FromNativeRequest: func(any, *ToolCall) *harness.ToolCallPart {
				return &harness.ToolCallPart{ToolCallID: "question-1", ToolName: "askUserQuestions", Input: `{}`}
			},
			ToNativeResponse: func(any, types.ToolResultContent) any { panic("requires a JSON tool result") },
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: newFakeSandbox(srv), SessionWorkDir: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Ask."), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	turn := <-started
	result := types.ToolResultContent{ToolCallID: "question-1", ToolName: "askUserQuestions",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "nope"}}
	if err := control.SubmitToolResult(context.Background(), harness.ToolResultSubmission{ToolCallID: "question-1", Output: "nope", ToolResult: &result}); err != nil {
		t.Fatalf("buffering SubmitToolResult: %v", err)
	}
	turn.Emit(map[string]any{"type": "acp-question-request", "requestId": "request-1", "nativeRequest": map[string]any{}})
	select {
	case <-control.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not settle")
	}
	if err := control.Err(); err == nil || !strings.Contains(err.Error(), "requires a JSON tool result") {
		t.Fatalf("turn error = %v, want the adapter's error", err)
	}
}
