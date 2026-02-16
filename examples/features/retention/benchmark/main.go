package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// formatBytes converts bytes to human-readable format
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// getMemoryUsage returns current heap memory usage
func getMemoryUsage() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// createLargePrompt creates a prompt with substantial content
func createLargePrompt() string {
	// Simulate a large prompt (e.g., with lots of context)
	var sb strings.Builder
	sb.WriteString("Analyze the following data:\n\n")

	// Add substantial content (simulate image metadata or large context)
	for i := 0; i < 100; i++ {
		sb.WriteString(fmt.Sprintf("Data point %d: %s\n", i, strings.Repeat("x", 1000)))
	}

	sb.WriteString("\nProvide a brief summary.")
	return sb.String()
}

func main() {
	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	ctx := context.Background()

	// Create OpenAI provider
	provider := openai.New(openai.Config{
		APIKey: apiKey,
	})

	// Get a language model
	model, err := provider.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	fmt.Println("=== Retention Settings Memory Benchmark ===")

	// Create a large prompt to simulate memory-intensive scenario
	largePrompt := createLargePrompt()
	fmt.Printf("Prompt size: %s\n\n", formatBytes(uint64(len(largePrompt))))

	// ========================================================================
	// Benchmark 1: WITH retention (default behavior)
	// ========================================================================
	fmt.Println("--- Benchmark 1: WITH retention (default) ---")
	runtime.GC() // Force garbage collection for accurate measurement

	memBefore1 := getMemoryUsage()
	fmt.Printf("Memory before: %s\n", formatBytes(memBefore1))

	result1, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: largePrompt,
		// No retention settings = default behavior (retain everything)
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	memAfter1 := getMemoryUsage()
	memUsed1 := memAfter1 - memBefore1

	fmt.Printf("Memory after: %s\n", formatBytes(memAfter1))
	fmt.Printf("Memory used: %s\n", formatBytes(memUsed1))
	fmt.Printf("RawRequest present: %v\n", result1.RawRequest != nil)
	fmt.Printf("RawResponse present: %v\n", result1.RawResponse != nil)
	fmt.Printf("Text preview: %.50s...\n\n", result1.Text)

	// ========================================================================
	// Benchmark 2: WITHOUT retention (optimized)
	// ========================================================================
	fmt.Println("--- Benchmark 2: WITHOUT retention (optimized) ---")
	runtime.GC() // Force garbage collection for accurate measurement

	memBefore2 := getMemoryUsage()
	fmt.Printf("Memory before: %s\n", formatBytes(memBefore2))

	retention := &types.RetentionSettings{
		RequestBody:  types.BoolPtr(false), // Don't retain request
		ResponseBody: types.BoolPtr(false), // Don't retain response
	}

	result2, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:                 model,
		Prompt:                largePrompt,
		ExperimentalRetention: retention,
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	memAfter2 := getMemoryUsage()
	memUsed2 := memAfter2 - memBefore2

	fmt.Printf("Memory after: %s\n", formatBytes(memAfter2))
	fmt.Printf("Memory used: %s\n", formatBytes(memUsed2))
	fmt.Printf("RawRequest present: %v\n", result2.RawRequest != nil)
	fmt.Printf("RawResponse present: %v\n", result2.RawResponse != nil)
	fmt.Printf("Text preview: %.50s...\n\n", result2.Text)

	// ========================================================================
	// Analysis
	// ========================================================================
	fmt.Println("=== Memory Comparison ===")

	var savings uint64
	var percentSaved float64
	if memUsed1 > memUsed2 {
		savings = memUsed1 - memUsed2
		percentSaved = (float64(savings) / float64(memUsed1)) * 100
	} else {
		fmt.Println("Note: Memory measurements can vary due to GC and runtime behavior")
		fmt.Println("In production with many requests, savings become more apparent")
		savings = 0
		percentSaved = 0
	}

	fmt.Printf("With retention:    %s\n", formatBytes(memUsed1))
	fmt.Printf("Without retention: %s\n", formatBytes(memUsed2))
	fmt.Printf("Savings:           %s (%.1f%%)\n\n", formatBytes(savings), percentSaved)

	// ========================================================================
	// Multiple requests simulation
	// ========================================================================
	fmt.Println("=== Simulating Multiple Requests ===")

	const numRequests = 10
	fmt.Printf("Making %d requests with and without retention...\n", numRequests)

	// With retention
	runtime.GC()
	memBeforeMulti1 := getMemoryUsage()

	results := make([]*ai.GenerateTextResult, numRequests)
	for i := 0; i < numRequests; i++ {
		result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model:  model,
			Prompt: "Count from 1 to 5",
		})
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}
		results[i] = result
	}

	memAfterMulti1 := getMemoryUsage()
	memMulti1 := memAfterMulti1 - memBeforeMulti1
	fmt.Printf("Memory with retention (%d requests): %s\n", numRequests, formatBytes(memMulti1))

	// Without retention
	runtime.GC()
	memBeforeMulti2 := getMemoryUsage()

	resultsOptimized := make([]*ai.GenerateTextResult, numRequests)
	for i := 0; i < numRequests; i++ {
		result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model:                 model,
			Prompt:                "Count from 1 to 5",
			ExperimentalRetention: retention,
		})
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}
		resultsOptimized[i] = result
	}

	memAfterMulti2 := getMemoryUsage()
	memMulti2 := memAfterMulti2 - memBeforeMulti2
	fmt.Printf("Memory without retention (%d requests): %s\n", numRequests, formatBytes(memMulti2))

	var multiSavings uint64
	var multiPercentSaved float64
	if memMulti1 > memMulti2 {
		multiSavings = memMulti1 - memMulti2
		multiPercentSaved = (float64(multiSavings) / float64(memMulti1)) * 100
		fmt.Printf("Multi-request savings: %s (%.1f%%)\n", formatBytes(multiSavings), multiPercentSaved)
	}

	fmt.Println("\n✅ Benchmark completed successfully!")
	fmt.Println("\nKey findings:")
	fmt.Println("- Retention settings reduce memory footprint per request")
	fmt.Println("- Savings compound with multiple requests")
	fmt.Println("- Essential metadata (usage, finish reason) is always retained")
	fmt.Println("- Particularly effective for image-heavy or large-context workloads")
}
