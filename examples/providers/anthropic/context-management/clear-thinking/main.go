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

	// Create model with context management enabled (clear_thinking edit)
	// This will keep the last 2 thinking turns and clear older ones
	model, err := p.LanguageModelWithOptions("claude-sonnet-4-5", &anthropic.ModelOptions{
		ContextManagement: &anthropic.ContextManagement{
			Edits: []anthropic.ContextManagementEdit{
				anthropic.NewClearThinkingEdit().
					WithKeepRecentTurns(2), // Keep last 2 thinking turns
			},
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
	fmt.Println("Context management keeps recent conclusions while removing old thinking processes.")
	fmt.Println()
	fmt.Println("Configuration:")
	fmt.Println("  - Keep: Last 2 thinking turns")
	fmt.Println("  - Clear: Older thinking blocks")
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
		cmr, ok := result.ContextManagement.(*anthropic.ContextManagementResponse)
		if ok && len(cmr.AppliedEdits) > 0 {
			fmt.Println("Context Management Applied:")
			for _, edit := range cmr.AppliedEdits {
				switch e := edit.(type) {
				case *anthropic.AppliedClearThinkingEdit:
					fmt.Printf("  ✓ Cleared %d thinking turns\n", e.ClearedThinkingTurns)
					fmt.Printf("    Freed %d input tokens\n", e.ClearedInputTokens)
				}
			}
			fmt.Println()
			fmt.Println("✓ Context window optimized by removing old thinking blocks!")
		}
	} else {
		fmt.Println("No context management was triggered for this conversation.")
		fmt.Println("(Not enough thinking turns to trigger clearing)")
	}

	// Show token usage
	if result.Usage.TotalTokens != nil {
		fmt.Printf("\nToken usage: %d total\n", *result.Usage.TotalTokens)
		if result.Usage.InputTokens != nil {
			fmt.Printf("  Input: %d tokens\n", *result.Usage.InputTokens)
		}
		if result.Usage.OutputTokens != nil {
			fmt.Printf("  Output: %d tokens\n", *result.Usage.OutputTokens)
		}
		fmt.Println("\n(Thinking blocks can consume 10x more tokens than regular responses)")
		fmt.Println("(Context management helps manage this in long reasoning sessions)")
	}
}
