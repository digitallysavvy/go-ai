package agent

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The legacy loop times its steps as TS generateText: stepStartTimestampMs is
// taken just before onLanguageModelCallStart, so a slow callback is in both
// the model call's response time and the step time.
func TestR64GenerateAgentStepClockStartsBeforeLanguageModelCallStart(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
	}}
	res, err := NewToolLoopAgent(AgentConfig{
		Model: model,
		OnLanguageModelCallStart: func(context.Context, ai.LanguageModelCallStartEvent) {
			time.Sleep(80 * time.Millisecond)
		},
	}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	p := res.Steps[0].Performance
	if p.ResponseTimeMs < 80 {
		t.Fatalf("ResponseTimeMs = %d, want it to include the 80ms OnLanguageModelCallStart", p.ResponseTimeMs)
	}
	if p.StepTimeMs < 80 {
		t.Fatalf("StepTimeMs = %d, want it to include the 80ms OnLanguageModelCallStart", p.StepTimeMs)
	}
}
