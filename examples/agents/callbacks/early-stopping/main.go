package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// This example demonstrates how to use the OnStepFinish callback
// to implement custom logic for monitoring and early stopping.
//
// Note: In the current Go implementation, OnStepFinish doesn't return
// an error to stop execution. This example shows the pattern that could
// be used for monitoring and validation.

func main() {
	ctx := context.Background()

	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Create OpenAI provider
	provider := openai.New(openai.Config{
		APIKey: apiKey,
	})
	model, err := provider.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// Define a tool that might be expensive
	expensiveTool := types.Tool{
		Name:        "expensive_api_call",
		Description: "Make an expensive API call",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The query to process",
				},
			},
			"required": []string{"query"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			query := args["query"].(string)
			fmt.Printf("⚠️  Expensive API called with: %s\n", query)
			return fmt.Sprintf("Expensive result for: %s", query), nil
		},
	}

	// Track token usage and set limits
	totalTokensUsed := int64(0)
	const TOKEN_LIMIT = 1000
	limitExceeded := false

	myAgent := agent.NewToolLoopAgent(agent.AgentConfig{
		Model:  model,
		System: "You are a helpful assistant. Try to be efficient with your responses.",
		Tools: []types.Tool{
			expensiveTool,
		},
		MaxSteps: 10,
		OnStepFinish: func(step types.StepResult) {
			// Track cumulative token usage
			if step.Usage.TotalTokens != nil {
				totalTokensUsed += *step.Usage.TotalTokens
			}

			fmt.Printf("\n--- Step %d ---\n", step.StepNumber)
			fmt.Printf("Tokens this step: %d\n", getTokens(step.Usage.TotalTokens))
			fmt.Printf("Total tokens: %d / %d\n", totalTokensUsed, TOKEN_LIMIT)

			// Check if we're approaching the limit
			if totalTokensUsed > TOKEN_LIMIT {
				if !limitExceeded {
					fmt.Printf("⚠️  WARNING: Token limit exceeded! (%d > %d)\n", totalTokensUsed, TOKEN_LIMIT)
					fmt.Printf("Consider stopping the agent or adjusting limits.\n")
					limitExceeded = true
				}
			} else if totalTokensUsed > TOKEN_LIMIT*8/10 {
				fmt.Printf("⚠️  WARNING: Approaching token limit (80%%)\n")
			}

			// Log warnings
			if len(step.Warnings) > 0 {
				fmt.Printf("Warnings detected:\n")
				for _, warning := range step.Warnings {
					fmt.Printf("  - [%s] %s\n", warning.Type, warning.Message)
				}
			}

			// Check for problematic patterns
			if len(step.ToolCalls) > 3 {
				fmt.Printf("⚠️  Multiple tool calls in one step (%d) - might be inefficient\n", len(step.ToolCalls))
			}

			// In a real implementation, you might:
			// - Store metrics in a database
			// - Send alerts when limits are approached
			// - Implement circuit breakers
			// - Track cost per step
		},
	})

	fmt.Println("Starting agent with monitoring and limits...")
	fmt.Printf("Token limit: %d\n", TOKEN_LIMIT)
	fmt.Println()

	result, err := myAgent.Execute(ctx, "Help me understand quantum computing and its applications.")
	if err != nil {
		log.Fatalf("Agent execution failed: %v", err)
	}

	fmt.Printf("\n\n=== Execution Summary ===\n")
	fmt.Printf("Completed: %v\n", !limitExceeded)
	fmt.Printf("Total steps: %d\n", len(result.Steps))
	fmt.Printf("Total tokens used: %d\n", totalTokensUsed)
	fmt.Printf("Finish reason: %s\n", result.FinishReason)

	if limitExceeded {
		fmt.Printf("\n⚠️  Execution completed but exceeded token budget\n")
		fmt.Printf("Consider: reducing scope, using cheaper models, or increasing limits\n")
	} else {
		fmt.Printf("\n✅ Execution completed within budget\n")
	}
}

func getTokens(tokens *int64) int64 {
	if tokens == nil {
		return 0
	}
	return *tokens
}
