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

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	p := anthropic.New(anthropic.Config{
		APIKey: apiKey,
	})

	// Create model with context management enabled (clear_thinking strategy)
	model, err := p.LanguageModelWithOptions("claude-sonnet-4", &anthropic.ModelOptions{
		ContextManagement: &anthropic.ContextManagement{
			Strategies: []string{anthropic.StrategyClearThinking},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Anthropic Context Management: Clear Thinking ===")
	fmt.Println()
	fmt.Println("This example demonstrates automatic cleanup of thinking blocks.")
	fmt.Println("Extended thinking can consume significant context window space.")
	fmt.Println("Context management keeps final conclusions while removing thinking process.")
	fmt.Println()

	// Simulate a conversation with extended thinking
	// Extended thinking is used for complex reasoning tasks
	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: `Analyze this complex algorithm and explain its time complexity.
Consider edge cases and propose optimizations.`},
			},
		},
		// ... the model would respond with extensive thinking blocks ...
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Now apply similar analysis to another algorithm..."},
			},
		},
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Messages: messages,
		System:   "You are an expert algorithm analyst. Think through problems carefully.",
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response:")
	fmt.Println(result.Text)
	fmt.Println()

	// Access context management statistics
	if result.ContextManagement != nil {
		cm, ok := result.ContextManagement.(*anthropic.ContextManagementResponse)
		if ok {
			fmt.Println("Context Management Statistics:")
			fmt.Printf("  Messages deleted: %d\n", cm.MessagesDeleted)
			fmt.Printf("  Messages truncated: %d\n", cm.MessagesTruncated)
			fmt.Printf("  Tool uses cleared: %d\n", cm.ToolUsesCleared)
			fmt.Printf("  Thinking blocks cleared: %d\n", cm.ThinkingBlocksCleared)
			fmt.Println()
			if cm.ThinkingBlocksCleared > 0 {
				fmt.Println("✓ Context window optimized by removing old thinking blocks!")
			}
		}
	} else {
		fmt.Println("No context management was needed for this conversation.")
	}

	// Show token usage
	if result.Usage.TotalTokens != nil {
		fmt.Printf("\nToken usage: %d total\n", *result.Usage.TotalTokens)
		fmt.Println("(Thinking blocks can consume 10x more tokens than regular responses)")
		fmt.Println("(Context management helps manage this in long reasoning sessions)")
	}
}
