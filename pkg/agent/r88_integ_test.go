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

func r88integTextStream(text string) provider.TextStream {
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: text},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})
}

// cr-r88-gen-to-integrator CR 2, tool-loop-agent-steering.test.ts "should
// stream delivered messages in stream()" (TS 1a1da8d68c): a message steered
// during the first model call is streamed once, for step 1.
func TestR88integAgentStreamsSteeringMessage(t *testing.T) {
	steering := ai.NewSteering()
	var mu sync.Mutex
	call := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		defer mu.Unlock()
		call++
		if call == 1 {
			steering.Steer("Also tell me about Tokyo.", ai.WithSteerID("steer-1"))
			return r88integTextStream("Sunny."), nil
		}
		return r88integTextStream("Tokyo is rainy."), nil
	}}
	a := NewToolLoopAgent(AgentConfig{Model: model})
	res, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{
		Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var parts []provider.StreamChunk
	for c := range res.Chunks() {
		if c.Type == provider.ChunkTypeSteeringMessage {
			parts = append(parts, c)
		}
	}
	if len(parts) != 1 {
		t.Fatalf("steering parts = %d, want 1", len(parts))
	}
	p := parts[0]
	if p.ID != "steer-1" || p.StepNumber == nil || *p.StepNumber != 1 || p.SteeringMessage == nil || p.SteeringMessage.Role != types.RoleUser {
		t.Fatalf("steering part = %#v", p)
	}
	var text string
	for _, c := range p.SteeringMessage.Content {
		if tc, ok := c.(types.TextContent); ok {
			text += tc.Text
		}
	}
	if text != "Also tell me about Tokyo." {
		t.Fatalf("steering message text = %q", text)
	}
}
