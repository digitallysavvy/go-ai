package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// cr-r90-carry-to-integrator 1: TS WorkflowAgent answers an approval whose
// signature is missing with getErrorMessage(error), the error's toString():
// "AI_InvalidToolApprovalSignatureError: Tool approval signature ..."
// (invalid-tool-approval-signature-error.ts 3, workflow-agent.ts 2091-2093).
func TestR90integSignatureErrorTextHasTSName(t *testing.T) {
	model := &wfMockModel{}
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model:                          model,
		ExperimentalToolApprovalSecret: []byte("workflow-secret"),
		Tools: []types.Tool{{
			Name:         "getWeather",
			ToolApproval: true,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				t.Fatal("unsigned approval executed")
				return nil, nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: workflowApprovalMessages("")})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Steps()
	tr := lastToolResultInPrompt(t, model, "weather-1")
	const want = `AI_InvalidToolApprovalSignatureError: Tool approval signature verification failed for approval "approval-weather-1" (tool call "weather-1"): missing signature`
	if tr.Output == nil || tr.Output.Type != types.ToolResultOutputErrorText {
		t.Fatalf("tool result = %+v, want error-text", tr.Output)
	}
	if got, _ := tr.Output.Value.(string); !strings.HasPrefix(got, "AI_InvalidToolApprovalSignatureError: ") || got != want {
		t.Fatalf("error text = %q, want %q", got, want)
	}
}
