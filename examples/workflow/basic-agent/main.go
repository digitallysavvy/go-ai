package main

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/workflow"
)

type demoModel struct{}

func (demoModel) SpecificationVersion() string   { return "v3" }
func (demoModel) Provider() string               { return "demo" }
func (demoModel) ModelID() string                { return "demo-model" }
func (demoModel) SupportsTools() bool            { return true }
func (demoModel) SupportsStructuredOutput() bool { return true }
func (demoModel) SupportsImageInput() bool       { return false }
func (demoModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	return nil, fmt.Errorf("stream not implemented in example model")
}
func (demoModel) DoGenerate(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
	return &types.GenerateResult{Text: "workflow complete", FinishReason: types.FinishReasonStop}, nil
}

func main() {
	tools := []types.Tool{
		{Name: "echo", Description: "Echo input"},
		{Name: "lookup", Description: "Lookup mock data"},
	}
	agent, err := workflow.NewWorkflowAgent(workflow.WorkflowAgent{
		ID:       "basic-workflow-agent",
		Model:    demoModel{},
		System:   "You are a helpful assistant.",
		Tools:    tools,
		Prompt:   "Use tools when needed.",
		StopWhen: nil,
	})
	if err != nil {
		panic(err)
	}
	result, err := agent.Generate(context.Background(), "Say hello", nil)
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Text)
}
