package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Round 89 gen tests (TS 382d0b8eaf).

func r89genOKModel() *functionalAgentLanguageModel {
	return &functionalAgentLanguageModel{
		doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "ok", Content: []types.ContentPart{types.TextContent{Text: "ok"}}, FinishReason: types.FinishReasonStop}, nil
		},
	}
}

// TS ToolLoopAgent.generate/stream validate the call options (prepareCall,
// tool-loop-agent.ts 118-127) before generateText/streamText check the
// prompt, so a call with invalid options and no prompt reports the options.
func TestR89genAgentCallOptionsCheckedBeforePrompt(t *testing.T) {
	a := NewToolLoopAgent(AgentConfig{
		Model:             r89genOKModel(),
		CallOptionsSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "required": []interface{}{"ok"}}),
	})
	opts := AgentGenerateOptions{CallOptions: map[string]interface{}{}}
	_, err := a.Generate(context.Background(), opts)
	var validation *providererrors.ValidationError
	var prompt *providererrors.InvalidPromptError
	if !errors.As(err, &validation) || errors.As(err, &prompt) {
		t.Fatalf("Generate err = %v, want the call options error", err)
	}
	_, err = a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: opts})
	if !errors.As(err, &validation) || errors.As(err, &prompt) {
		t.Fatalf("Stream err = %v, want the call options error", err)
	}
}

// TS checks the prompt of the prepared call, so a PrepareCall that supplies
// the messages makes a call without a prompt valid.
func TestR89genAgentPrepareCallSuppliesMessages(t *testing.T) {
	a := NewToolLoopAgent(AgentConfig{
		Model: r89genOKModel(),
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.Messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}
			return c
		},
	})
	if _, err := a.Generate(context.Background(), AgentGenerateOptions{}); err != nil {
		t.Fatalf("Generate err = %v", err)
	}
	if _, err := a.Stream(context.Background(), AgentStreamOptions{}); err != nil {
		t.Fatalf("Stream err = %v", err)
	}
}

// A prompt error keeps the *ValidationError cause v0.5.2 returned.
func TestR89genAgentPromptErrorKeepsCause(t *testing.T) {
	a := NewToolLoopAgent(AgentConfig{Model: r89genOKModel()})
	_, err := a.Generate(context.Background(), AgentGenerateOptions{})
	var prompt *providererrors.InvalidPromptError
	var validation *providererrors.ValidationError
	if !errors.As(err, &prompt) || !errors.As(err, &validation) {
		t.Fatalf("Generate err = %v, want InvalidPromptError with a ValidationError cause", err)
	}
}

