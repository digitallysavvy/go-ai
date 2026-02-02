package main

import (
	"context"
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

	// Define computer use tools
	// Note: These tools will be automatically upgraded to Bedrock-compatible versions
	computerTool := types.Tool{
		Name:        "computer_20241022", // Will be upgraded to computer_20250124
		Description: "Control the computer (mouse, keyboard, screenshot)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type": "string",
					"enum": []string{"key", "type", "mouse_move", "left_click", "screenshot"},
				},
			},
			"required": []string{"action"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			// Mock implementation
			action := args["action"].(string)
			return map[string]interface{}{
				"action": action,
				"result": "success",
			}, nil
		},
	}

	bashTool := types.Tool{
		Name:        "bash_20241022", // Will be upgraded to bash_20250124
		Description: "Execute bash commands",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The bash command to execute",
				},
			},
			"required": []string{"command"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			// Mock implementation
			return map[string]interface{}{
				"output": "Command executed successfully",
			}, nil
		},
	}

	// Generate text with computer use tools
	// The provider will automatically:
	// 1. Upgrade tool versions (computer_20241022 -> computer_20250124)
	// 2. Add anthropic_beta headers for computer use
	result, err := ai.GenerateText(ctx, ai.GenerateOptions{
		Model:  model,
		Prompt: "Take a screenshot of the current screen",
		Tools:  []types.Tool{computerTool, bashTool},
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	// Display result
	fmt.Println("Response:")
	fmt.Println(result.Text)
	fmt.Println()

	if len(result.ToolCalls) > 0 {
		fmt.Println("Tool calls:")
		for _, tc := range result.ToolCalls {
			fmt.Printf("- %s (ID: %s)\n", tc.ToolName, tc.ID)
		}
	}

	fmt.Printf("\nNote: Tool versions were automatically upgraded for Bedrock compatibility\n")
	fmt.Printf("Token usage: %d total\n", result.Usage.GetTotalTokens())
}
