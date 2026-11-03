package agent

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ports tool-loop-agent.test.ts "LanguageModelCallOptions forwarding" (8
// cases) and "RequestOptions forwarding > should forward headers" for
// generate and stream, through Generate, Stream and the legacy agent loop
// (GenerateAgent).
func TestR83genAgentCallOptionsForwarding(t *testing.T) {
	temp, topP, pres, freq := 0.5, 0.9, 0.1, 0.2
	maxTok, topK, seed := 256, 40, 42
	var mu sync.Mutex
	var seen []*provider.GenerateOptions
	record := func(o *provider.GenerateOptions) {
		mu.Lock()
		seen = append(seen, o)
		mu.Unlock()
	}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			record(o)
			return &types.GenerateResult{Text: "reply", FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			record(o)
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "reply"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	a := NewToolLoopAgent(AgentConfig{
		Model:            model,
		Temperature:      &temp,
		MaxTokens:        &maxTok,
		TopP:             &topP,
		TopK:             &topK,
		PresencePenalty:  &pres,
		FrequencyPenalty: &freq,
		StopSequences:    []string{"STOP", "END"},
		Seed:             &seed,
		Headers:          map[string]string{"x-custom": "value"},
	})
	ctx := context.Background()
	if _, err := a.Generate(ctx, AgentGenerateOptions{Prompt: "test"}); err != nil {
		t.Fatal(err)
	}
	sr, err := a.Stream(ctx, AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{Prompt: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := sr.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GenerateAgent(ctx, AgentGenerateOptions{Prompt: "test"}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 {
		t.Fatalf("%d model calls, want 3", len(seen))
	}
	for i, o := range seen {
		if o.Temperature == nil || *o.Temperature != temp || o.MaxTokens == nil || *o.MaxTokens != maxTok ||
			o.TopP == nil || *o.TopP != topP || o.TopK == nil || *o.TopK != topK ||
			o.PresencePenalty == nil || *o.PresencePenalty != pres || o.FrequencyPenalty == nil || *o.FrequencyPenalty != freq ||
			!reflect.DeepEqual(o.StopSequences, []string{"STOP", "END"}) || o.Seed == nil || *o.Seed != seed {
			t.Fatalf("call %d: settings not forwarded: %+v", i, o)
		}
		if o.Headers["x-custom"] != "value" {
			t.Fatalf("call %d: headers %v", i, o.Headers)
		}
	}
}

// tool-loop-agent.test.ts "should forward include to generateText" / "to
// streamText": include.requestMessages puts the request messages on the
// result.
func TestR83genAgentIncludeRequestMessages(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "reply", FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "reply"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	a := NewToolLoopAgent(AgentConfig{Model: model, Include: &ai.IncludeOptions{RequestMessages: true}})
	ctx := context.Background()
	res, err := a.Generate(ctx, AgentGenerateOptions{Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, msgs []types.Message) {
		t.Helper()
		if len(msgs) != 1 || msgs[0].Role != types.RoleUser {
			t.Fatalf("%s: request messages %+v", name, msgs)
		}
		if tc, ok := msgs[0].Content[0].(types.TextContent); !ok || tc.Text != "test" {
			t.Fatalf("%s: request messages %+v", name, msgs)
		}
	}
	check("generate", res.FinalStep.Request.Messages)
	sr, err := a.Stream(ctx, AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{Prompt: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := sr.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	check("stream", sr.Request().Messages)
}
