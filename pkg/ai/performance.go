package ai

import (
	"math"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

func calculateTokensPerSecond(outputTokens *int64, responseTimeMs int64) float64 {
	if responseTimeMs == 0 {
		return 0
	}
	var tokens int64
	if outputTokens != nil {
		tokens = *outputTokens
	}
	tokensPerSecond := (1000 * float64(tokens)) / float64(responseTimeMs)
	if math.IsInf(tokensPerSecond, 0) || math.IsNaN(tokensPerSecond) {
		return 0
	}
	return tokensPerSecond
}

func stepPerformance(start time.Time, usage types.Usage, firstTokenAt *time.Time) types.StepPerformance {
	responseTimeMs := time.Since(start).Milliseconds()
	var timeToFirstTokenMs *int64
	if firstTokenAt != nil {
		value := firstTokenAt.Sub(start).Milliseconds()
		timeToFirstTokenMs = &value
	}
	return types.StepPerformance{
		StepTimeMs:         responseTimeMs,
		ResponseTimeMs:     responseTimeMs,
		ToolExecutionMs:    map[string]int64{},
		TokensPerSecond:    calculateTokensPerSecond(usage.OutputTokens, responseTimeMs),
		TimeToFirstTokenMs: timeToFirstTokenMs,
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
		ResponseTimeMs:     performance.ResponseTimeMs,
		TokensPerSecond:    performance.TokensPerSecond,
		TimeToFirstTokenMs: performance.TimeToFirstTokenMs,
	}
}
