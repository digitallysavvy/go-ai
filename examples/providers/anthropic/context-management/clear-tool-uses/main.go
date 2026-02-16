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

	// Create model with context management enabled (clear_tool_uses edit)
	// This will automatically clear old tool calls when input tokens exceed 10,000
	// while keeping the 3 most recent tool uses
	model, err := p.LanguageModelWithOptions("claude-sonnet-4-5", &anthropic.ModelOptions{
		ContextManagement: &anthropic.ContextManagement{
			Edits: []anthropic.ContextManagementEdit{
				anthropic.NewClearToolUsesEdit().
					WithInputTokensTrigger(10000). // Clear when > 10K tokens
					WithKeepToolUses(3).            // Keep last 3 tool uses
					WithClearAtLeast(5000),         // Clear at least 5K tokens
			},
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
	fmt.Println("Context management automatically removes old tool calls while keeping recent ones.")
	fmt.Println()
	fmt.Println("Configuration:")
	fmt.Println("  - Trigger: When input tokens > 10,000")
	fmt.Println("  - Keep: Last 3 tool uses")
	fmt.Println("  - Clear at least: 5,000 tokens")
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

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
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
	if result.ContextManagement != nil {
		cmr, ok := result.ContextManagement.(*anthropic.ContextManagementResponse)
		if ok && len(cmr.AppliedEdits) > 0 {
			fmt.Println("Context Management Applied:")
			for _, edit := range cmr.AppliedEdits {
				switch e := edit.(type) {
				case *anthropic.AppliedClearToolUsesEdit:
					fmt.Printf("  ✓ Cleared %d tool uses\n", e.ClearedToolUses)
					fmt.Printf("    Freed %d input tokens\n", e.ClearedInputTokens)
				}
			}
			fmt.Println()
			fmt.Println("✓ Context window optimized by removing old tool calls!")
		}
	} else {
		fmt.Println("No context management was triggered for this conversation.")
		fmt.Println("(Trigger threshold not reached)")
	}

	// Show token usage
	fmt.Printf("\nToken usage: %d total\n", result.Usage.GetTotalTokens())
	fmt.Printf("  Input: %d tokens\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output: %d tokens\n", result.Usage.GetOutputTokens())
	fmt.Println("\n(Context management helps reduce token usage in long conversations)")
}
