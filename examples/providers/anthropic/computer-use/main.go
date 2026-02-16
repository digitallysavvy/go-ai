package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	// Create Anthropic provider
	provider := anthropic.New(anthropic.Config{
		APIKey: apiKey,
	})

	// Get language model
	model, err := provider.LanguageModel("claude-opus-4-20250514")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Create computer use tool
	// Configure for a standard 1920x1080 display with zoom enabled
	computerTool := tools.Computer20251124(tools.Computer20251124Args{
		DisplayWidthPx:  1920,
		DisplayHeightPx: 1080,
		EnableZoom:      true,
	})

	// Generate text with computer use tool
	ctx := context.Background()
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "Take a screenshot and describe what you see on the screen."},
				},
			},
		},
		Tools: []types.Tool{computerTool},
		MaxSteps: intPtr(5),
	})

	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	// Print the response
	fmt.Printf("Response: %s\n", result.Text)

	// Print tool calls if any
	if len(result.ToolCalls) > 0 {
		fmt.Println("\nTool Calls:")
		for _, toolCall := range result.ToolCalls {
			fmt.Printf("  - %s: %v\n", toolCall.ToolName, toolCall.Arguments)
		}
	}

	// Print usage
	fmt.Printf("\nUsage: %+v\n", result.Usage)
}

func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}
