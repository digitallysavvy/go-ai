package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// ThroughputBenchmark measures requests per second
type ThroughputBenchmark struct {
	model         provider.LanguageModel
	concurrency   int
	duration      time.Duration
	requestCount  atomic.Int64
	successCount  atomic.Int64
	errorCount    atomic.Int64
	totalTokens   atomic.Int64
	totalDuration atomic.Int64
}

func NewThroughputBenchmark(model provider.LanguageModel, concurrency int, duration time.Duration) *ThroughputBenchmark {
	return &ThroughputBenchmark{
		model:       model,
		concurrency: concurrency,
		duration:    duration,
	}
}

func (tb *ThroughputBenchmark) Run(ctx context.Context) *BenchmarkResults {
	fmt.Printf("Starting throughput benchmark...\n")
	fmt.Printf("Concurrency: %d\n", tb.concurrency)
	fmt.Printf("Duration: %v\n\n", tb.duration)

	startTime := time.Now()
	deadline := startTime.Add(tb.duration)

	var wg sync.WaitGroup
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	// Start concurrent workers
	for i := 0; i < tb.concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			tb.worker(ctx, workerID)
		}(i)
	}

	// Progress reporter
	go tb.reportProgress(ctx, startTime)

	wg.Wait()
	elapsed := time.Since(startTime)

	return &BenchmarkResults{
		TotalRequests:      tb.requestCount.Load(),
		SuccessfulRequests: tb.successCount.Load(),
		FailedRequests:     tb.errorCount.Load(),
		TotalTokens:        tb.totalTokens.Load(),
		Duration:           elapsed,
		Concurrency:        tb.concurrency,
	}
}

func (tb *ThroughputBenchmark) worker(ctx context.Context, workerID int) {
	prompts := []string{
		"What is Go programming language?",
		"Explain concurrency in simple terms.",
		"How do you handle errors in Go?",
		"What are goroutines?",
		"Describe the benefits of using Go.",
	}

	promptIndex := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
			tb.requestCount.Add(1)

			prompt := prompts[promptIndex%len(prompts)]
			promptIndex++

			reqStart := time.Now()

			result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
				Model:  tb.model,
				Prompt: prompt,
			})

			reqDuration := time.Since(reqStart)
			tb.totalDuration.Add(reqDuration.Milliseconds())

			if err != nil {
				tb.errorCount.Add(1)
			} else {
				tb.successCount.Add(1)
				tb.totalTokens.Add(result.Usage.GetTotalTokens())
			}
		}
	}
}

func (tb *ThroughputBenchmark) reportProgress(ctx context.Context, startTime time.Time) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			elapsed := time.Since(startTime)
			requests := tb.requestCount.Load()
			rps := float64(requests) / elapsed.Seconds()

			fmt.Printf("[%.0fs] Requests: %d | RPS: %.2f | Success: %d | Errors: %d\n",
				elapsed.Seconds(), requests, rps, tb.successCount.Load(), tb.errorCount.Load())
		}
	}
}

// BenchmarkResults contains benchmark metrics
type BenchmarkResults struct {
	TotalRequests      int64
	SuccessfulRequests int64
	FailedRequests     int64
	TotalTokens        int64
	Duration           time.Duration
	Concurrency        int
}

func (br *BenchmarkResults) Print() {
	fmt.Println("\n" + "=== Benchmark Results ===")
	fmt.Printf("Duration:             %v\n", br.Duration)
	fmt.Printf("Concurrency:          %d\n", br.Concurrency)
	fmt.Printf("Total Requests:       %d\n", br.TotalRequests)
	fmt.Printf("Successful:           %d (%.1f%%)\n", br.SuccessfulRequests, br.SuccessRate())
	fmt.Printf("Failed:               %d (%.1f%%)\n", br.FailedRequests, br.ErrorRate())
	fmt.Printf("Requests/second:      %.2f\n", br.RequestsPerSecond())
	fmt.Printf("Total Tokens:         %d\n", br.TotalTokens)
	fmt.Printf("Tokens/second:        %.2f\n", br.TokensPerSecond())
	fmt.Printf("Tokens/request (avg): %.2f\n", br.AvgTokensPerRequest())
}

func (br *BenchmarkResults) RequestsPerSecond() float64 {
	return float64(br.TotalRequests) / br.Duration.Seconds()
}

func (br *BenchmarkResults) TokensPerSecond() float64 {
	return float64(br.TotalTokens) / br.Duration.Seconds()
}

func (br *BenchmarkResults) SuccessRate() float64 {
	if br.TotalRequests == 0 {
		return 0
	}
	return float64(br.SuccessfulRequests) / float64(br.TotalRequests) * 100
}

func (br *BenchmarkResults) ErrorRate() float64 {
	if br.TotalRequests == 0 {
		return 0
	}
	return float64(br.FailedRequests) / float64(br.TotalRequests) * 100
}

func (br *BenchmarkResults) AvgTokensPerRequest() float64 {
	if br.SuccessfulRequests == 0 {
		return 0
	}
	return float64(br.TotalTokens) / float64(br.SuccessfulRequests)
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, _ := p.LanguageModel("gpt-4")

	ctx := context.Background()

	fmt.Println("=== Throughput Benchmarks ===")

	// Benchmark 1: Low concurrency
	fmt.Println("--- Test 1: Concurrency = 1 ---")
	bench1 := NewThroughputBenchmark(model, 1, 30*time.Second)
	results1 := bench1.Run(ctx)
	results1.Print()
	fmt.Println()

	// Benchmark 2: Medium concurrency
	fmt.Println("\n--- Test 2: Concurrency = 5 ---")
	bench2 := NewThroughputBenchmark(model, 5, 30*time.Second)
	results2 := bench2.Run(ctx)
	results2.Print()
	fmt.Println()

	// Benchmark 3: High concurrency
	fmt.Println("\n--- Test 3: Concurrency = 10 ---")
	bench3 := NewThroughputBenchmark(model, 10, 30*time.Second)
	results3 := bench3.Run(ctx)
	results3.Print()
	fmt.Println()

	// Compare results
	fmt.Println("\n=== Comparison ===")
	fmt.Printf("%-15s %10s %15s %15s\n", "Concurrency", "Requests", "RPS", "Tokens/sec")
	fmt.Println(string(make([]byte, 60)))

	printComparison := func(r *BenchmarkResults) {
		fmt.Printf("%-15d %10d %15.2f %15.2f\n",
			r.Concurrency, r.TotalRequests, r.RequestsPerSecond(), r.TokensPerSecond())
	}

	printComparison(results1)
	printComparison(results2)
	printComparison(results3)

	fmt.Println("\n=== Insights ===")
	fmt.Println("• Higher concurrency → more requests/second (up to rate limits)")
	fmt.Println("• Monitor error rate at high concurrency")
	fmt.Println("• Token throughput indicates actual work completed")
	fmt.Println("• Balance concurrency with error rate and cost")

	fmt.Println("\n=== Optimization Tips ===")
	tips := []string{
		"✓ Use connection pooling",
		"✓ Implement rate limiting",
		"✓ Add retry logic with backoff",
		"✓ Monitor and adjust concurrency",
		"✓ Use streaming for long responses",
		"✓ Cache frequent requests",
		"✓ Batch similar requests",
	}
	for _, tip := range tips {
		fmt.Println("  " + tip)
	}
}
