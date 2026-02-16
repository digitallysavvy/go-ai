package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Pricing constants (example rates for GPT-4o)
const (
	textInputCost  = 0.0000025 // $2.50 per 1M tokens
	imageInputCost = 0.0000075 // ~$7.50 per 1M tokens (varies by image size)
	outputCost     = 0.0000100 // $10.00 per 1M tokens
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	ctx := context.Background()
	prov := openai.New(openai.Config{APIKey: apiKey})
	model, err := prov.LanguageModel("gpt-4o")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	fmt.Println("=== Multimodal Token Tracking Example ===\n")

	// Example 1: Text-only request
	fmt.Println("1. Text-only request:")
	textOnlyResult, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "What is the capital of France?",
	})
	if err != nil {
		log.Fatal(err)
	}
	printUsageDetails("Text-only", textOnlyResult.Usage)

	// Example 2: Multimodal request (text + image)
	fmt.Println("\n2. Multimodal request (text + image):")
	multimodalResult, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "What's in this image?"},
					types.ImageContent{
						URL: "https://upload.wikimedia.org/wikipedia/commons/thumb/3/3a/Cat03.jpg/1200px-Cat03.jpg",
					},
				},
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	printUsageDetails("Multimodal", multimodalResult.Usage)

	// Example 3: Cost comparison
	fmt.Println("\n3. Cost comparison:")
	fmt.Printf("Text-only cost: $%.6f\n", calculateCost(textOnlyResult.Usage))
	fmt.Printf("Multimodal cost: $%.6f\n", calculateCost(multimodalResult.Usage))
	fmt.Printf("Difference: $%.6f\n",
		calculateCost(multimodalResult.Usage)-calculateCost(textOnlyResult.Usage))

	// Example 4: Batch processing with token tracking
	fmt.Println("\n4. Batch processing with token tracking:")
	runBatchProcessing(ctx, model)
}

func printUsageDetails(label string, usage types.Usage) {
	fmt.Printf("=== %s Usage ===\n", label)
	fmt.Printf("Total input tokens: %d\n", usage.GetInputTokens())
	fmt.Printf("Total output tokens: %d\n", usage.GetOutputTokens())

	// Check if detailed breakdown is available
	if usage.InputDetails != nil {
		if usage.InputDetails.TextTokens != nil {
			fmt.Printf("  - Text input tokens: %d\n", *usage.InputDetails.TextTokens)
		}
		if usage.InputDetails.ImageTokens != nil {
			fmt.Printf("  - Image input tokens: %d\n", *usage.InputDetails.ImageTokens)
		}
		if usage.InputDetails.CacheReadTokens != nil && *usage.InputDetails.CacheReadTokens > 0 {
			fmt.Printf("  - Cached tokens: %d\n", *usage.InputDetails.CacheReadTokens)
		}
	} else {
		fmt.Println("  (Detailed breakdown not available)")
	}

	fmt.Printf("Estimated cost: $%.6f\n", calculateCost(usage))
}

func calculateCost(usage types.Usage) float64 {
	var inputCost float64

	// Try to use detailed breakdown for accurate cost
	if usage.InputDetails != nil &&
		usage.InputDetails.TextTokens != nil &&
		usage.InputDetails.ImageTokens != nil {
		// Accurate multimodal pricing
		inputCost = float64(*usage.InputDetails.TextTokens)*textInputCost +
			float64(*usage.InputDetails.ImageTokens)*imageInputCost
	} else {
		// Fallback to average rate if breakdown not available
		inputCost = float64(usage.GetInputTokens()) * textInputCost
	}

	outputCostTotal := float64(usage.GetOutputTokens()) * outputCost
	return inputCost + outputCostTotal
}

func runBatchProcessing(ctx context.Context, model provider.LanguageModel) {
	tasks := []string{
		"Summarize: AI is transforming industries",
		"Explain quantum computing in simple terms",
		"What are the benefits of cloud computing?",
	}

	var totalUsage types.Usage
	var totalCost float64

	for i, task := range tasks {
		result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model:  model,
			Prompt: task,
		})
		if err != nil {
			log.Printf("Error on task %d: %v", i+1, err)
			continue
		}

		cost := calculateCost(result.Usage)
		totalCost += cost

		fmt.Printf("Task %d: %d tokens, $%.6f\n",
			i+1, result.Usage.GetTotalTokens(), cost)

		totalUsage = totalUsage.Add(result.Usage)
	}

	fmt.Printf("\nBatch Summary:\n")
	fmt.Printf("Total tokens: %d\n", totalUsage.GetTotalTokens())
	fmt.Printf("Total cost: $%.6f\n", totalCost)

	// Show text vs image breakdown if available
	if totalUsage.InputDetails != nil &&
		totalUsage.InputDetails.TextTokens != nil &&
		totalUsage.InputDetails.ImageTokens != nil {
		fmt.Printf("Text input tokens: %d\n", *totalUsage.InputDetails.TextTokens)
		fmt.Printf("Image input tokens: %d\n", *totalUsage.InputDetails.ImageTokens)
	}
}

func ptr(s string) *string {
	return &s
}
