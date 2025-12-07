package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Create OpenAI provider
	provider := openai.New(openai.Config{
		APIKey: apiKey,
	})

	// Create language model
	model, err := provider.LanguageModel("gpt-4")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Example 1: Simple text generation
	fmt.Println("=== Example 1: Simple Text Generation ===")
	simpleExample(ctx, model)

	// Example 2: Streaming text generation
	fmt.Println("\n=== Example 2: Streaming Text Generation ===")
	streamingExample(ctx, model)

	// Example 3: Tool calling
	fmt.Println("\n=== Example 3: Tool Calling ===")
	toolCallingExample(ctx, model)

	// Example 4: Structured output
	fmt.Println("\n=== Example 4: Structured Output ===")
	structuredOutputExample(ctx, model)
}

func simpleExample(ctx context.Context, model interface{ ai.GenerateText(context.Context, ai.GenerateTextOptions) (*ai.GenerateTextResult, error) }) {
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model.(ai.LanguageModel),
		Prompt: "Tell me a joke about programming",
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
	fmt.Printf("Tokens used: %d (input: %d, output: %d)\n",
		result.Usage.TotalTokens,
		result.Usage.InputTokens,
		result.Usage.OutputTokens)
}

func streamingExample(ctx context.Context, model interface{}) {
	result, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model:  model.(ai.LanguageModel),
		Prompt: "Write a haiku about Go programming",
		OnChunk: func(chunk ai.StreamChunk) {
			if chunk.Type == ai.ChunkTypeText {
				fmt.Print(chunk.Text)
			}
		},
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}
	defer result.Close()

	// Wait for stream to complete
	_, err = result.ReadAll()
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("\n\nTokens used: %d\n", result.Usage().TotalTokens)
}

func toolCallingExample(ctx context.Context, model interface{}) {
	// Define a tool
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
			},
			"required": []string{"location"},
		},
		Execute: func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			location := input["location"].(string)
			// Simulate weather API call
			return map[string]interface{}{
				"location":    location,
				"temperature": 72,
				"condition":   "sunny",
			}, nil
		},
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model.(ai.LanguageModel),
		Prompt: "What's the weather like in San Francisco?",
		Tools:  []types.Tool{weatherTool},
		OnStepFinish: func(step types.StepResult) {
			fmt.Printf("Step %d completed:\n", step.StepNumber)
			if len(step.ToolCalls) > 0 {
				fmt.Printf("  Tool calls: %d\n", len(step.ToolCalls))
				for _, tc := range step.ToolCalls {
					fmt.Printf("    - %s\n", tc.ToolName)
				}
			}
			if step.Text != "" {
				fmt.Printf("  Text: %s\n", step.Text)
			}
		},
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("\nFinal response: %s\n", result.Text)
	fmt.Printf("Steps taken: %d\n", len(result.Steps))
	fmt.Printf("Tools called: %d\n", len(result.ToolResults))
}

func structuredOutputExample(ctx context.Context, model interface{}) {
	// Define a schema for a person object
	personSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "The person's full name",
			},
			"age": map[string]interface{}{
				"type":        "number",
				"description": "The person's age in years",
			},
			"occupation": map[string]interface{}{
				"type":        "string",
				"description": "The person's occupation",
			},
			"hobbies": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "List of the person's hobbies",
			},
		},
		"required": []string{"name", "age", "occupation"},
	})

	result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
		Model:  model.(ai.LanguageModel),
		Prompt: "Generate a fictional person profile for a software engineer",
		Schema: personSchema,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("Generated object:\n%s\n", result.Text)
	fmt.Printf("Tokens used: %d\n", result.Usage.TotalTokens)
}
