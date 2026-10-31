package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS runPrompt denies a host (not provider-executed) tool call unless
// `hasTool({ tools: activeTools, ... })`, where activeTools holds the active
// user tools only. A call for an included builtin other than askUserQuestions
// is therefore answered with execution-denied, not failed as a host tool that
// could not be validated.
func TestR46HostCallForBuiltinNameIsDeniedLikeTS(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		builtinTools: map[string]BuiltinTool{"bash": {Tool: types.Tool{Name: "bash", Parameters: map[string]any{"type": "object"}}}},
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "bash", Input: `{}`},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	a, err := NewAgent(AgentSettings{Harness: mock.harness})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(mock.toolResults) != 1 {
		t.Fatalf("toolResults = %+v, want 1", mock.toolResults)
	}
	got, _ := mock.toolResults[0].Output.(map[string]interface{})
	if got["type"] != "execution-denied" || got["reason"] != "Tool 'bash' is inactive due to the HarnessAgent tool filtering policy." {
		t.Fatalf("Output = %#v, want the filtering denial", mock.toolResults[0].Output)
	}
}
