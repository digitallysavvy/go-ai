package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r67UsageOutput records the usage its ParseCompleteOutput receives.
type r67UsageOutput struct {
	usage *types.Usage
	calls *int
}

func (o r67UsageOutput) ParseCompleteOutput(_ context.Context, opts ai.ParseCompleteOutputOptions) (interface{}, error) {
	*o.calls++
	if opts.Usage != nil {
		u := *opts.Usage
		*o.usage = u
	}
	return "parsed:" + opts.Text, nil
}

func r67Int64(v int64) *int64 { return &v }

// TestR67LegacyLoopPreparedCallToolSearchAndOutput: TS ToolLoopAgent passes
// the prepareCall result to generateText, which resolves the tool search
// state from those tools and parses the output with that output and the
// last step's usage. The legacy loop used the agent's configured tools (a
// toolSearch added by PrepareCall stayed unbound) and output, and passed the
// total usage.
func TestR67LegacyLoopPreparedCallToolSearchAndOutput(t *testing.T) {
	calls := 0
	var secondStepTools []string
	model := &functionalAgentLanguageModel{
		doGenerate: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "toolSearch", Arguments: map[string]interface{}{"query": "secret"}}},
					Usage:        types.Usage{InputTokens: r67Int64(10), OutputTokens: r67Int64(1), TotalTokens: r67Int64(11)},
				}, nil
			}
			for _, tl := range opts.Tools {
				secondStepTools = append(secondStepTools, tl.Name)
			}
			return &types.GenerateResult{
				Text: "done", FinishReason: types.FinishReasonStop,
				Usage: types.Usage{InputTokens: r67Int64(3), OutputTokens: r67Int64(2), TotalTokens: r67Int64(5)},
			}, nil
		},
	}
	secret := types.Tool{
		Name: "secret", Description: "a secret tool", DeferLoading: true, Type: types.ToolTypeFunction,
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	var parsedUsage types.Usage
	parseCalls := 0
	var stepStartOutputs []interface{}
	var stepStartTools [][]string
	agent := NewToolLoopAgent(AgentConfig{
		Model:    model,
		MaxSteps: 5,
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.Tools = []types.Tool{ai.ToolSearch(), secret}
			c.Output = r67UsageOutput{usage: &parsedUsage, calls: &parseCalls}
			return c
		},
		OnStepStartEvent: func(_ context.Context, e ai.OnStepStartEvent) {
			stepStartOutputs = append(stepStartOutputs, e.Output)
			names := []string{}
			for _, tl := range e.Tools {
				names = append(names, tl.Name)
			}
			stepStartTools = append(stepStartTools, names)
		},
	})
	result, err := agent.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "hi"})
	if err != nil {
		t.Fatalf("GenerateAgent: %v", err)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(result.Steps))
	}
	if tr := result.Steps[0].ToolResults; len(tr) != 1 || tr[0].Error != nil {
		t.Fatalf("toolSearch result = %+v, want a bound search result", tr)
	}
	if len(secondStepTools) != 2 || secondStepTools[1] != "secret" {
		t.Fatalf("second step model tools = %v, want the discovered secret tool", secondStepTools)
	}
	if parseCalls != 1 || result.Output != "parsed:done" {
		t.Fatalf("output = %v (parse calls %d), want the PrepareCall output parsed", result.Output, parseCalls)
	}
	if parsedUsage.TotalTokens == nil || *parsedUsage.TotalTokens != 5 {
		t.Fatalf("parse usage = %+v, want the last step's usage (5)", parsedUsage)
	}
	for i, out := range stepStartOutputs {
		if out == nil {
			t.Fatalf("step %d start event Output not set", i)
		}
		if len(stepStartTools[i]) != 2 {
			t.Fatalf("step %d start event Tools = %v, want the call's tools", i, stepStartTools[i])
		}
	}
}

// TestR67LegacyLoopValidatesPreparedToolCallConcurrency: TS generateText
// validates the prepareCall result's toolCallConcurrency before the run
// starts, whether or not the model calls a tool, and an invalid configured
// value that PrepareCall replaces is not an error.
func TestR67LegacyLoopValidatesPreparedToolCallConcurrency(t *testing.T) {
	zero, two := 0, 2
	starts := 0
	textModel := &functionalAgentLanguageModel{doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
	}}
	bad := NewToolLoopAgent(AgentConfig{
		Model:   textModel,
		OnStart: func(context.Context, ai.OnStartEvent) { starts++ },
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.ToolCallConcurrency = &zero
			return c
		},
		OnStepStartEvent: func(context.Context, ai.OnStepStartEvent) { starts++ },
	})
	if _, err := bad.Execute(context.Background(), "hi"); err == nil || !strings.Contains(err.Error(), "toolCallConcurrency must be >= 1") {
		t.Fatalf("err = %v, want the toolCallConcurrency error", err)
	}
	if starts != 0 {
		t.Fatalf("%d start events fired, want none before the validation error", starts)
	}
	fixed := NewToolLoopAgent(AgentConfig{
		Model:               textModel,
		ToolCallConcurrency: &zero,
		PrepareCall:         func(_ context.Context, c PrepareCallConfig) PrepareCallConfig { c.ToolCallConcurrency = &two; return c },
	})
	if _, err := fixed.Execute(context.Background(), "hi"); err != nil {
		t.Fatalf("Execute with a PrepareCall-fixed concurrency: %v", err)
	}
}
