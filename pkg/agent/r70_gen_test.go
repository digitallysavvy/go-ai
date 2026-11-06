package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r70StepModel answers the first call with a tool call (and text) and later
// calls with a stop finish.
func r70StepModel() *functionalAgentLanguageModel {
	n := 0
	return &functionalAgentLanguageModel{doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		n++
		if n == 1 {
			return &types.GenerateResult{
				Text:         "first",
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "x", Arguments: map[string]interface{}{}}},
			}, nil
		}
		return &types.GenerateResult{Text: "second", FinishReason: types.FinishReasonStop}, nil
	}}
}

// Carry-in r70: AgentResult.Text is the last step's text (TS lastStep.text)
// in the legacy loop, for a natural end and for a stop condition after a
// tool-calls step, while Output follows the GenerateText rule.
func TestR70Gen_LegacyLoopTextIsLastStepText(t *testing.T) {
	result, err := NewToolLoopAgent(AgentConfig{Model: r70StepModel(), Tools: []types.Tool{r67Tool("x")}}).
		GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != 2 || result.Text != "second" || result.Output != "second" {
		t.Errorf("natural end: steps=%d Text=%q Output=%#v, want 2/second/second", len(result.Steps), result.Text, result.Output)
	}

	result, err = NewToolLoopAgent(AgentConfig{Model: r70StepModel(), Tools: []types.Tool{r67Tool("x")}, StopWhen: []ai.StopCondition{ai.IsStepCount(1)}}).
		GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != 1 || result.Text != "first" || result.Output != nil || result.FinishReason != types.FinishReasonToolCalls {
		t.Errorf("stop condition: steps=%d Text=%q Output=%#v finish=%q, want 1/first/nil/tool-calls", len(result.Steps), result.Text, result.Output, result.FinishReason)
	}
}

// A model whose DoGenerate returns (nil, nil) fails the legacy loop with an
// error instead of panicking (TS rejects reading the response).
func TestR70Gen_LegacyLoopNilModelResultIsAnError(t *testing.T) {
	model := &functionalAgentLanguageModel{doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return nil, nil
	}}
	result, err := NewToolLoopAgent(AgentConfig{Model: model}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "hi"})
	if err == nil || result != nil {
		t.Fatalf("GenerateAgent = %v, %v; want an error", result, err)
	}
	if !strings.Contains(err.Error(), "language model mock/mock-model returned no result") {
		t.Errorf("error = %q", err)
	}
}

// Every legacy-loop step carries the run's call ID (TS DefaultStepResult
// callId), the one the start and end events report.
func TestR70Gen_LegacyLoopStepsCarryCallID(t *testing.T) {
	var startID, endID string
	var stepEventSteps []types.StepResult
	result, err := NewToolLoopAgent(AgentConfig{Model: r70StepModel(), Tools: []types.Tool{r67Tool("x")}}).
		GenerateAgent(context.Background(), AgentGenerateOptions{
			Prompt:      "hi",
			OnStart:     func(_ context.Context, e ai.OnStartEvent) { startID = e.CallID },
			OnStepStart: func(_ context.Context, e ai.OnStepStartEvent) { stepEventSteps = e.Steps },
			OnEnd:       func(_ context.Context, e ai.OnFinishEvent) { endID = e.CallID },
		})
	if err != nil {
		t.Fatal(err)
	}
	if startID == "" || startID != endID {
		t.Fatalf("start/end call IDs = %q/%q", startID, endID)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("%d steps", len(result.Steps))
	}
	for i, s := range result.Steps {
		if s.CallID != startID {
			t.Errorf("Steps[%d].CallID = %q, want %q", i, s.CallID, startID)
		}
	}
	if len(stepEventSteps) != 1 || stepEventSteps[0].CallID != startID {
		t.Errorf("OnStepStart previous steps = %+v", stepEventSteps)
	}
}
