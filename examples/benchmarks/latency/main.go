package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// LatencyStats tracks latency measurements
type LatencyStats struct {
	Measurements []time.Duration
	Errors       int
}

func (ls *LatencyStats) Add(duration time.Duration) {
	ls.Measurements = append(ls.Measurements, duration)
}

func (ls *LatencyStats) AddError() {
	ls.Errors++
}

func (ls *LatencyStats) Sort() {
	sort.Slice(ls.Measurements, func(i, j int) bool {
		return ls.Measurements[i] < ls.Measurements[j]
	})
}

func (ls *LatencyStats) Min() time.Duration {
	if len(ls.Measurements) == 0 {
		return 0
	}
	ls.Sort()
	return ls.Measurements[0]
}

func (ls *LatencyStats) Max() time.Duration {
	if len(ls.Measurements) == 0 {
		return 0
	}
	ls.Sort()
	return ls.Measurements[len(ls.Measurements)-1]
}

func (ls *LatencyStats) Mean() time.Duration {
	if len(ls.Measurements) == 0 {
		return 0
	}

	var total time.Duration
	for _, d := range ls.Measurements {
		total += d
	}

	return total / time.Duration(len(ls.Measurements))
}

func (ls *LatencyStats) Median() time.Duration {
	if len(ls.Measurements) == 0 {
		return 0
	}

	ls.Sort()
	mid := len(ls.Measurements) / 2

	if len(ls.Measurements)%2 == 0 {
		return (ls.Measurements[mid-1] + ls.Measurements[mid]) / 2
	}

	return ls.Measurements[mid]
}

func (ls *LatencyStats) Percentile(p float64) time.Duration {
	if len(ls.Measurements) == 0 {
		return 0
	}

	ls.Sort()
	index := int(math.Ceil(float64(len(ls.Measurements)) * p / 100.0))
	if index >= len(ls.Measurements) {
		index = len(ls.Measurements) - 1
	}

	return ls.Measurements[index]
}

func (ls *LatencyStats) StdDev() time.Duration {
	if len(ls.Measurements) == 0 {
		return 0
	}

	mean := ls.Mean()
	var sumSquares float64

	for _, d := range ls.Measurements {
		diff := float64(d - mean)
		sumSquares += diff * diff
	}

	variance := sumSquares / float64(len(ls.Measurements))
	return time.Duration(math.Sqrt(variance))
}

func (ls *LatencyStats) Print(label string) {
	fmt.Printf("\n=== %s ===\n", label)
	fmt.Printf("Samples:      %d\n", len(ls.Measurements))
	fmt.Printf("Errors:       %d\n", ls.Errors)
	fmt.Printf("Min:          %v\n", ls.Min())
	fmt.Printf("Max:          %v\n", ls.Max())
	fmt.Printf("Mean:         %v\n", ls.Mean())
	fmt.Printf("Median:       %v\n", ls.Median())
	fmt.Printf("Std Dev:      %v\n", ls.StdDev())
	fmt.Printf("P50:          %v\n", ls.Percentile(50))
	fmt.Printf("P90:          %v\n", ls.Percentile(90))
	fmt.Printf("P95:          %v\n", ls.Percentile(95))
	fmt.Printf("P99:          %v\n", ls.Percentile(99))
}

