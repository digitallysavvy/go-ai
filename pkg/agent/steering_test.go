package agent

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #23813: ToolLoopAgent.generate accepts experimental_steering.
func TestToolLoopAgentGenerateDeliversSteering(t *testing.T) {
	model := &mockLanguageModel{responses: []types.GenerateResult{{Text: "answer", FinishReason: types.FinishReasonStop}}}
	a := NewToolLoopAgent(AgentConfig{Model: model})
	steering := ai.NewSteering()
	h := steering.Steer("I meant Paris, Texas.")
	if _, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "weather", ExperimentalSteering: steering}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	o, err := h.Wait(ctx)
	if err != nil || !o.Delivered || o.StepNumber != 0 {
		t.Fatalf("outcome=%+v err=%v", o, err)
	}
	last := model.options[0].Prompt.Messages
	if len(last) < 2 || last[len(last)-1].Role != types.RoleUser {
		t.Fatalf("prompt = %+v", last)
	}
}
