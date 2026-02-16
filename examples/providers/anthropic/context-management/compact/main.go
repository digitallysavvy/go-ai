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

	// Create model with compaction enabled
	// This will compact (summarize) the conversation when input tokens exceed 50,000
	model, err := p.LanguageModelWithOptions("claude-sonnet-4-5", &anthropic.ModelOptions{
		ContextManagement: &anthropic.ContextManagement{
			Edits: []anthropic.ContextManagementEdit{
				anthropic.NewCompactEdit().
					WithTrigger(50000). // Compact when > 50K tokens
					WithInstructions("Keep the most important context and recent conversation history"),
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Anthropic Context Management: Compact ===")
	fmt.Println()
	fmt.Println("This example demonstrates conversation compaction.")
	fmt.Println("Compaction summarizes the entire conversation history into a condensed form.")
	fmt.Println("This is the most aggressive form of context management.")
	fmt.Println()
	fmt.Println("Configuration:")
	fmt.Println("  - Trigger: When input tokens > 50,000")
	fmt.Println("  - Instructions: Keep important context and recent history")
	fmt.Println()

	// Simulate a very long conversation
	// In a real scenario, this would be a conversation approaching the context limit
	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Let's discuss the architecture of a large-scale system..."},
			},
		},
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: "Sure! Let me break down the components..."},
			},
		},
		// ... many more exchanges ...
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Given everything we've discussed, what's your final recommendation?"},
			},
		},
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Messages: messages,
		System:   "You are a helpful system architecture consultant.",
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
				switch edit.(type) {
				case *anthropic.AppliedCompactEdit:
					fmt.Println("  ✓ Conversation compacted")
					fmt.Println("    The conversation history has been summarized")
					fmt.Println("    to fit within the context window")
				}
			}
			fmt.Println()
			fmt.Println("✓ Context window optimized through compaction!")
		}
	} else {
		fmt.Println("No compaction was triggered for this conversation.")
		fmt.Println("(Trigger threshold not reached)")
	}

	// Show token usage
	// Note: When compaction occurs, usage.Iterations will contain breakdown
	// of tokens used for compaction vs. the actual message
	fmt.Printf("\nToken usage: %d total\n", result.Usage.GetTotalTokens())
	fmt.Printf("  Input: %d tokens\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output: %d tokens\n", result.Usage.GetOutputTokens())

	// Check if iterations data is available (indicates compaction occurred)
	if result.Usage.Raw != nil {
		if iterations, ok := result.Usage.Raw["iterations"].([]interface{}); ok && len(iterations) > 0 {
			fmt.Println("\n  Iterations breakdown:")
			for i, iter := range iterations {
				if iterMap, ok := iter.(map[string]interface{}); ok {
					iterType := iterMap["type"]
					inputTokens := iterMap["input_tokens"]
					outputTokens := iterMap["output_tokens"]
					fmt.Printf("    %d. %v: %v input, %v output\n", i+1, iterType, inputTokens, outputTokens)
				}
			}
		}
	}

	fmt.Println("\n(Compaction helps maintain context in extremely long conversations)")
}