func measureLatency(ctx context.Context, model provider.LanguageModel, prompt string, iterations int) *LatencyStats {
	stats := &LatencyStats{}

	fmt.Printf("Running %d iterations...\n", iterations)

	for i := 0; i < iterations; i++ {
		start := time.Now()

		_, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model:  model,
			Prompt: prompt,
		})

		duration := time.Since(start)

		if err != nil {
			stats.AddError()
			fmt.Printf("[%d] Error: %v\n", i+1, err)
		} else {
			stats.Add(duration)
			if (i+1)%5 == 0 {
				fmt.Printf("[%d] %.2fs\n", i+1, duration.Seconds())
			}
		}

		// Small delay to avoid rate limiting
		time.Sleep(100 * time.Millisecond)
	}

	return stats
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, _ := p.LanguageModel("gpt-4")

	ctx := context.Background()

	fmt.Println("=== Latency Benchmarks ===")

	// Test 1: Short prompt
	fmt.Println("--- Test 1: Short Prompt ---")
	shortPrompt := "Say 'hello' once."
	stats1 := measureLatency(ctx, model, shortPrompt, 10)
	stats1.Print("Short Prompt Latency")

	// Test 2: Medium prompt
	fmt.Println("\n\n--- Test 2: Medium Prompt ---")
	mediumPrompt := "Explain what Go programming language is in 2-3 sentences."
	stats2 := measureLatency(ctx, model, mediumPrompt, 10)
	stats2.Print("Medium Prompt Latency")

	// Test 3: Long prompt
	fmt.Println("\n\n--- Test 3: Long Prompt ---")
	longPrompt := `Given the following code snippet, explain what it does, identify any potential issues, and suggest improvements:

func processData(data []int) []int {
	var result []int
	for i := 0; i < len(data); i++ {
		if data[i] % 2 == 0 {
			result = append(result, data[i] * 2)
		}
	}
	return result
}

Provide a detailed analysis.`
	stats3 := measureLatency(ctx, model, longPrompt, 10)
	stats3.Print("Long Prompt Latency")

	// Comparison
	fmt.Println("\n\n=== Latency Comparison ===")
	fmt.Printf("%-20s %10s %10s %10s %10s\n", "Test", "Min", "Mean", "P95", "P99")
	fmt.Println(string(make([]byte, 70)))

	printRow := func(label string, stats *LatencyStats) {
		fmt.Printf("%-20s %10v %10v %10v %10v\n",
			label,
			stats.Min().Round(time.Millisecond),
			stats.Mean().Round(time.Millisecond),
			stats.Percentile(95).Round(time.Millisecond),
			stats.Percentile(99).Round(time.Millisecond))
	}

	printRow("Short Prompt", stats1)
	printRow("Medium Prompt", stats2)
	printRow("Long Prompt", stats3)

	// Latency distribution
	fmt.Println("\n\n=== Latency Distribution (P95) ===")
	drawHistogram := func(label string, stats *LatencyStats) {
		p95 := stats.Percentile(95)
		bars := int(p95.Seconds() * 10) // Scale for visualization
		if bars > 50 {
			bars = 50
		}

		fmt.Printf("%-20s ", label)
		for i := 0; i < bars; i++ {
			fmt.Print("█")
		}
		fmt.Printf(" %.2fs\n", p95.Seconds())
	}

	drawHistogram("Short", stats1)
	drawHistogram("Medium", stats2)
	drawHistogram("Long", stats3)

	// Insights
	fmt.Println("\n\n=== Insights ===")
	fmt.Println("• Longer prompts → higher latency")
	fmt.Println("• P95/P99 show tail latency (important for UX)")
	fmt.Println("• Std dev indicates consistency")
	fmt.Println("• First-token latency vs total latency")

	fmt.Println("\n=== Latency Optimization Tips ===")
	tips := []string{
		"✓ Use streaming for perceived lower latency",
		"✓ Cache common requests",
		"✓ Optimize prompt length",
		"✓ Use faster models when appropriate (e.g., gpt-3.5-turbo)",
		"✓ Set appropriate max_tokens",
		"✓ Implement timeout and retry logic",
		"✓ Consider geographic proximity to API",
		"✓ Monitor and alert on P99 latency",
	}
	for _, tip := range tips {
		fmt.Println("  " + tip)
	}

	fmt.Println("\n=== Latency SLOs (Example) ===")
	fmt.Println("  • P50 latency: < 2s")
	fmt.Println("  • P95 latency: < 5s")
	fmt.Println("  • P99 latency: < 10s")
	fmt.Println("  • Error rate: < 0.1%")

	fmt.Println("\n=== What Affects Latency? ===")
	factors := map[string]string{
		"Prompt length":   "Longer prompts take longer to process",
		"Response length": "More tokens → more time",
		"Model size":      "Larger models have higher latency",
		"Network latency": "Distance to API servers matters",
		"API load":        "Peak times may have higher latency",
		"Rate limiting":   "Throttling adds wait time",
	}

	for factor, description := range factors {
		fmt.Printf("  • %-20s: %s\n", factor, description)
	}
}
