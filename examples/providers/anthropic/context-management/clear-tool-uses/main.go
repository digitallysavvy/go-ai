package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// weatherTool is a mock weather tool for demonstration
var weatherTool = types.Tool{
	Name:        "get_weather",
	Description: "Get the current weather for a location",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]string{
				"type":        "string",
				"description": "The city and state, e.g., San Francisco, CA",
			},
		},
		"required": []string{"location"},
	},
}

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	p := anthropic.New(anthropic.Config{
		APIKey: apiKey,
	})

	// Create model with context management enabled (clear_tool_uses strategy)
	model, err := p.LanguageModelWithOptions("claude-sonnet-4", &anthropic.ModelOptions{
		ContextManagement: &anthropic.ContextManagement{
			Strategies: []string{anthropic.StrategyClearToolUses},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Anthropic Context Management: Clear Tool Uses ===")
	fmt.Println()
	fmt.Println("This example demonstrates automatic cleanup of tool use history.")
	fmt.Println("Long conversations with many tool calls can overflow the context window.")
	fmt.Println("Context management automatically removes old tool calls while keeping results.")
	fmt.Println()

	// Simulate a long conversation with many tool calls
	// In a real scenario, this would be a conversation with 20+ tool interactions
	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "What's the weather in San Francisco?"},
			},
		},
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: "I'll check the weather for you."},
			},
		},
		// ... many more tool interactions would go here ...
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Can you give me a weather summary for all the cities we discussed?"},
			},
		},
	}

	result, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model:    model,
		Messages: messages,
		Tools:    []types.Tool{weatherTool},
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response:")
	fmt.Println(result.Text)
	fmt.Println()

	// Access context management statistics
	if result.ContextManagement() != nil {
		cm, ok := result.ContextManagement().(*anthropic.ContextManagementResponse)
		if ok {
			fmt.Println("Context Management Statistics:")
			fmt.Printf("  Messages deleted: %d\n", cm.MessagesDeleted)
			fmt.Printf("  Messages truncated: %d\n", cm.MessagesTruncated)
			fmt.Printf("  Tool uses cleared: %d\n", cm.ToolUsesCleared)
			fmt.Printf("  Thinking blocks cleared: %d\n", cm.ThinkingBlocksCleared)
			fmt.Println()
			fmt.Println("✓ Context window optimized by removing old tool calls!")
		}
	} else {
		fmt.Println("No context management was needed for this conversation.")
	}

	// Show token usage
	usage := result.Usage()
	if usage.TotalTokens != nil {
		fmt.Printf("\nToken usage: %d total\n", *usage.TotalTokens)
		fmt.Println("(Context management helps reduce token usage in long conversations)")
	}
}
