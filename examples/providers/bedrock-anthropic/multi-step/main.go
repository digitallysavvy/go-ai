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
			Execute: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
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
			Execute: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
				// Mock calculation
				return map[string]interface{}{
					"result": 42,
				}, nil
			},
		},
	}

	// Initial user message
	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.NewTextPart("What's the population of Tokyo, and what's that number divided by 1000?"),
			},
		},
	}

	// Multi-step agent loop
	maxSteps := 5
	for step := 0; step < maxSteps; step++ {
		fmt.Printf("\n=== Step %d ===\n", step+1)

		// Generate response
		result, err := ai.GenerateText(ctx, ai.GenerateOptions{
			Model: model,
			Prompt: types.Prompt{
				Messages: messages,
			},
			Tools: tools,
		})
		if err != nil {
			log.Fatalf("Failed to generate text: %v", err)
		}

		// Add assistant response to messages
		assistantContent := []types.ContentPart{}
		if result.Text != "" {
			fmt.Printf("Assistant: %s\n", result.Text)
			assistantContent = append(assistantContent, types.NewTextPart(result.Text))
		}

		// Check if there are tool calls
		if len(result.ToolCalls) == 0 {
			fmt.Println("\nTask completed!")
			fmt.Printf("Total token usage: %d\n", result.Usage.GetTotalTokens())
			break
		}

		// Execute tool calls
		fmt.Printf("\nTool calls: %d\n", len(result.ToolCalls))
		for _, tc := range result.ToolCalls {
			fmt.Printf("- %s\n", tc.ToolName)
			argsJSON, _ := json.MarshalIndent(tc.Arguments, "  ", "  ")
			fmt.Printf("  Args: %s\n", string(argsJSON))

			// Add tool call to assistant content
			assistantContent = append(assistantContent, types.NewToolCallPart(tc.ID, tc.ToolName, tc.Arguments))

			// Execute tool
			var toolResult interface{}
			var toolErr error
			for _, tool := range tools {
				if tool.Name == tc.ToolName {
					toolResult, toolErr = tool.Execute(ctx, tc.Arguments)
					break
				}
			}

			// Add tool result to messages
			if toolErr != nil {
				messages = append(messages, types.Message{
					Role: types.RoleAssistant,
					Content: assistantContent,
				})
				messages = append(messages, types.Message{
					Role: types.RoleTool,
					Content: []types.ContentPart{
						types.NewToolResultPart(tc.ID, nil, toolErr.Error()),
					},
				})
			} else {
				resultJSON, _ := json.MarshalIndent(toolResult, "  ", "  ")
				fmt.Printf("  Result: %s\n", string(resultJSON))

				messages = append(messages, types.Message{
					Role: types.RoleAssistant,
					Content: assistantContent,
				})
				messages = append(messages, types.Message{
					Role: types.RoleTool,
					Content: []types.ContentPart{
						types.NewToolResultPart(tc.ID, toolResult, ""),
					},
				})
			}
		}
	}
}
