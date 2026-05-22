package ai

import (
	"math"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

func calculateTokensPerSecond(tokens *int64, durationMs int64) float64 {
	if durationMs == 0 {
		return 0
	}
	var tokenCount int64
	if tokens != nil {
		tokenCount = *tokens
	}
	tokensPerSecond := (1000 * float64(tokenCount)) / float64(durationMs)
	if math.IsInf(tokensPerSecond, 0) || math.IsNaN(tokensPerSecond) {
		return 0
	}
	return tokensPerSecond
}

func sumTokenCounts(a, b *int64) *int64 {
	if a == nil && b == nil {
		return nil
	}
	var total int64
	if a != nil {
		total += *a
	}
	if b != nil {
		total += *b
	}
	return &total
}

func stepPerformance(start time.Time, usage types.Usage, firstTokenAt *time.Time) types.StepPerformance {
	responseTimeMs := time.Since(start).Milliseconds()
	var timeToFirstTokenMs *int64
	var outputTokensPerSecond *float64
	var inputTokensPerSecond *float64
	if firstTokenAt != nil {
		value := firstTokenAt.Sub(start).Milliseconds()
		timeToFirstTokenMs = &value
		input := calculateTokensPerSecond(usage.InputTokens, value)
		inputTokensPerSecond = &input
		outputStreamMs := responseTimeMs - value
		output := calculateTokensPerSecond(usage.OutputTokens, outputStreamMs)
		outputTokensPerSecond = &output
	}
	return types.StepPerformance{
		StepTimeMs:                     responseTimeMs,
		ResponseTimeMs:                 responseTimeMs,
		ToolExecutionMs:                map[string]int64{},
		EffectiveOutputTokensPerSecond: calculateTokensPerSecond(usage.OutputTokens, responseTimeMs),
		OutputTokensPerSecond:          outputTokensPerSecond,
		InputTokensPerSecond:           inputTokensPerSecond,
		EffectiveTotalTokensPerSecond:  calculateTokensPerSecond(sumTokenCounts(usage.InputTokens, usage.OutputTokens), responseTimeMs),
		TimeToFirstTokenMs:             timeToFirstTokenMs,
	}
}

func finishStepPerformance(performance types.StepPerformance, stepStart time.Time, toolExecutionMs map[string]int64) types.StepPerformance {
	if toolExecutionMs == nil {
		toolExecutionMs = map[string]int64{}
	}
	performance.ToolExecutionMs = toolExecutionMs
	performance.StepTimeMs = time.Since(stepStart).Milliseconds()
	return performance
}

func languageModelCallPerformance(performance types.StepPerformance) telemetry.LanguageModelCallPerformance {
	return telemetry.LanguageModelCallPerformance{
		ResponseTimeMs:                 performance.ResponseTimeMs,
		EffectiveOutputTokensPerSecond: performance.EffectiveOutputTokensPerSecond,
		OutputTokensPerSecond:          performance.OutputTokensPerSecond,
		InputTokensPerSecond:           performance.InputTokensPerSecond,
		EffectiveTotalTokensPerSecond:  performance.EffectiveTotalTokensPerSecond,
		TimeToFirstOutputTokenMs:       performance.TimeToFirstTokenMs,
	}
}
