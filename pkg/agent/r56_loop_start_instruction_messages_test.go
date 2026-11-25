package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The legacy loop's OnStart event carries the instruction messages like its
// step start event and GenerateText's start event (TS generateText onStart
// reports instructions given as system messages).
func TestR56LoopStartEventInstructionMessages(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
		},
	}
	var start, stepStart []types.Message
	a := NewToolLoopAgent(AgentConfig{
		Model: model,
		InstructionMessages: []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "be brief"}}},
		},
		OnStart:          func(_ context.Context, e ai.OnStartEvent) { start = e.InstructionMessages },
		OnStepStartEvent: func(_ context.Context, e ai.OnStepStartEvent) { stepStart = e.InstructionMessages },
	})
	if _, err := a.Execute(context.Background(), "Hello"); err != nil {
		t.Fatal(err)
	}
	if len(stepStart) != 1 {
		t.Fatalf("step start InstructionMessages = %v", stepStart)
	}
	if len(start) != 1 {
		t.Fatalf("start InstructionMessages = %v, want the configured system message", start)
	}
}
