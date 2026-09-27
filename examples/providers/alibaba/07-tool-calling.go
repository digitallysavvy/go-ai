//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
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

	// Define tools. Execute functions let ai.GenerateText run the full tool
	// loop (call model -> execute tool -> send result back) automatically.
	weatherTool := types.Tool{
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
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			location, _ := params["location"].(string)
			unit := "celsius"
			if u, ok := params["unit"].(string); ok {
				unit = u
			}
			return map[string]interface{}{
				"temperature": 18,
				"unit":        unit,
				"condition":   "partly cloudy",
				"location":    location,
			}, nil
		},
	}

	timeTool := types.Tool{
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
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			timezone, _ := params["timezone"].(string)
			return map[string]interface{}{
				"timezone": timezone,
				"time":     time.Now().Format("15:04:05"),
			}, nil
		},
	}

	fmt.Println("User: What's the weather in Paris, France and what time is it there?")
	fmt.Println()

	maxSteps := 5
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Prompt:   "What's the weather in Paris, France and what time is it there?",
		Tools:    []types.Tool{weatherTool, timeTool},
		MaxSteps: &maxSteps,
	})
	if err != nil {
		log.Fatal(err)
	}

	if len(result.ToolCalls) > 0 {
		fmt.Printf("Model made %d tool call(s) across %d step(s):\n", len(result.ToolCalls), len(result.Steps))
		for _, tc := range result.ToolCalls {
			fmt.Printf("  - %s(%v)\n", tc.ToolName, tc.Arguments)
		}
		fmt.Println()
	}

	fmt.Println("Assistant Response:")
	fmt.Println(result.Text)
	fmt.Println()

	// Print token usage
	fmt.Printf("Token Usage:\n")
	fmt.Printf("  Input:  %d tokens\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output: %d tokens\n", result.Usage.GetOutputTokens())
	fmt.Printf("  Total:  %d tokens\n", result.Usage.GetTotalTokens())
}
