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

// TS toNativeResponse throws on a malformed client result, which surfaces as
// a rejected submitToolResult; Go must return that as an error instead of
// panicking in the caller's goroutine.
func TestSubmitToolResultToNativeResponsePanicIsReturnedAsError(t *testing.T) {
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
	questionSeen := make(chan struct{}, 1)
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Ask."), Emit: func(p harness.StreamPart) {
		if tc, ok := p.(*harness.ToolCallPart); ok && tc.ToolCallID == "question-1" {
			select {
			case questionSeen <- struct{}{}:
			default:
			}
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	turn := <-started
	turn.Emit(map[string]any{"type": "acp-question-request", "requestId": "request-1", "nativeRequest": map[string]any{}})
	select {
	case <-questionSeen: // the question is now active
	case <-time.After(10 * time.Second):
		t.Fatal("question tool call was not forwarded")
	}
	result := types.ToolResultContent{ToolCallID: "question-1", ToolName: "askUserQuestions",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "nope"}}
	err = control.SubmitToolResult(context.Background(), harness.ToolResultSubmission{ToolCallID: "question-1", Output: "nope", ToolResult: &result})
	if err == nil || !strings.Contains(err.Error(), "requires a JSON tool result") {
		t.Fatalf("err = %v, want the adapter's error", err)
	}
}
