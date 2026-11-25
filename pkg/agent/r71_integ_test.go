package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// r71integAgentToolErrors returns the tool-error parts of every step: since
// the owner's r71 ruling (TS toolResults = tool-result parts only), a failed
// tool call is reported there and not in ToolResults.
func r71integAgentToolErrors(steps []types.StepResult) []types.ToolErrorContent {
	var out []types.ToolErrorContent
	for _, step := range steps {
		for _, part := range step.Content {
			switch p := part.(type) {
			case types.ToolErrorContent:
				out = append(out, p)
			case *types.ToolErrorContent:
				if p != nil {
					out = append(out, *p)
				}
			}
		}
	}
	return out
}

type r71integAgentTelemetry struct {
	telemetry.NoopTelemetryIntegration
	mu    sync.Mutex
	steps [][]types.ToolResult
}

func (r *r71integAgentTelemetry) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, e.ToolResults)
}

// Owner ruling r71 (gen CR-C) and gen CR-B in the legacy loop: a provider
// isError result is a tool-error content part (TS convertLanguageModelContent)
// and, like a failed local call, not one of the step's or the run's
// ToolResults; the telemetry step end keeps both failures.
func TestR71Integ_LegacyLoopToolResultsExcludeFailures(t *testing.T) {
	mock := &mockLanguageModel{responses: []types.GenerateResult{{
		Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "p-1", ToolName: "web_search", Input: `{"query":"go"}`, ProviderExecuted: true},
			types.ToolResultContent{ToolCallID: "p-1", ToolName: "web_search", ProviderExecuted: true, IsError: r71integAgentTrue(),
				Result: map[string]interface{}{"type": "web_search_tool_result_error", "errorCode": "unavailable"}},
		},
		ToolCalls: []types.ToolCall{
			{ID: "p-1", ToolName: "web_search", Arguments: map[string]interface{}{"query": "go"}, ProviderExecuted: true},
			{ID: "c-ok", ToolName: "ok", Arguments: map[string]interface{}{}},
			{ID: "c-bad", ToolName: "bad", Arguments: map[string]interface{}{}},
		},
		FinishReason: types.FinishReasonToolCalls,
	}, {Text: "done", FinishReason: types.FinishReasonStop}}}
	tel := &r71integAgentTelemetry{}
	a := NewToolLoopAgent(AgentConfig{Model: mock, MaxSteps: 2,
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{tel}},
		Tools: []types.Tool{
			{Name: "web_search", ProviderExecuted: true, Type: types.ToolTypeProviderDefined},
			{Name: "ok", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "fine", nil
			}},
			{Name: "bad", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return nil, errors.New("boom")
			}},
		}})
	res, err := a.Execute(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	for name, results := range map[string][]types.ToolResult{"result": res.ToolResults, "step": res.Steps[0].ToolResults, "static": res.Steps[0].StaticToolResults} {
		if len(results) != 1 || results[0].ToolCallID != "c-ok" {
			t.Errorf("%s tool results = %+v, want only c-ok", name, results)
		}
	}
	toolErrors := r71integAgentToolErrors(res.Steps[:1])
	if len(toolErrors) != 2 {
		t.Fatalf("tool errors = %+v, want the provider and the local failure", toolErrors)
	}
	if got, _ := json.Marshal(toolErrors[0].Error); toolErrors[0].ToolCallID != "p-1" || !toolErrors[0].ProviderExecuted || string(got) != `{"errorCode":"unavailable","type":"web_search_tool_result_error"}` {
		t.Errorf("provider tool error = %+v (%s)", toolErrors[0], got)
	}
	if toolErrors[1].ToolCallID != "c-bad" {
		t.Errorf("local tool error = %+v", toolErrors[1])
	}
	tel.mu.Lock()
	defer tel.mu.Unlock()
	if len(tel.steps) < 1 || len(tel.steps[0]) != 3 {
		t.Errorf("telemetry step end tool results = %+v, want all three", tel.steps)
	}
}

func r71integAgentTrue() *bool { b := true; return &b }
