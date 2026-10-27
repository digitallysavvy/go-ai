package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS ToolLoopAgent has one path, through generateText, so every model call
// carries both the ai-sdk-agent/tool-loop and the ai/<version> user-agent
// segments. The legacy GenerateAgent loop calls the provider itself and sent
// the caller's headers untagged.
func TestLegacyLoopSendsTheSameUserAgentAsGenerate(t *testing.T) {
	headersOf := func(run func(*ToolLoopAgent) error) map[string]string {
		var got map[string]string
		model := &functionalAgentLanguageModel{
			doGenerate: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				got = opts.Headers
				return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
			},
		}
		a := NewToolLoopAgent(AgentConfig{Model: model, Headers: map[string]string{"x-user": "custom"}})
		if err := run(a); err != nil {
			t.Fatal(err)
		}
		return got
	}
	viaGenerate := headersOf(func(a *ToolLoopAgent) error {
		_, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "p"})
		return err
	})
	viaLegacy := headersOf(func(a *ToolLoopAgent) error {
		_, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "p"})
		return err
	})
	if viaLegacy["user-agent"] == "" || viaLegacy["user-agent"] != viaGenerate["user-agent"] || viaLegacy["x-user"] != "custom" {
		t.Fatalf("legacy headers %#v, Generate headers %#v", viaLegacy, viaGenerate)
	}

	// WorkflowAgent tags its own requests; nothing more is added.
	var workflowUA string
	model := &functionalAgentLanguageModel{
		doGenerate: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			workflowUA = opts.Headers["user-agent"]
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}
	a := NewToolLoopAgent(AgentConfig{Model: model, Headers: map[string]string{"user-agent": "ai-sdk-agent/workflow"}})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if workflowUA != "ai-sdk-agent/workflow" {
		t.Fatalf("workflow user-agent = %q", workflowUA)
	}
}
