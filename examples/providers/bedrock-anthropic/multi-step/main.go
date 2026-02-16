package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	bedrockAnthropic "github.com/digitallysavvy/go-ai/pkg/providers/bedrock/anthropic"
)

func main() {
	ctx := context.Background()

	// Create Bedrock Anthropic provider
	provider := bedrockAnthropic.New(bedrockAnthropic.Config{
		Region: "us-east-1",
		Credentials: &bedrockAnthropic.AWSCredentials{
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		},
	})

	// Get language model
	model, err := provider.LanguageModel("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Define tools
	tools := []types.Tool{
		{
			Name:        "search_web",
			Description: "Search the web for information",
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
				return map[string]interface{}{
					"results": []string{
						fmt.Sprintf("Result 1 for '%s'", query),
						fmt.Sprintf("Result 2 for '%s'", query),
					},
				}, nil
			},
		},
		{
			Name:        "calculate",
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
				// Mock calculation
				return map[string]interface{}{
					"result": 42,
				}, nil
			},
		},
	}

	// Use GenerateText with MaxSteps for automatic multi-turn tool calling
	// This is the recommended approach - the SDK handles the tool loop automatically
	maxSteps := 5
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "What's the population of Tokyo, and what's that number divided by 1000?"},
				},
			},
		},
		Tools:    tools,
		MaxSteps: &maxSteps,
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	// Display final result
	fmt.Printf("\nFinal response: %s\n", result.Text)
	fmt.Printf("Total steps: %d\n", len(result.Steps))
	fmt.Printf("Total token usage: %d\n", result.Usage.GetTotalTokens())

	// Display step-by-step details
	fmt.Println("\n=== Step Details ===")
	for i, step := range result.Steps {
		fmt.Printf("\nStep %d:\n", i+1)
		if step.Text != "" {
			fmt.Printf("  Text: %s\n", step.Text)
		}
		if len(step.ToolCalls) > 0 {
			fmt.Printf("  Tool Calls: %d\n", len(step.ToolCalls))
			for _, tc := range step.ToolCalls {
				fmt.Printf("    - %s\n", tc.ToolName)
				argsJSON, _ := json.MarshalIndent(tc.Arguments, "      ", "  ")
				fmt.Printf("      Args: %s\n", string(argsJSON))
			}
		}
		if len(step.ToolResults) > 0 {
			fmt.Printf("  Tool Results: %d\n", len(step.ToolResults))
			for _, tr := range step.ToolResults {
				resultJSON, _ := json.MarshalIndent(tr.Result, "      ", "  ")
				fmt.Printf("    - %s: %s\n", tr.ToolName, string(resultJSON))
			}
		}
	}
}
