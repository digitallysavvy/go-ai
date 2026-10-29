package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r63: TS ToolLoopAgent hands its settings' toolChoice to generateText and
// streamText as given (undefined when unset), so the start and step start
// events report no tool choice, and generateText's prepareToolChoice sends
// the model auto.
func TestR63AgentUnsetToolChoiceIsPassedAsGiven(t *testing.T) {
	var mu sync.Mutex
	var modelChoices, eventChoices []types.ToolChoice
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			modelChoices = append(modelChoices, o.ToolChoice)
			mu.Unlock()
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			modelChoices = append(modelChoices, o.ToolChoice)
			mu.Unlock()
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	record := func(c types.ToolChoice) {
		mu.Lock()
		eventChoices = append(eventChoices, c)
		mu.Unlock()
	}
	a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{{Name: "a", Parameters: map[string]interface{}{"type": "object"}}}})
	opts := AgentGenerateOptions{
		Prompt:      "go",
		OnStart:     func(_ context.Context, e ai.OnStartEvent) { record(e.ToolChoice) },
		OnStepStart: func(_ context.Context, e ai.OnStepStartEvent) { record(e.ToolChoice) },
	}
	if _, err := a.Generate(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	st, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: opts})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadAll(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(modelChoices) != 2 || modelChoices[0] != types.AutoToolChoice() || modelChoices[1] != types.AutoToolChoice() {
		t.Errorf("model tool choices = %+v, want auto for Generate and Stream", modelChoices)
	}
	if len(eventChoices) != 4 {
		t.Fatalf("events = %d, want 4", len(eventChoices))
	}
	for _, c := range eventChoices {
		if c.Type != "" {
			t.Errorf("event ToolChoice = %+v, want unset (TS undefined)", c)
		}
	}
}
