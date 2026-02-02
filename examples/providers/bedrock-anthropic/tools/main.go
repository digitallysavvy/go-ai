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
	getWeatherTool := types.Tool{
		Name:        "get_weather",
		Description: "Get the current weather for a location",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "The city and state, e.g. San Francisco, CA",
				},
				"unit": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"celsius", "fahrenheit"},
					"description": "The unit of temperature",
				},
			},
			"required": []string{"location"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			// Mock implementation
			location := args["location"].(string)
			unit := "fahrenheit"
			if u, ok := args["unit"].(string); ok {
				unit = u
			}
			return map[string]interface{}{
				"location":    location,
				"temperature": 72,
				"unit":        unit,
				"condition":   "sunny",
			}, nil
		},
	}

	// Generate text with tool calling
	result, err := ai.GenerateText(ctx, ai.GenerateOptions{
		Model:  model,
		Prompt: "What's the weather like in San Francisco?",
		Tools:  []types.Tool{getWeatherTool},
		ToolChoice: types.ToolChoice{
			Type: "auto",
		},
	})
	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	// Display result
	fmt.Println("Response:")
	fmt.Println(result.Text)
	fmt.Println()

	// Display tool calls
	if len(result.ToolCalls) > 0 {
		fmt.Println("Tool calls:")
		for _, tc := range result.ToolCalls {
			fmt.Printf("- %s (ID: %s)\n", tc.ToolName, tc.ID)
			argsJSON, _ := json.MarshalIndent(tc.Arguments, "  ", "  ")
			fmt.Printf("  Arguments: %s\n", string(argsJSON))
		}
	}

	fmt.Printf("\nToken usage: %d total\n", result.Usage.GetTotalTokens())
}
