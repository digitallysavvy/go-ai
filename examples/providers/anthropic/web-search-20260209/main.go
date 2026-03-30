package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	p := anthropic.New(anthropic.Config{APIKey: apiKey})

	model, err := p.LanguageModel("claude-sonnet-4-6-20251231")
	if err != nil {
		log.Fatalf("failed to get model: %v", err)
	}

	fmt.Println("=== web_search_20260209 — Basic Search ===")
	runBasicSearch(model)

	fmt.Println("\n=== web_search_20260209 — With Domain Filter and User Location ===")
	runFilteredSearch(model)
}

// runBasicSearch demonstrates a simple web search.
func runBasicSearch(model provider.LanguageModel) {
	maxUses := 3
	searchTool := tools.WebSearch20260209(tools.WebSearch20260209Config{
		MaxUses: &maxUses,
	})

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		System: "You are a helpful research assistant. Use web search to find current information.",
		Messages: []types.Message{
			{
				Role:    types.RoleUser,
				Content: []types.ContentPart{types.TextContent{Text: "What are the latest Anthropic model releases in 2026?"}},
			},
		},
		Tools:     []types.Tool{searchTool},
		MaxTokens: intPtr(1024),
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	fmt.Printf("Response: %s\n", result.Text)
	fmt.Printf("Tool calls: %d\n", len(result.ToolCalls))

	for _, tc := range result.ToolCalls {
		if tc.ToolName == "anthropic.web_search_20260209" {
			fmt.Printf("Search query: %v\n", tc.Arguments["query"])
		}
	}
}

// runFilteredSearch demonstrates web search with domain restrictions and user location.
func runFilteredSearch(model provider.LanguageModel) {
	searchTool := tools.WebSearch20260209(tools.WebSearch20260209Config{
		AllowedDomains: []string{"docs.anthropic.com", "anthropic.com"},
		UserLocation: &tools.WebSearchUserLocation{
			Type:    "approximate",
			Country: "US",
			City:    "San Francisco",
		},
	})

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model: model,
		Messages: []types.Message{
			{
				Role:    types.RoleUser,
				Content: []types.ContentPart{types.TextContent{Text: "Find information about Claude's extended thinking feature from Anthropic's documentation."}},
			},
		},
		Tools:     []types.Tool{searchTool},
		MaxTokens: intPtr(1024),
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	fmt.Printf("Response: %s\n", result.Text)

	// In a multi-turn conversation, the EncryptedContent from web search results
	// must be preserved and passed back to Anthropic for citations to work.
	// Parse tool results using tools.ParseWebSearchResults(rawJSON).
	fmt.Println("\nNote: For multi-turn citations, preserve EncryptedContent from")
	fmt.Println("WebSearchResult20260209 and include it in subsequent tool_result messages.")
	_ = result
}

func intPtr(v int) *int { return &v }
