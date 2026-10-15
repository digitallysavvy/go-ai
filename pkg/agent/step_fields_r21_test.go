package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The legacy loop's StepResult carries reasoning, files, sources and the
// response id/model like GenerateText's.
func TestLegacyLoopStepResultCarriesContentFields(t *testing.T) {
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		Text: "hi",
		Content: []types.ContentPart{
			types.ReasoningContent{Text: "think"},
			types.GeneratedFileContent{MediaType: "image/png", Data: []byte{1}},
			types.SourceContent{ID: "s1", URL: "https://x"},
		},
		FinishReason: types.FinishReasonStop,
	}}}
	a := NewToolLoopAgent(AgentConfig{Model: model})
	res, err := a.Execute(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	st := res.Steps[0]
	if len(st.Reasoning) != 1 || st.ReasoningText != "think" || len(st.Files) != 1 || len(st.Sources) != 1 {
		t.Fatalf("step = %+v", st)
	}
	if st.Response.ID == "" || st.Response.ModelID == "" || st.Response.Timestamp.IsZero() {
		t.Fatalf("response = %+v", st.Response)
	}
}

// The legacy loop hands tools the step messages and usage and enforces the
// per-tool timeout, like GenerateText.
func TestLegacyLoopToolOptionsAndTimeout(t *testing.T) {
	var gotMsgs int
	var gotUsage bool
	tool := types.Tool{
		Name:       "slow",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			gotMsgs, gotUsage = len(o.Messages), o.Usage != nil
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "1", ToolName: "slow", Arguments: map[string]interface{}{}}},
	}, {Text: "done", FinishReason: types.FinishReasonStop}}}
	d := 20 * time.Millisecond
	a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{tool}, Timeout: &ai.TimeoutConfig{ToolMs: &d}})
	res, err := a.Execute(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if gotMsgs == 0 || !gotUsage {
		t.Fatalf("messages=%d usage=%v", gotMsgs, gotUsage)
	}
	var te *ai.TimeoutError
	if len(res.ToolResults) != 1 || !errors.As(res.ToolResults[0].Error, &te) || te.Reason != ai.TimeoutReasonTool {
		t.Fatalf("tool results = %+v", res.ToolResults)
	}
}
