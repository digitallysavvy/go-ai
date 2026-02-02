package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
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

	// Create model with BOTH context management strategies enabled
	// This provides maximum context window optimization
	model, err := p.LanguageModelWithOptions("claude-sonnet-4", &anthropic.ModelOptions{
		ContextManagement: &anthropic.ContextManagement{
			Strategies: []string{
				anthropic.StrategyClearToolUses,
				anthropic.StrategyClearThinking,
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Anthropic Context Management: Multiple Strategies ===")
	fmt.Println()
	fmt.Println("This example uses BOTH cleanup strategies:")
	fmt.Println("  1. clear_tool_uses: Removes old tool call/result pairs")
	fmt.Println("  2. clear_thinking: Removes extended thinking blocks")
	fmt.Println()
	fmt.Println("Best for: Very long agentic conversations with reasoning")
	fmt.Println()

	// Example: Complex research task with tools and thinking
	analysisResult, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: `Conduct a comprehensive competitive analysis of the top 3 cloud providers.
Use available tools to gather data and think carefully about the implications.`,
		System: "You are a tech industry analyst. Use tools to gather information and think deeply about your analysis.",
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Analysis Result:")
	fmt.Println(analysisResult.Text[:200] + "...") // Show first 200 chars
	fmt.Println()

	// Show detailed context management results
	if analysisResult.ContextManagement != nil {
		cm, ok := analysisResult.ContextManagement.(*anthropic.ContextManagementResponse)
		if ok {
			fmt.Println("Context Management Results:")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

			totalCleanedItems := cm.ToolUsesCleared + cm.ThinkingBlocksCleared
			if totalCleanedItems > 0 {
				fmt.Printf("✓ Cleaned %d items from context\n", totalCleanedItems)
				fmt.Println()
			}

			if cm.MessagesDeleted > 0 {
				fmt.Printf("  • Deleted %d complete messages\n", cm.MessagesDeleted)
			}
			if cm.MessagesTruncated > 0 {
				fmt.Printf("  • Truncated %d messages\n", cm.MessagesTruncated)
			}
			if cm.ToolUsesCleared > 0 {
				fmt.Printf("  • Cleared %d tool uses\n", cm.ToolUsesCleared)
			}
			if cm.ThinkingBlocksCleared > 0 {
				fmt.Printf("  • Cleared %d thinking blocks\n", cm.ThinkingBlocksCleared)
			}

			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println()
		}
	} else {
		fmt.Println("ℹ No context cleanup was needed yet.")
		fmt.Println("(Context management activates automatically when needed)")
	}

	// Token usage comparison
	if analysisResult.Usage.TotalTokens != nil {
		fmt.Printf("Token Usage: %d tokens\n", *analysisResult.Usage.TotalTokens)
		fmt.Println()
		fmt.Println("Without context management, long conversations can:")
		fmt.Println("  • Hit context limits (200K+ tokens)")
		fmt.Println("  • Become expensive ($$$)")
		fmt.Println("  • Slow down (more context = slower responses)")
		fmt.Println()
		fmt.Println("With context management:")
		fmt.Println("  • Stay within limits automatically")
		fmt.Println("  • Save on token costs")
		fmt.Println("  • Maintain fast responses")
	}

	fmt.Println()
	fmt.Println("Strategy Selection Guide:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("clear_tool_uses ONLY:")
	fmt.Println("  ✓ Agentic workflows with many tools")
	fmt.Println("  ✓ Multi-turn conversations")
	fmt.Println("  ✓ Keep assistant reasoning, remove tool noise")
	fmt.Println()
	fmt.Println("clear_thinking ONLY:")
	fmt.Println("  ✓ Complex analysis tasks")
	fmt.Println("  ✓ Long-form reasoning")
	fmt.Println("  ✓ Keep conclusions, remove process")
	fmt.Println()
	fmt.Println("BOTH (like this example):")
	fmt.Println("  ✓ Very long conversations")
	fmt.Println("  ✓ Agentic + reasoning workflows")
	fmt.Println("  ✓ Maximum context savings")
}
