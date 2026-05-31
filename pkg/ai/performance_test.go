package ai

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestCalculateTokensPerSecondParity(t *testing.T) {
	outputTokens := int64(10)
	if got := calculateTokensPerSecond(&outputTokens, 500); got != 20 {
		t.Fatalf("tokens/sec = %v, want 20", got)
	}
	if got := calculateTokensPerSecond(&outputTokens, 0); got != 0 {
		t.Fatalf("tokens/sec for zero response time = %v, want 0", got)
	}
	if got := calculateTokensPerSecond(nil, 500); got != 0 {
		t.Fatalf("tokens/sec for nil output tokens = %v, want 0", got)
	}
}

func TestCalculateOutputChunkTimingStatsParity(t *testing.T) {
	stats := calculateOutputChunkTimingStats([]int64{30, 10, 20, 100, 40})
	if stats == nil {
		t.Fatal("stats = nil")
	}
	if stats.Min != 10 || stats.P10 != 10 || stats.Median != 30 || stats.Avg != 40 || stats.P90 != 100 || stats.Max != 100 {
		t.Fatalf("stats = %+v", stats)
	}
	if got := calculateOutputChunkTimingStats(nil); got != nil {
		t.Fatalf("nil stats = %+v", got)
	}
}

func TestGenerateTextStepPerformance(t *testing.T) {
	outputTokens := int64(4)
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			time.Sleep(20 * time.Millisecond)
			return &types.GenerateResult{
				Text:         "ok",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{OutputTokens: &outputTokens},
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "hello",
	})
	if err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	if result.FinalStep.Performance.ResponseTimeMs <= 0 {
		t.Fatalf("responseTimeMs = %d, want > 0", result.FinalStep.Performance.ResponseTimeMs)
	}
	if result.FinalStep.Performance.StepTimeMs < result.FinalStep.Performance.ResponseTimeMs {
		t.Fatalf("stepTimeMs = %d, responseTimeMs = %d", result.FinalStep.Performance.StepTimeMs, result.FinalStep.Performance.ResponseTimeMs)
	}
	if result.FinalStep.Performance.ToolExecutionMs == nil {
		t.Fatal("toolExecutionMs = nil, want empty map")
	}
	if result.FinalStep.Performance.EffectiveOutputTokensPerSecond <= 0 {
		t.Fatalf("effectiveOutputTokensPerSecond = %v, want > 0", result.FinalStep.Performance.EffectiveOutputTokensPerSecond)
	}
	if result.FinalStep.Performance.TimeToFirstOutputMs != nil {
		t.Fatalf("generateText timeToFirstOutputMs = %v, want nil", *result.FinalStep.Performance.TimeToFirstOutputMs)
	}
	data, err := json.Marshal(result.FinalStep.Performance)
	if err != nil {
		t.Fatalf("marshal performance: %v", err)
	}
	for _, field := range []string{"stepTimeMs", "responseTimeMs", "toolExecutionMs", "effectiveOutputTokensPerSecond", "effectiveTotalTokensPerSecond"} {
		if !json.Valid(data) || !containsJSONField(data, field) {
			t.Fatalf("performance JSON missing %s: %s", field, data)
		}
	}
}

func TestGenerateTextPerformanceIncludesToolExecutionDurations(t *testing.T) {
	outputTokens := int64(1)
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{{
					ID:        "call-1",
					ToolName:  "lookup",
					Arguments: map[string]interface{}{},
				}},
				Usage: types.Usage{OutputTokens: &outputTokens},
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "hello",
		Tools: []types.Tool{{
			Name: "lookup",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				time.Sleep(time.Millisecond)
				return "ok", nil
			},
		}},
		StopWhen: []StopCondition{StepCountIs(1)},
	})
	if err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	duration, ok := result.FinalStep.Performance.ToolExecutionMs["call-1"]
	if !ok {
		t.Fatalf("toolExecutionMs missing call-1: %#v", result.FinalStep.Performance.ToolExecutionMs)
	}
	if duration < 0 {
		t.Fatalf("toolExecutionMs[call-1] = %d, want >= 0", duration)
	}
}

func containsJSONField(data []byte, field string) bool {
	var object map[string]interface{}
	if err := json.Unmarshal(data, &object); err != nil {
		return false
	}
	_, ok := object[field]
	return ok
}
