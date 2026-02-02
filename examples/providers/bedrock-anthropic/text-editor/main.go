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

	// Define text editor tool
	// Note: This will be automatically upgraded and renamed:
	// text_editor_20241022 -> text_editor_20250728 -> str_replace_based_edit_tool
	textEditorTool := types.Tool{
		Name:        "text_editor_20241022",
		Description: "Edit text files using string replacement",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type": "string",
					"enum": []string{"view", "str_replace"},
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The file path",
				},
				"old_str": map[string]interface{}{
					"type":        "string",
					"description": "The string to replace",
				},
				"new_str": map[string]interface{}{
					"type":        "string",
					"description": "The replacement string",
				},
			},
			"required": []string{"command", "path"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			command := args["command"].(string)
			path := args["path"].(string)

			switch command {
			case "view":
				return map[string]interface{}{
					"content": "File content here...",
				}, nil
			case "str_replace":
				oldStr := args["old_str"].(string)
				newStr := args["new_str"].(string)
				return map[string]interface{}{
					"success": true,
					"message": fmt.Sprintf("Replaced '%s' with '%s' in %s", oldStr, newStr, path),
				}, nil
			}
			return nil, fmt.Errorf("unknown command: %s", command)
		},
	}

	// Generate text with text editor tool
	result, err := ai.GenerateText(ctx, ai.GenerateOptions{
		Model:  model,
		Prompt: "Replace the word 'hello' with 'goodbye' in the file test.txt",
		Tools:  []types.Tool{textEditorTool},
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
		fmt.Println("Note: Tool was automatically transformed:")
		fmt.Println("  text_editor_20241022 -> text_editor_20250728 -> str_replace_based_edit_tool")
	}

	fmt.Printf("\nToken usage: %d total\n", result.Usage.GetTotalTokens())
}
