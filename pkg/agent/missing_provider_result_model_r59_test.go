package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The missing-provider-result warning names the step's model (a PrepareCall
// override), as StreamText's does, not the agent's configured model.
func TestR59AgentMissingProviderResultWarningNamesTheStepModel(t *testing.T) {
	var mu sync.Mutex
	var providers []string
	ai.SetLogWarnings(func(o ai.LogWarningsOptions) {
		mu.Lock()
		defer mu.Unlock()
		for _, w := range o.Warnings {
			if strings.Contains(w.Message, "did not receive a result") {
				providers = append(providers, o.Provider+"/"+o.Model)
			}
		}
	})
	t.Cleanup(func() { ai.SetLogWarnings(nil) })
	newModel := func(name string) *testutil.MockLanguageModel {
		return &testutil.MockLanguageModel{ProviderName: name, ModelName: name + "-model", DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "ws", ToolName: "web_search", RawArguments: "{}", Arguments: map[string]interface{}{}, ProviderExecuted: true}},
				FinishReason: types.FinishReasonStop,
			}, nil
		}}
	}
	stepModel := newModel("step")
	a := NewToolLoopAgent(AgentConfig{
		Model:    newModel("configured"),
		MaxSteps: 1,
		Tools:    []types.Tool{{Name: "web_search", ProviderExecuted: true}},
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.Model = stepModel
			c.Internal = &ai.InternalOptions{ContinueOnMissingProviderResult: true}
			return c
		},
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(providers) != 1 || providers[0] != "step/step-model" {
		t.Fatalf("warning models = %q, want [step/step-model]", providers)
	}
}
