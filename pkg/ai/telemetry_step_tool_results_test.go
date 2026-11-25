package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type stepEndRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu    sync.Mutex
	steps []telemetry.TelemetryStepEndEvent
}

func (r *stepEndRecorder) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) {
	r.mu.Lock()
	r.steps = append(r.steps, e)
	r.mu.Unlock()
}

func stepToolResultsTool() types.Tool {
	return types.Tool{
		Name: "get_weather",
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		},
	}
}

func assertStepToolResults(t *testing.T, rec *stepEndRecorder) {
	t.Helper()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.steps) < 2 {
		t.Fatalf("got %d step-end events, want 2", len(rec.steps))
	}
	first := rec.steps[0]
	if len(first.ToolResults) != 1 || first.ToolResults[0].ToolCallID != "call_1" || first.ToolResults[0].Result != "sunny" {
		t.Fatalf("step 0 ToolResults = %+v", first.ToolResults)
	}
	if len(rec.steps[1].ToolResults) != 0 {
		t.Fatalf("step 1 ToolResults = %+v, want none", rec.steps[1].ToolResults)
	}
}

func TestGenerateTextStepEndTelemetryCarriesToolResults(t *testing.T) {
	rec := &stepEndRecorder{}
	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return &types.GenerateResult{
					ToolCalls:    []types.ToolCall{{ID: "call_1", ToolName: "get_weather", Arguments: map[string]interface{}{}}},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "w?", Tools: []types.Tool{stepToolResultsTool()},
		StopWhen:  []StopCondition{IsLoopFinished()},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStepToolResults(t, rec)
}

func TestStreamTextStepEndTelemetryCarriesToolResults(t *testing.T) {
	rec := &stepEndRecorder{}
	n := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			n++
			if n == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call_1", ToolName: "get_weather", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	s, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "w?", Tools: []types.Tool{stepToolResultsTool()},
		StopWhen:  []StopCondition{IsLoopFinished()},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadAll(); err != nil {
		t.Fatal(err)
	}
	assertStepToolResults(t, rec)
}
