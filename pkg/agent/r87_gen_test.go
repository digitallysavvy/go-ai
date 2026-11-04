package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR87genAgentStepRequestURLOrigin: GenerateAgent's loop step request carries
// the sanitized origin of the provider-reported URL, as TS generateText's
// step request does (934100cad4).
func TestR87genAgentStepRequestURLOrigin(t *testing.T) {
	model := &functionalAgentLanguageModel{
		modelID: "m",
		doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "ok",
				FinishReason: types.FinishReasonStop,
				RequestURL:   "https://user:password@model.example.test:8443/private?key=secret#fragment",
			}, nil
		},
	}
	result, err := NewToolLoopAgent(AgentConfig{Model: model}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) == 0 {
		t.Fatal("no steps")
	}
	for i, s := range result.Steps {
		if s.Request.URL != "https://model.example.test:8443" {
			t.Errorf("step %d request URL = %q", i, s.Request.URL)
		}
	}
}
