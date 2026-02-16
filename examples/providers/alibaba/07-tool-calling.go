//go:build ignore
// +build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
)

// Example 7: Tool Calling with Qwen
// This example demonstrates function calling capabilities with Alibaba's models

func main() {
	// Create Alibaba provider
	cfg, err := alibaba.NewConfig(os.Getenv("ALIBABA_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	prov := alibaba.New(cfg)
	model, err := prov.LanguageModel("qwen-max")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

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
						"description": "The city and country, e.g. London, UK",
					},
					"unit": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"celsius", "fahrenheit"},
						"description": "The unit of temperature",
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
						"description": "IANA timezone identifier, e.g. America/New_York",
					},
				},
				"required": []string{"timezone"},
			},
		},
	}

	// Create prompt
	prompt := types.Prompt{
		Text: "What's the weather in Paris, France and what time is it there?",
	}

	// Generate with tools
	fmt.Println("User: What's the weather in Paris, France and what time is it there?")
	fmt.Println()

	result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: prompt,
		Tools:  tools,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Check for tool calls
	if len(result.ToolCalls) > 0 {
		fmt.Printf("Model requested %d tool calls:\n", len(result.ToolCalls))
		fmt.Println()

		// Execute tool calls and collect results
		toolResults := make([]types.ToolResult, 0)
		for _, tc := range result.ToolCalls {
			fmt.Printf("Tool: %s\n", tc.ToolName)
			argsJSON, _ := json.MarshalIndent(tc.Arguments, "  ", "  ")
			fmt.Printf("  Arguments: %s\n", string(argsJSON))

			// Simulate tool execution
			var resultContent string
			switch tc.ToolName {
			case "get_weather":
				location := tc.Arguments["location"].(string)
				unit := "celsius"
				if u, ok := tc.Arguments["unit"].(string); ok {
					unit = u
				}
				resultContent = fmt.Sprintf(`{"temperature": 18, "unit": "%s", "condition": "partly cloudy", "location": "%s"}`, unit, location)

			case "get_time":
				timezone := tc.Arguments["timezone"].(string)
				now := time.Now()
				resultContent = fmt.Sprintf(`{"timezone": "%s", "time": "%s"}`, timezone, now.Format("15:04:05"))
			}

			fmt.Printf("  Result: %s\n", resultContent)
			fmt.Println()

			toolResults = append(toolResults, types.ToolResult{
				ToolCallID: tc.ID,
				ToolName:   tc.ToolName,
				Result:     resultContent,
			})
		}

		// Send tool results back to the model
		messages := []types.Message{
			{
				Role:    "user",
				Content: []types.ContentPart{{Type: "text", Text: prompt.Text}},
			},
			{
				Role:      "assistant",
				Content:   []types.ContentPart{{Type: "text", Text: result.Text}},
				ToolCalls: result.ToolCalls,
			},
			{
				Role:        "tool",
				ToolResults: toolResults,
			},
		}

		finalResult, err := model.DoGenerate(ctx, &provider.GenerateOptions{
			Prompt: types.Prompt{Messages: messages},
			Tools:  tools,
		})
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println("Assistant Response:")
		fmt.Println(finalResult.Text)
		fmt.Println()

		// Print token usage
		fmt.Printf("Token Usage:\n")
		fmt.Printf("  Input:  %d tokens\n", finalResult.Usage.GetInputTokens())
		fmt.Printf("  Output: %d tokens\n", finalResult.Usage.GetOutputTokens())
		fmt.Printf("  Total:  %d tokens\n", finalResult.Usage.GetTotalTokens())
	} else {
		fmt.Println("Assistant Response:")
		fmt.Println(result.Text)
	}
}
