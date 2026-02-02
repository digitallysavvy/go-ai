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
// to track agent execution step by step.

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

	// Define some tools for the agent
	searchTool := types.Tool{
		Name:        "search",
		Description: "Search for information",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query",
				},
			},
			"required": []string{"query"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			query := args["query"].(string)
			// Simulate search
			return fmt.Sprintf("Search results for: %s - Found 3 relevant articles about %s", query, query), nil
		},
	}

	calculatorTool := types.Tool{
		Name:        "calculator",
		Description: "Perform mathematical calculations",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"expression": map[string]interface{}{
					"type":        "string",
					"description": "The mathematical expression to evaluate",
				},
			},
			"required": []string{"expression"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			expression := args["expression"].(string)
			// Simulate calculation
			return fmt.Sprintf("Result of %s = 42", expression), nil
		},
	}

	// Track agent execution with OnStepFinish
	stepCount := 0

	myAgent := agent.NewToolLoopAgent(agent.AgentConfig{
		Model:  model,
		System: "You are a helpful assistant with access to search and calculator tools.",
		Tools: []types.Tool{
			searchTool,
			calculatorTool,
		},
		MaxSteps: 5,
		OnStepFinish: func(step types.StepResult) {
			stepCount++
			fmt.Printf("\n=== Step %d Completed ===\n", stepCount)
			fmt.Printf("Tool calls: %d\n", len(step.ToolCalls))
			if step.Usage.TotalTokens != nil {
				fmt.Printf("Tokens used: %d\n", *step.Usage.TotalTokens)
			}
			fmt.Printf("Finish reason: %s\n", step.FinishReason)

			if len(step.Warnings) > 0 {
				fmt.Printf("Warnings:\n")
				for _, warning := range step.Warnings {
					fmt.Printf("  - [%s] %s\n", warning.Type, warning.Message)
				}
			}

			if step.Text != "" {
				fmt.Printf("Text: %s\n", step.Text)
			}

			if len(step.ToolCalls) > 0 {
				fmt.Printf("Tools called:\n")
				for _, tc := range step.ToolCalls {
					fmt.Printf("  - %s\n", tc.ToolName)
				}
			}
			fmt.Println("=======================")
		},
	})

	fmt.Println("Agent starting...")
	fmt.Println("Question: What is the population of Tokyo and calculate its square root?")
	fmt.Println()

	result, err := myAgent.Execute(ctx, "What is the population of Tokyo and calculate its square root?")
	if err != nil {
		log.Fatalf("Agent execution failed: %v", err)
	}

	fmt.Printf("\n\n=== Final Result ===\n")
	fmt.Printf("Text: %s\n", result.Text)
	fmt.Printf("Total steps: %d\n", stepCount)
	fmt.Printf("Total tokens: %d\n", *result.Usage.TotalTokens)
	fmt.Printf("Finish reason: %s\n", result.FinishReason)
}
