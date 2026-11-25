package workflow

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type fixedGenerateModel struct {
	wfMockModel
	res *types.GenerateResult
}

func (m *fixedGenerateModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	return m.res, nil
}

func generateWithOutput(t *testing.T, res *types.GenerateResult, output interface{}, tools []types.Tool) (*WorkflowResult, []string, error) {
	t.Helper()
	var events []string
	a, err := NewWorkflowAgent(WorkflowAgent{
		Model: &fixedGenerateModel{res: res}, Output: output, Tools: tools,
		OnStepEnd: func(context.Context, ai.OnStepFinishEvent) { events = append(events, "step") },
		OnEnd:     func(context.Context, ai.OnFinishEvent) { events = append(events, "end") },
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "p"})
	return r, events, err
}

// TS workflow-agent-generate.test.ts: onEnd runs before the output is parsed,
// so a parse failure rejects after the end event.
func TestWorkflowGenerateRunsOnEndBeforeOutputParseFailure(t *testing.T) {
	_, events, err := generateWithOutput(t,
		&types.GenerateResult{Text: "{", FinishReason: types.FinishReasonStop},
		ai.JSONOutput(ai.JSONOutputOptions{}), nil)
	if err == nil {
		t.Fatal("want a parse error")
	}
	if want := []string{"step", "end"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

// TS: a tool-calls finish returns the client tool calls and leaves the output
// unset; the output is not parsed.
func TestWorkflowGenerateClientToolCallsDoNotParseOutput(t *testing.T) {
	r, events, err := generateWithOutput(t,
		&types.GenerateResult{Text: "{", FinishReason: types.FinishReasonToolCalls,
			ToolCalls: []types.ToolCall{{ID: "c", ToolName: "lookup", Arguments: map[string]interface{}{"city": "x"}}}},
		ai.JSONOutput(ai.JSONOutputOptions{}), []types.Tool{{Name: "lookup"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Output != nil || len(r.Steps) != 1 {
		t.Fatalf("output=%v steps=%d", r.Output, len(r.Steps))
	}
	if want := []string{"step", "end"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestWorkflowGenerateEmptyStopParsesTextOutput(t *testing.T) {
	r, _, err := generateWithOutput(t, &types.GenerateResult{FinishReason: types.FinishReasonStop}, ai.TextOutput(), nil)
	if err != nil || r.Output != "" {
		t.Fatalf("output=%#v err=%v", r.Output, err)
	}
}
