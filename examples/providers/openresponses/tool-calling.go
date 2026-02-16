//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
)

// This example demonstrates tool calling with a local LLM
// Prerequisites:
// 1. Install and start LMStudio (or another service)
// 2. Load a model that supports tool calling (e.g., Mistral-7B-Instruct, Llama-3)
// Note: Not all models support tool calling. Check your model's capabilities.

func main() {
	// Create Open Responses provider
	provider := openresponses.New(openresponses.Config{
		BaseURL: "http://localhost:1234/v1",
	})

	// Get language model
	model, err := provider.LanguageModel("local-model")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Tool Calling with Local LLM ===
")

	// Example 1: Simple tool call
	fmt.Println("Example 1: Weather Tool")
	weatherExample(ctx, model, provider)

	fmt.Println("

" + strings.Repeat("=", 50))

	// Example 2: Math tool
	fmt.Println("

Example 2: Calculator Tool")
	calculatorExample(ctx, model, provider)

	fmt.Println("

" + strings.Repeat("=", 50))

	// Example 3: Multiple tools
	fmt.Println("

Example 3: Multiple Tools")
	multipleToolsExample(ctx, model, provider)
}

func weatherExample(ctx context.Context, model provider.LanguageModel, p *openresponses.Provider) {
	// Define a weather tool
	weatherTool := types.Tool{
		Name:        "get_weather",
		Description: "Get the current weather for a location",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "The city and state, e.g., San Francisco, CA",
				},
				"unit": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"celsius", "fahrenheit"},
					"description": "Temperature unit",
				},
			},
			"required": []string{"location"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			location := params["location"].(string)
			unit := "fahrenheit"
			if u, ok := params["unit"].(string); ok {
				unit = u
			}

			// Simulate weather API call
			return map[string]interface{}{
				"location":    location,
				"temperature": 72,
				"condition":   "sunny",
				"unit":        unit,
			}, nil
		},
	}

	// Ask about weather
	maxSteps := 5
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Prompt:   "What's the weather like in San Francisco right now?",
		Tools:    []types.Tool{weatherTool},
		MaxSteps: &maxSteps,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("Question: What's the weather like in San Francisco right now?
")
	fmt.Printf("Answer: %s
", result.Text)

	if len(result.ToolCalls) > 0 {
		fmt.Printf("
Tool calls made: %d
", len(result.ToolCalls))
		for i, tc := range result.ToolCalls {
			fmt.Printf("  %d. %s with args: %v
", i+1, tc.ToolName, tc.Arguments)
		}
	}
}

func calculatorExample(ctx context.Context, model provider.LanguageModel, p *openresponses.Provider) {
	// Define a calculator tool
	calculatorTool := types.Tool{
		Name:        "calculate",
		Description: "Perform arithmetic calculations",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"expression": map[string]interface{}{
					"type":        "string",
					"description": "Mathematical expression to evaluate (e.g., '15 * 23')",
				},
			},
			"required": []string{"expression"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			expression := params["expression"].(string)
			// Simple calculation (in production, use a proper math parser)
			// For demo purposes, just return hardcoded results
			results := map[string]float64{
				"15 * 23":   345,
				"100 / 4":   25,
				"50 + 125":  175,
				"1000 - 1":  999,
				"2^8":       256,
				"sqrt(144)": 12,
			}

			if result, ok := results[expression]; ok {
				return result, nil
			}
			return "Unable to calculate", nil
		},
	}

	// Ask a math question
	maxSteps := 5
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Prompt:   "What is 15 multiplied by 23?",
		Tools:    []types.Tool{calculatorTool},
		MaxSteps: &maxSteps,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("Question: What is 15 multiplied by 23?
")
	fmt.Printf("Answer: %s
", result.Text)

	if len(result.Steps) > 1 {
		fmt.Printf("
Steps taken: %d
", len(result.Steps))
	}
}

func multipleToolsExample(ctx context.Context, model provider.LanguageModel, p *openresponses.Provider) {
	// Define multiple tools
	timeTool := types.Tool{
		Name:        "get_current_time",
		Description: "Get the current time",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return time.Now().Format("3:04 PM MST"), nil
		},
	}

	dateTool := types.Tool{
		Name:        "get_current_date",
		Description: "Get the current date",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return time.Now().Format("Monday, January 2, 2006"), nil
		},
	}

	// Ask a question that might use both tools
	maxSteps := 5
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Prompt:   "What's the current date and time?",
		Tools:    []types.Tool{timeTool, dateTool},
		MaxSteps: &maxSteps,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("Question: What's the current date and time?
")
	fmt.Printf("Answer: %s
", result.Text)

	if len(result.ToolCalls) > 0 {
		fmt.Printf("
Tools used:
")
		for _, tc := range result.ToolCalls {
			fmt.Printf("  - %s
", tc.ToolName)
		}
	}
}
