package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway/tools"
)

func main() {
	// Create gateway provider
	provider, err := gateway.New(gateway.Config{
		APIKey: os.Getenv("AI_GATEWAY_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create gateway provider: %v", err)
	}

	// Create language model
	model, err := provider.LanguageModel("openai/gpt-4")
	if err != nil {
		log.Fatalf("Failed to create language model: %v", err)
	}

	fmt.Println("=======================================================")
	fmt.Println("Example 1: Basic Parallel Search")
	fmt.Println("=======================================================")

	// Create basic parallel search tool
	basicSearch := tools.NewParallelSearch(tools.ParallelSearchConfig{
		Mode:       tools.ParallelSearchModeBasic,
		MaxResults: intPtr(5),
	})

	result1, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Search for information about quantum computing breakthroughs in 2024.",
		Tools: []types.Tool{
			basicSearch.ToTool(),
		},
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	fmt.Println("\nResult:")
	fmt.Println(result1.Text)

	if len(result1.ToolCalls) > 0 {
		fmt.Println("\nTool calls made:")
		for _, call := range result1.ToolCalls {
			fmt.Printf("  - %s\n", call.ToolName)
			argsJSON, _ := json.MarshalIndent(call.Arguments, "    ", "  ")
			fmt.Printf("    Arguments: %s\n", string(argsJSON))
		}
	}

	fmt.Println("\n=======================================================")
	fmt.Println("Example 2: Parallel Search with Domain Filtering")
	fmt.Println("=======================================================")

	// Create parallel search with source policy
	filteredSearch := tools.NewParallelSearch(tools.ParallelSearchConfig{
		Mode:       tools.ParallelSearchModeBasic,
		MaxResults: intPtr(10),
		SourcePolicy: &tools.ParallelSearchSourcePolicy{
			IncludeDomains: []string{"wikipedia.org", "nature.com", "science.org"},
			AfterDate:      "2024-01-01",
		},
	})

	result2, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "What are the latest developments in renewable energy?",
		Tools: []types.Tool{
			filteredSearch.ToTool(),
		},
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	fmt.Println("\nResult:")
	fmt.Println(result2.Text)

	fmt.Println("\n=======================================================")
	fmt.Println("Example 3: Advanced Mode with Location and Fetch Policy")
	fmt.Println("=======================================================")

	// Create an advanced mode search with a location and fetch policy
	maxAgeSeconds := 600
	timeoutSeconds := 10
	advancedSearch := tools.NewParallelSearch(tools.ParallelSearchConfig{
		Mode:          tools.ParallelSearchModeAdvanced,
		MaxResults:    intPtr(3),
		Location:      "us",
		MaxCharsTotal: intPtr(2000),
		Excerpts: &tools.ParallelSearchExcerpts{
			MaxCharsPerResult: intPtr(500),
		},
		FetchPolicy: &tools.ParallelSearchFetchPolicy{
			MaxAgeSeconds:  &maxAgeSeconds,
			TimeoutSeconds: &timeoutSeconds,
		},
	})

	result3, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Find recent news about AI safety regulations.",
		Tools: []types.Tool{
			advancedSearch.ToTool(),
		},
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	fmt.Println("\nResult:")
	fmt.Println(result3.Text)

	fmt.Println("\n=======================================================")
	fmt.Println("Example 4: Streaming with Parallel Search")
	fmt.Println("=======================================================")

	searchTool := tools.NewParallelSearch(tools.ParallelSearchConfig{
		Mode:       tools.ParallelSearchModeBasic,
		MaxResults: intPtr(5),
	})

	stream, err := ai.StreamText(context.Background(), ai.StreamTextOptions{
		Model:  model,
		Prompt: "Search for and summarize the top 3 climate change solutions being implemented globally.",
		Tools: []types.Tool{
			searchTool.ToTool(),
		},
	})
	if err != nil {
		log.Fatalf("Failed to stream text: %v", err)
	}

	fmt.Println("\nStreaming response:")
	for chunk := range stream.Chunks() {
		fmt.Print(chunk.Text)
	}
	fmt.Println()

	if err := stream.Err(); err != nil {
		log.Fatalf("Stream error: %v", err)
	}

	fmt.Println("\nDone!")
}

func intPtr(v int) *int {
	return &v
}
