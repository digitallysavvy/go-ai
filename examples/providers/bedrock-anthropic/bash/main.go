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

	// Define bash tool
	// Note: This will be automatically upgraded:
	// bash_20241022 -> bash_20250124
	bashTool := types.Tool{
		Name:        "bash_20241022",
		Description: "Execute bash commands in a shell",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The bash command to execute",
				},
				"restart": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to restart the shell session",
				},
			},
			"required": []string{"command"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			command := args["command"].(string)

			// Mock execution
			return map[string]interface{}{
				"output":    fmt.Sprintf("Executed: %s", command),
				"exit_code": 0,
			}, nil
		},
	}

	// Generate text with bash tool
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "List all files in the current directory using bash",
		Tools:  []types.Tool{bashTool},
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
			argsJSON, _ := json.MarshalIndent(tc.Arguments, "  ", "  ")
			fmt.Printf("  Arguments: %s\n", string(argsJSON))
		}
		fmt.Println()
		fmt.Println("Note: Tool version was automatically upgraded:")
		fmt.Println("  bash_20241022 -> bash_20250124")
		fmt.Println("  anthropic_beta header added: computer-use-2025-01-24")
	}

	fmt.Printf("\nToken usage: %d total\n", result.Usage.GetTotalTokens())
}
