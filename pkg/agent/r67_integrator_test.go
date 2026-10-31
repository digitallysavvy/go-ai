package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r67Tool(name string) types.Tool {
	return types.Tool{
		Name: name, Type: types.ToolTypeFunction, Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
}

// TestR67IntegratorLegacyLoopResolvesPreparedToolCallers: TS generateText
// resolves experimental_toolCallers from the prepareCall result's tools and
// tool callers, so a caller map that only PrepareCall sets is validated, and
// it is validated against the prepared tools, not the configured ones.
func TestR67IntegratorLegacyLoopResolvesPreparedToolCallers(t *testing.T) {
	textModel := &functionalAgentLanguageModel{doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
	}}

	// Callers set only by PrepareCall: the unknown tool is reported.
	preparedCallers := NewToolLoopAgent(AgentConfig{
		Model: textModel,
		Tools: []types.Tool{r67Tool("x")},
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.ExperimentalToolCallers = ai.ExperimentalToolCallers{"missing": {ai.DirectToolCall}}
			return c
		},
	})
	if _, err := preparedCallers.Execute(context.Background(), "hi"); err == nil || !strings.Contains(err.Error(), `unknown tool "missing".`) {
		t.Fatalf("prepared callers: err = %v, want the unknown tool error", err)
	}

	// Callers checked against the prepared tools: x was replaced by y.
	preparedTools := NewToolLoopAgent(AgentConfig{
		Model:                   textModel,
		Tools:                   []types.Tool{r67Tool("x")},
		ExperimentalToolCallers: ai.ExperimentalToolCallers{"x": {ai.DirectToolCall}},
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.Tools = []types.Tool{r67Tool("y")}
			return c
		},
	})
	if _, err := preparedTools.Execute(context.Background(), "hi"); err == nil || !strings.Contains(err.Error(), `unknown tool "x".`) {
		t.Fatalf("prepared tools: err = %v, want the unknown tool error for the replaced tool", err)
	}
}

// TestR67IntegratorLegacyLoopPreparedOutputSkipsToolCallsFinish: with an
// output that only PrepareCall sets, a run that ends on a tool-calls finish
// leaves the output unset (TS generateText parses only when the last step
// finished with text), as for a configured output.
func TestR67IntegratorLegacyLoopPreparedOutputSkipsToolCallsFinish(t *testing.T) {
	model := &functionalAgentLanguageModel{doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "x", Arguments: map[string]interface{}{}}},
		}, nil
	}}
	var usage types.Usage
	parseCalls := 0
	agent := NewToolLoopAgent(AgentConfig{
		Model:    model,
		MaxSteps: 1,
		Tools:    []types.Tool{r67Tool("x")},
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.Output = r67UsageOutput{usage: &usage, calls: &parseCalls}
			return c
		},
	})
	result, err := agent.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "hi"})
	if err != nil {
		t.Fatalf("GenerateAgent: %v", err)
	}
	if parseCalls != 0 || result.Output != nil {
		t.Fatalf("output = %v (parse calls %d), want no parse after a tool-calls finish", result.Output, parseCalls)
	}
}
