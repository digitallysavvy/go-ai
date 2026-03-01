//go:build ignore
// +build ignore

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

// Example: Anthropic Context Editing
// Demonstrates the three context management strategies available through
// Anthropic's API: clear_tool_uses, clear_thinking, and compact.
//
// Context editing lets Claude automatically manage its conversation history
// to stay within the context window during long agentic workflows.
//
// Run with: ANTHROPIC_API_KEY=... go run context-editing.go

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	p := anthropic.New(anthropic.Config{APIKey: apiKey})

	ctx := context.Background()

	fmt.Println("=== Anthropic Context Editing Examples ===")
	fmt.Println()

	// --- Strategy 1: clear_tool_uses ---
	// Removes old tool calls and their results from the conversation history
	// when the input token count exceeds a threshold. Use this for long agentic
	// workflows where intermediate tool call results aren't needed downstream.
	fmt.Println("--- Strategy 1: clear_tool_uses ---")
	fmt.Println("Removes old tool calls once input tokens exceed 10,000.")
	fmt.Println()

	clearToolUsesModel, err := p.LanguageModelWithOptions(
		anthropic.ClaudeSonnet4_5,
		&anthropic.ModelOptions{
			ContextManagement: &anthropic.ContextManagement{
				Edits: []anthropic.ContextManagementEdit{
					anthropic.NewClearToolUsesEdit().
						WithInputTokensTrigger(10000). // Trigger at 10K input tokens
						WithKeepToolUses(3).           // Keep the 3 most recent tool uses
						WithClearAtLeast(2000),        // Clear at least 2K tokens when triggered
				},
			},
		},
	)
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	result1, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  clearToolUsesModel,
		System: "You are a helpful assistant.",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{
				types.TextContent{Text: "What is the capital of France?"},
			}},
		},
	})
	if err != nil {
		log.Printf("clear_tool_uses example failed: %v", err)
	} else {
		fmt.Printf("Response: %s\n", result1.Text)
		if result1.ContextManagement != nil {
			cmr, ok := result1.ContextManagement.(*anthropic.ContextManagementResponse)
			if ok && len(cmr.AppliedEdits) > 0 {
				for _, edit := range cmr.AppliedEdits {
					if e, ok := edit.(*anthropic.AppliedClearToolUsesEdit); ok {
						fmt.Printf("  ✓ Cleared %d tool uses (%d tokens freed)\n",
							e.ClearedToolUses, e.ClearedInputTokens)
					}
				}
			}
		}
	}
	fmt.Println()

	// --- Strategy 2: clear_thinking ---
	// Removes extended thinking blocks from conversation history.
	// Use this when your agent uses extended thinking but the thinking
	// traces don't need to be visible in subsequent turns.
	fmt.Println("--- Strategy 2: clear_thinking ---")
	fmt.Println("Removes extended thinking blocks, keeping the 2 most recent thinking turns.")
	fmt.Println()

	clearThinkingModel, err := p.LanguageModelWithOptions(
		anthropic.ClaudeOpus4_5,
		&anthropic.ModelOptions{
			ContextManagement: &anthropic.ContextManagement{
				Edits: []anthropic.ContextManagementEdit{
					anthropic.NewClearThinkingEdit().
						WithKeepRecentTurns(2), // Keep 2 most recent thinking turns
				},
			},
		},
	)
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	result2, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  clearThinkingModel,
		System: "You are a thoughtful assistant.",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{
				types.TextContent{Text: "Explain the concept of entropy in simple terms."},
			}},
		},
	})
	if err != nil {
		log.Printf("clear_thinking example failed: %v", err)
	} else {
		fmt.Printf("Response: %s\n", result2.Text)
		if result2.ContextManagement != nil {
			cmr, ok := result2.ContextManagement.(*anthropic.ContextManagementResponse)
			if ok && len(cmr.AppliedEdits) > 0 {
				for _, edit := range cmr.AppliedEdits {
					if e, ok := edit.(*anthropic.AppliedClearThinkingEdit); ok {
						fmt.Printf("  ✓ Cleared %d thinking turns (%d tokens freed)\n",
							e.ClearedThinkingTurns, e.ClearedInputTokens)
					}
				}
			}
		}
	}
	fmt.Println()

	// --- Strategy 3: compact ---
	// The most aggressive strategy — Claude summarizes older parts of the
	// conversation into a compact form when the context window fills up.
	// Use this for very long conversations that must fit within the context window.
	fmt.Println("--- Strategy 3: compact ---")
	fmt.Println("Summarizes conversation history once input tokens exceed 50,000.")
	fmt.Println()

	compactModel, err := p.LanguageModelWithOptions(
		anthropic.ClaudeSonnet4_5,
		&anthropic.ModelOptions{
			ContextManagement: &anthropic.ContextManagement{
				Edits: []anthropic.ContextManagementEdit{
					anthropic.NewCompactEdit().
						WithTrigger(50000). // Compact when input > 50K tokens
						WithInstructions("Preserve the key decisions and outcomes. Discard intermediate reasoning steps."),
				},
			},
		},
	)
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	result3, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  compactModel,
		System: "You are a helpful system architecture consultant.",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{
				types.TextContent{Text: "Summarize the trade-offs between microservices and monolithic architectures."},
			}},
		},
	})
	if err != nil {
		log.Printf("compact example failed: %v", err)
	} else {
		fmt.Printf("Response: %s\n", result3.Text)
		if result3.ContextManagement != nil {
			cmr, ok := result3.ContextManagement.(*anthropic.ContextManagementResponse)
			if ok && len(cmr.AppliedEdits) > 0 {
				for _, edit := range cmr.AppliedEdits {
					if _, ok := edit.(*anthropic.AppliedCompactEdit); ok {
						fmt.Println("  ✓ Conversation compacted")
					}
				}
			}
		}
	}
	fmt.Println()

	// --- Combined strategy ---
	// Use multiple strategies together for maximum flexibility.
	// Strategies are applied in order: clear_tool_uses first, then compact.
	fmt.Println("--- Combined: clear_tool_uses + compact ---")
	fmt.Println("First clears old tool uses, then compacts if still too large.")
	fmt.Println()

	combinedModel, err := p.LanguageModelWithOptions(
		anthropic.ClaudeSonnet4_5,
		&anthropic.ModelOptions{
			ContextManagement: &anthropic.ContextManagement{
				Edits: []anthropic.ContextManagementEdit{
					// First: clear old tool uses at 30K tokens
					anthropic.NewClearToolUsesEdit().
						WithInputTokensTrigger(30000).
						WithKeepToolUses(5),
					// Then: compact if still above 80K tokens
					anthropic.NewCompactEdit().
						WithTrigger(80000).
						WithInstructions("Keep recent tool results and key findings."),
				},
			},
		},
	)
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	result4, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  combinedModel,
		System: "You are a research assistant.",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{
				types.TextContent{Text: "What are the main benefits of Go's garbage collector compared to manual memory management?"},
			}},
		},
	})
	if err != nil {
		log.Printf("Combined example failed: %v", err)
	} else {
		fmt.Printf("Response: %s\n", result4.Text)
		printContextManagementResult(result4.ContextManagement)
	}
}

// printContextManagementResult displays applied context management edits.
func printContextManagementResult(cm interface{}) {
	if cm == nil {
		return
	}
	cmr, ok := cm.(*anthropic.ContextManagementResponse)
	if !ok || len(cmr.AppliedEdits) == 0 {
		return
	}
	fmt.Printf("  Applied %d context edit(s):\n", len(cmr.AppliedEdits))
	for _, edit := range cmr.AppliedEdits {
		switch e := edit.(type) {
		case *anthropic.AppliedClearToolUsesEdit:
			fmt.Printf("    - clear_tool_uses: %d tool uses, %d tokens freed\n",
				e.ClearedToolUses, e.ClearedInputTokens)
		case *anthropic.AppliedClearThinkingEdit:
			fmt.Printf("    - clear_thinking: %d turns, %d tokens freed\n",
				e.ClearedThinkingTurns, e.ClearedInputTokens)
		case *anthropic.AppliedCompactEdit:
			fmt.Printf("    - compact: conversation summarized (%s)\n", e.Type)
		}
	}
}
