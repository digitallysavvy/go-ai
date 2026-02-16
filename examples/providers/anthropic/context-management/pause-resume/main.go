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

	// Create model with compaction that pauses after compaction
	// This allows you to inspect the compacted history before continuing
	model, err := p.LanguageModelWithOptions("claude-sonnet-4-5", &anthropic.ModelOptions{
		ContextManagement: &anthropic.ContextManagement{
			Edits: []anthropic.ContextManagementEdit{
				anthropic.NewCompactEdit().
					WithTrigger(50000).                  // Compact at 50K tokens
					WithPauseAfterCompaction(true).      // PAUSE after compaction
					WithInstructions("Preserve key technical decisions and recent context"),
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Anthropic Context Management: Pause/Resume ===")
	fmt.Println()
	fmt.Println("This example demonstrates pausing after compaction.")
	fmt.Println("When PauseAfterCompaction is enabled:")
	fmt.Println("  1. Compaction occurs when the threshold is reached")
	fmt.Println("  2. The request pauses, returning the compacted history")
	fmt.Println("  3. You can inspect the compacted conversation")
	fmt.Println("  4. Resume with the updated history for the next turn")
	fmt.Println()
	fmt.Println("This is useful for:")
	fmt.Println("  • Verifying important context wasn't lost")
	fmt.Println("  • Debugging context management behavior")
	fmt.Println("  • Implementing custom compaction logic")
	fmt.Println()

	// Start with a long conversation
	// In practice, this would be built up over many turns
	conversation := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Let's design a microservices architecture..."},
			},
		},
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: "I'll help you design a comprehensive microservices architecture..."},
			},
		},
		// ... many more messages that would approach 50K tokens ...
	}

	fmt.Println("Step 1: Making request with long conversation...")
	fmt.Println()

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Messages: conversation,
		System:   "You are a system architecture expert.",
	})

	if err != nil {
		log.Fatal(err)
	}

	// Check if compaction occurred
	if result.ContextManagement != nil {
		cmr, ok := result.ContextManagement.(*anthropic.ContextManagementResponse)
		if ok && len(cmr.AppliedEdits) > 0 {
			for _, edit := range cmr.AppliedEdits {
				if _, ok := edit.(*anthropic.AppliedCompactEdit); ok {
					fmt.Println("✓ Compaction was triggered!")
					fmt.Println()
					fmt.Println("The conversation has been compacted to save space.")
					fmt.Println("Generation was paused after compaction.")
					fmt.Println()

					// At this point, you would typically:
					// 1. Update your conversation history with the compacted version
					// 2. Inspect what was kept/removed
					// 3. Make any adjustments if needed
					// 4. Continue the conversation with the updated history

					fmt.Println("Next steps in your application:")
					fmt.Println("  1. The compacted history is now in the conversation")
					fmt.Println("  2. Review the compaction results")
					fmt.Println("  3. Continue with the next turn using updated history")
					fmt.Println()
				}
			}
		}
	}

	fmt.Println("Step 2: Continuing conversation after compaction...")
	fmt.Println()

	// Add the assistant's response to conversation
	conversation = append(conversation, types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentPart{
			types.TextContent{Text: result.Text},
		},
	})

	// Continue the conversation with a new question
	conversation = append(conversation, types.Message{
		Role: types.RoleUser,
		Content: []types.ContentPart{
			types.TextContent{Text: "Based on everything we discussed, what's your recommendation?"},
		},
	})

	// This request will use the compacted history
	resumedResult, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Messages: conversation,
		System:   "You are a system architecture expert.",
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response after resume:")
	fmt.Println(resumedResult.Text)
	fmt.Println()

	// Show token usage
	fmt.Printf("Token usage: %d total\n", resumedResult.Usage.GetTotalTokens())
	fmt.Println()
	fmt.Println("Benefits of pause/resume:")
	fmt.Println("  ✓ Verify compaction quality")
	fmt.Println("  ✓ Adjust conversation if needed")
	fmt.Println("  ✓ Debug context management")
	fmt.Println("  ✓ Implement custom compaction logic")
	fmt.Println()
	fmt.Println("Without pause/resume:")
	fmt.Println("  • Compaction happens automatically")
	fmt.Println("  • No inspection of compacted history")
	fmt.Println("  • Simpler but less control")

	fmt.Println()
	fmt.Println("When to use PauseAfterCompaction:")
	fmt.Println("  ✓ Critical conversations where context loss is risky")
	fmt.Println("  ✓ Development and testing")
	fmt.Println("  ✓ Custom compaction workflows")
	fmt.Println("  ✗ Production workflows (adds latency)")
}
