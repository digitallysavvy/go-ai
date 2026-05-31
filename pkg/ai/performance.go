package ai

import (
	"math"
	"sort"
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

func stepPerformance(start time.Time, usage types.Usage, firstTokenAt *time.Time, outputChunkGapsMs []int64) types.StepPerformance {
	responseTimeMs := time.Since(start).Milliseconds()
	var timeToFirstOutputMs *int64
	var timeBetweenOutputChunksMs *types.OutputChunkTimingStats
	var outputTokensPerSecond *float64
	var inputTokensPerSecond *float64
	if firstTokenAt != nil {
		value := firstTokenAt.Sub(start).Milliseconds()
		timeToFirstOutputMs = &value
		input := calculateTokensPerSecond(usage.InputTokens, value)
		inputTokensPerSecond = &input
		outputStreamMs := responseTimeMs - value
		output := calculateTokensPerSecond(usage.OutputTokens, outputStreamMs)
		outputTokensPerSecond = &output
	}
	timeBetweenOutputChunksMs = calculateOutputChunkTimingStats(outputChunkGapsMs)
	return types.StepPerformance{
		StepTimeMs:                     responseTimeMs,
		ResponseTimeMs:                 responseTimeMs,
		ToolExecutionMs:                map[string]int64{},
		EffectiveOutputTokensPerSecond: calculateTokensPerSecond(usage.OutputTokens, responseTimeMs),
		OutputTokensPerSecond:          outputTokensPerSecond,
		InputTokensPerSecond:           inputTokensPerSecond,
		EffectiveTotalTokensPerSecond:  calculateTokensPerSecond(sumTokenCounts(usage.InputTokens, usage.OutputTokens), responseTimeMs),
		TimeToFirstOutputMs:            timeToFirstOutputMs,
		TimeBetweenOutputChunksMs:      timeBetweenOutputChunksMs,
	}
}

func calculateOutputChunkTimingStats(timingsMs []int64) *types.OutputChunkTimingStats {
	if len(timingsMs) == 0 {
		return nil
	}
	sorted := append([]int64(nil), timingsMs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum int64
	for _, timing := range timingsMs {
		sum += timing
	}
	return &types.OutputChunkTimingStats{
		Min:    sorted[0],
		P10:    nearestRankPercentile(sorted, 0.1),
		Median: nearestRankPercentile(sorted, 0.5),
		Avg:    float64(sum) / float64(len(timingsMs)),
		P90:    nearestRankPercentile(sorted, 0.9),
		Max:    sorted[len(sorted)-1],
	}
}

func nearestRankPercentile(sorted []int64, percentile float64) int64 {
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
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
		TimeToFirstOutputMs:            performance.TimeToFirstOutputMs,
		TimeBetweenOutputChunksMs:      performance.TimeBetweenOutputChunksMs,
	}
}
