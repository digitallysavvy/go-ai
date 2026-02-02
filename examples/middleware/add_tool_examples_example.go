package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/middleware"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	// Create OpenAI provider
	openaiProvider := openai.New(openai.Config{
		APIKey: "your-api-key-here",
	})

	// Get a language model
	model := openaiProvider.LanguageModel("gpt-4")

	// Apply addToolInputExamples middleware
	// This middleware helps improve tool calling accuracy by adding examples to tool descriptions
	// Useful for providers that don't natively support inputExamples
	wrappedModel := middleware.WrapLanguageModel(
		model,
		[]*middleware.LanguageModelMiddleware{
			middleware.AddToolInputExamplesMiddleware(&middleware.AddToolInputExamplesOptions{
				Prefix: "Input Examples:",
				Remove: true, // Remove inputExamples after adding to description
			}),
		},
		nil,
		nil,
	)

	// Define a tool with input examples
	getWeatherTool := types.Tool{
		Name:        "get_weather",
		Description: "Get the current weather for a location",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"city": map[string]interface{}{
					"type":        "string",
					"description": "The city name",
				},
				"unit": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"celsius", "fahrenheit"},
					"description": "Temperature unit",
				},
			},
			"required": []string{"city"},
		},
		InputExamples: []types.ToolInputExample{
			{
				Input: map[string]interface{}{
					"city": "New York",
					"unit": "fahrenheit",
				},
			},
			{
				Input: map[string]interface{}{
					"city": "London",
					"unit": "celsius",
				},
			},
			{
				Input: map[string]interface{}{
					"city": "Tokyo",
				},
			},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			city := input["city"].(string)
			unit := "celsius"
			if u, ok := input["unit"]; ok {
				unit = u.(string)
			}

			return map[string]interface{}{
				"city":        city,
				"temperature": 22,
				"unit":        unit,
				"conditions":  "Sunny",
			}, nil
		},
	}

	// Use the wrapped model with the tool
	// The middleware will automatically append the examples to the tool description
	result, err := wrappedModel.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: provider.Prompt{
			Text: "What's the weather like in Paris?",
		},
		Tools: []types.Tool{getWeatherTool},
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Response: %s\n", result.Text)
	if len(result.ToolCalls) > 0 {
		fmt.Printf("Tool calls: %d\n", len(result.ToolCalls))
		for _, call := range result.ToolCalls {
			fmt.Printf("  - %s: %v\n", call.ToolName, call.Arguments)
		}
	}

	// Example with custom format
	customMiddleware := middleware.AddToolInputExamplesMiddleware(&middleware.AddToolInputExamplesOptions{
		Prefix: "Example Usage:",
		Format: func(example types.ToolInputExample, index int) string {
			// Custom formatting for examples
			return fmt.Sprintf("Example %d: %v", index+1, example.Input)
		},
		Remove: false, // Keep inputExamples in the tool definition
	})

	wrappedModel2 := middleware.WrapLanguageModel(
		model,
		[]*middleware.LanguageModelMiddleware{customMiddleware},
		nil,
		nil,
	)

	_ = wrappedModel2 // Use as needed

	// Benefits:
	// 1. Improves LLM tool calling accuracy
	// 2. Provides clear usage patterns for complex tools
	// 3. Works with providers that don't support inputExamples natively
	// 4. Reduces token usage by removing redundant example data after conversion
}
