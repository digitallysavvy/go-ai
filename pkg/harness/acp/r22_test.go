package acp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// TS `ACPToolCall` carries `content`; isMcpToolCall / isNativeToolCall
// callbacks receive it.
func TestToolCallCandidateFrameKeepsContent(t *testing.T) {
	var f ToolCallCandidateFrame
	in := `{"requestId":"r","toolCall":{"toolCallId":"c","title":"t","content":[{"type":"diff","path":"a.go","newText":"x"}]}}`
	if err := json.Unmarshal([]byte(in), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.ToolCall.Content) != 1 || f.ToolCall.Content[0]["type"] != "diff" || f.ToolCall.Content[0]["path"] != "a.go" {
		t.Fatalf("content = %+v", f.ToolCall.Content)
	}
}

// TS validates the response format before the replay-only check.
func TestDoPromptTurnResponseFormatCheckedBeforeReplayOnly(t *testing.T) {
	s := &session{p: sessionParams{harnessID: "h", sandbox: &fakeSandbox{files: map[string]string{}}, homePath: "/home/x", skillsDir: ".skills"}, replayOnly: true}
	_, err := s.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		ResponseFormat: &harness.ResponseFormat{Type: harness.ResponseFormatJSON},
	})
	if err == nil || !strings.Contains(err.Error(), "requires a JSON schema for structured output") {
		t.Fatalf("err = %v, want the JSON schema error", err)
	}
	_, err = s.DoPromptTurn(context.Background(), harness.PromptTurnOptions{})
	if err == nil || !strings.Contains(err.Error(), "disk replay only") {
		t.Fatalf("err = %v, want the replay-only error", err)
	}
}
