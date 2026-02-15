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

	// Create model with ALL context management edit types enabled
	// This provides maximum context window optimization
	model, err := p.LanguageModelWithOptions("claude-sonnet-4-5", &anthropic.ModelOptions{
		ContextManagement: &anthropic.ContextManagement{
			Edits: []anthropic.ContextManagementEdit{
				// Clear tool uses after 10K tokens, keep last 3
				anthropic.NewClearToolUsesEdit().
					WithInputTokensTrigger(10000).
					WithKeepToolUses(3),

				// Clear thinking blocks, keep last 2 turns
				anthropic.NewClearThinkingEdit().
					WithKeepRecentTurns(2),

				// Compact conversation at 50K tokens
				anthropic.NewCompactEdit().
					WithTrigger(50000).
					WithInstructions("Keep recent context and important decisions"),
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Anthropic Context Management: Multiple Edit Types ===")
	fmt.Println()
	fmt.Println("This example uses ALL THREE edit types:")
	fmt.Println("  1. clear_tool_uses: Removes old tool calls (trigger: 10K tokens, keep: 3)")
	fmt.Println("  2. clear_thinking: Removes thinking blocks (keep: last 2 turns)")
	fmt.Println("  3. compact: Summarizes conversation (trigger: 50K tokens)")
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
	fmt.Println(analysisResult.Text[:min(200, len(analysisResult.Text))] + "...") // Show first 200 chars
	fmt.Println()

	// Show detailed context management results
	if analysisResult.ContextManagement != nil {
		cmr, ok := analysisResult.ContextManagement.(*anthropic.ContextManagementResponse)
		if ok && len(cmr.AppliedEdits) > 0 {
			fmt.Println("Context Management Applied:")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println()

			for _, edit := range cmr.AppliedEdits {
				switch e := edit.(type) {
				case *anthropic.AppliedClearToolUsesEdit:
					fmt.Printf("✓ Cleared %d tool uses\n", e.ClearedToolUses)
					fmt.Printf("  Freed %d input tokens\n", e.ClearedInputTokens)
					fmt.Println()

				case *anthropic.AppliedClearThinkingEdit:
					fmt.Printf("✓ Cleared %d thinking turns\n", e.ClearedThinkingTurns)
					fmt.Printf("  Freed %d input tokens\n", e.ClearedInputTokens)
					fmt.Println()

				case *anthropic.AppliedCompactEdit:
					fmt.Println("✓ Conversation compacted")
					fmt.Println("  The conversation history has been summarized")
					fmt.Println()
				}
			}

			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println()
		}
	} else {
		fmt.Println("ℹ No context management was triggered yet.")
		fmt.Println("(Context management activates automatically when thresholds are reached)")
	}

	// Token usage comparison
	if analysisResult.Usage.TotalTokens != nil {
		fmt.Printf("Token Usage: %d tokens\n", *analysisResult.Usage.TotalTokens)
		if analysisResult.Usage.InputTokens != nil {
			fmt.Printf("  Input: %d tokens\n", *analysisResult.Usage.InputTokens)
		}
		if analysisResult.Usage.OutputTokens != nil {
			fmt.Printf("  Output: %d tokens\n", *analysisResult.Usage.OutputTokens)
		}
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
	fmt.Println("Edit Type Selection Guide:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("ClearToolUses:")
	fmt.Println("  ✓ Agentic workflows with many tools")
	fmt.Println("  ✓ Multi-turn conversations")
	fmt.Println("  ✓ Keep assistant reasoning, remove tool details")
	fmt.Println()
	fmt.Println("ClearThinking:")
	fmt.Println("  ✓ Complex analysis tasks")
	fmt.Println("  ✓ Long-form reasoning")
	fmt.Println("  ✓ Keep conclusions, remove thinking process")
	fmt.Println()
	fmt.Println("Compact:")
	fmt.Println("  ✓ Extremely long conversations")
	fmt.Println("  ✓ Approaching context limits")
	fmt.Println("  ✓ Maximum space savings (most aggressive)")
	fmt.Println()
	fmt.Println("ALL THREE (like this example):")
	fmt.Println("  ✓ Very long agentic + reasoning workflows")
	fmt.Println("  ✓ Maximum context management")
	fmt.Println("  ✓ Progressive optimization (clear → compact)")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
