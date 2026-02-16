// Package main demonstrates tool calling with Moonshot AI
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
)

func main() {
	// Create Moonshot provider
	cfg, err := moonshot.NewConfig("") // Uses MOONSHOT_API_KEY env var
	if err != nil {
		log.Fatalf("Failed to create config: %v", err)
	}

	prov := moonshot.New(cfg)

	// Get language model
	model, err := prov.LanguageModel("moonshot-v1-32k")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Define tools
	tools := []types.Tool{
		{
			Name:        "get_weather",
			Description: "Get the current weather for a location",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{
						"type":        "string",
						"description": "The city and country, e.g. San Francisco, US",
					},
					"unit": map[string]interface{}{
						"type": "string",
						"enum": []string{"celsius", "fahrenheit"},
					},
				},
				"required": []string{"location"},
			},
		},
		{
			Name:        "get_time",
			Description: "Get the current time for a timezone",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"timezone": map[string]interface{}{
						"type":        "string",
						"description": "The timezone, e.g. America/New_York",
					},
				},
				"required": []string{"timezone"},
			},
		},
	}

	// Generate response with tools
	ctx := context.Background()
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: "You are a helpful assistant with access to weather and time information.",
		Prompt: "What's the weather like in Tokyo, and what time is it there?",
		Tools:  tools,
	})
	if err != nil {
		log.Fatalf("Failed to generate: %v", err)
	}

	// Check if model wants to call tools
	if len(result.ToolCalls) > 0 {
		fmt.Println("Model requested tool calls:")
		for i, toolCall := range result.ToolCalls {
			fmt.Printf("\nTool Call %d:\n", i+1)
			fmt.Printf("  ID: %s\n", toolCall.ID)
			fmt.Printf("  Tool: %s\n", toolCall.ToolName)
			fmt.Printf("  Arguments: %+v\n", toolCall.Arguments)

			// Execute tool (simulated)
			var toolResult string
			switch toolCall.ToolName {
			case "get_weather":
				location := toolCall.Arguments["location"]
				toolResult = fmt.Sprintf("The weather in %v is sunny, 22°C", location)
			case "get_time":
				timezone := toolCall.Arguments["timezone"]
				toolResult = fmt.Sprintf("The current time in %v is 14:30", timezone)
			}
			fmt.Printf("  Result: %s\n", toolResult)
		}

		fmt.Printf("\nFinish reason: %s\n", result.FinishReason)
		fmt.Println("\nIn a real application, you would:")
		fmt.Println("1. Execute the tool calls")
		fmt.Println("2. Add the tool results to the conversation")
		fmt.Println("3. Call the model again to get the final response")
	} else {
		fmt.Println("Response:", result.Text)
	}

	// Print usage
	fmt.Println("\nToken Usage:")
	fmt.Printf("  Input tokens: %d\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output tokens: %d\n", result.Usage.GetOutputTokens())
	fmt.Printf("  Total tokens: %d\n", result.Usage.GetTotalTokens())
}
