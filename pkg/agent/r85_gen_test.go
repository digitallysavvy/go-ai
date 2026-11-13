package agent

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Port of tool-loop-agent-steering.test.ts 'should deliver queued messages
// one task at a time in generate()' (TS cf2796985a): with the default stop
// condition, each queued message starts the next task.
func TestR85genAgentQueueOneTaskAtATime(t *testing.T) {
	steering := ai.NewSteering()
	first := steering.Queue("Then tell me about Tokyo.")
	second := steering.Queue("Then tell me about Lagos.")
	var responses []types.GenerateResult
	for _, text := range []string{"It is sunny in Paris.", "Tokyo is rainy.", "Lagos is hot."} {
		responses = append(responses, types.GenerateResult{Text: text, Content: []types.ContentPart{types.TextContent{Text: text}}, FinishReason: types.FinishReasonStop})
	}
	model := &mockLanguageModel{responses: responses}
	a := NewToolLoopAgent(AgentConfig{Model: model})
	result, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "What is the weather in Paris?", ExperimentalSteering: steering})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Lagos is hot." {
		t.Fatalf("text = %q", result.Text)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for i, h := range []*ai.SteerHandle{first, second} {
		o, err := h.Wait(ctx)
		if err != nil || !o.Delivered || o.StepNumber != i+1 {
			t.Fatalf("outcome %d = %+v err=%v", i, o, err)
		}
	}
}
