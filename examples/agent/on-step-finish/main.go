package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Example demonstrating the OnStepFinish callback for monitoring multi-step agent execution
// The callback is triggered after each step in the agent's tool loop, allowing you to:
// - Track progress in real-time
// - Log intermediate results
// - Monitor token usage per step
// - Debug agent behavior
// - Build UI progress indicators

func main() {
	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable not set")
	}

	// Create OpenAI provider
	provider := openai.New(openai.Config{APIKey: apiKey})
	model, err := provider.LanguageModel("gpt-4")
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// Define a simple tool for the agent
	weatherTool := types.Tool{
		Name:        "getWeather",
		Description: "Get the current weather for a location",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "The city and state, e.g. San Francisco, CA",
				},
			},
			"required": []string{"location"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			location := args["location"].(string)
			// Simulate weather API call
			return map[string]interface{}{
				"location":    location,
				"temperature": 72,
				"conditions":  "sunny",
			}, nil
		},
	}

	// Track step metrics
	var totalSteps int
	var totalInputTokens, totalOutputTokens int64

	// Create agent with OnStepFinish callback
	agentInstance := agent.NewToolLoopAgent(agent.AgentConfig{
		Model:  model,
		System: "You are a helpful assistant that can check the weather.",
		Tools:  []types.Tool{weatherTool},

		// OnStepFinish is called after EACH step in the agent's execution
		// This allows you to monitor progress, log results, and track usage
		OnStepFinish: func(step types.StepResult) {
			totalSteps++

			// Track token usage
			if step.Usage.InputTokens != nil {
				totalInputTokens += *step.Usage.InputTokens
			}
			if step.Usage.OutputTokens != nil {
				totalOutputTokens += *step.Usage.OutputTokens
			}

			// Log step details
			fmt.Printf("\n=== Step %d Complete ===\n", step.StepNumber)
			fmt.Printf("Finish Reason: %s\n", step.FinishReason)

			// Show text output if any
			if step.Text != "" {
				fmt.Printf("Text: %s\n", step.Text)
			}

			// Show tool calls if any
			if len(step.ToolCalls) > 0 {
				fmt.Printf("Tool Calls (%d):\n", len(step.ToolCalls))
				for _, tc := range step.ToolCalls {
					fmt.Printf("  - %s(%v)\n", tc.ToolName, tc.Arguments)
				}
			}

			// Show tool results if any
			if len(step.ToolResults) > 0 {
				fmt.Printf("Tool Results (%d):\n", len(step.ToolResults))
				for _, tr := range step.ToolResults {
					fmt.Printf("  - %s: %v\n", tr.ToolName, tr.Result)
				}
			}

			// Show usage for this step
			if step.Usage.InputTokens != nil || step.Usage.OutputTokens != nil {
				fmt.Printf("Usage:\n")
				if step.Usage.InputTokens != nil {
					fmt.Printf("  Input: %d tokens\n", *step.Usage.InputTokens)
				}
				if step.Usage.OutputTokens != nil {
					fmt.Printf("  Output: %d tokens\n", *step.Usage.OutputTokens)
				}
			}

			// Show warnings if any
			if len(step.Warnings) > 0 {
				fmt.Printf("Warnings (%d):\n", len(step.Warnings))
				for _, w := range step.Warnings {
					fmt.Printf("  - %s\n", w.Message)
				}
			}
		},

		// OnFinish is called once when the agent completes
		OnFinish: func(result *agent.AgentResult) {
			fmt.Printf("\n=== Agent Execution Complete ===\n")
			fmt.Printf("Total Steps: %d\n", totalSteps)
			fmt.Printf("Total Input Tokens: %d\n", totalInputTokens)
			fmt.Printf("Total Output Tokens: %d\n", totalOutputTokens)
			fmt.Printf("Final Text: %s\n", result.Text)
		},
	})

	// Execute the agent
	ctx := context.Background()
	result, err := agentInstance.Execute(ctx, "What's the weather like in San Francisco and New York?")
	if err != nil {
		log.Fatalf("Agent execution failed: %v", err)
	}

	// Final summary
	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Agent completed successfully\n")
	fmt.Printf("Performed %d steps\n", len(result.Steps))
	fmt.Printf("Final answer: %s\n", result.Text)
}
