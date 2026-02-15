package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Example demonstrating AI SDK v6.0 features:
// 1. Output Objects (structured output with type safety)
// 2. Detailed Usage Tracking (cache and reasoning tokens)
// 3. Enhanced Tool System (with new v6.0 fields)
// 4. Context Flow Management (experimental context)

// WeatherTool demonstrates v6.0 tool enhancements
type WeatherTool struct{}

func (w *WeatherTool) Definition() types.Tool {
	return types.Tool{
		Name:        "get_weather",
		Description: "Get the current weather for a location",
		Title:       "Weather Information", // v6.0: NEW Title field

		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "The city and state, e.g. San Francisco, CA",
				},
				"unit": map[string]interface{}{
					"type": "string",
					"enum": []string{"celsius", "fahrenheit"},
				},
			},
			"required": []string{"location"},
		},

		// v6.0: NEW InputExamples for better LLM guidance
		InputExamples: []types.ToolInputExample{
			{
				Input: map[string]interface{}{
					"location": "New York, NY",
					"unit":     "fahrenheit",
				},
				Description: "Get weather in Fahrenheit for New York",
			},
		},

		// v6.0: NEW Strict mode for exact schema enforcement
		Strict: true,

		Execute: w.Execute,
	}
}

// Execute demonstrates v6.0 ToolExecutionOptions with ToolCallID and UserContext
func (w *WeatherTool) Execute(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
	location := input["location"].(string)
	unit := "fahrenheit"
	if u, ok := input["unit"].(string); ok {
		unit = u
	}

	// v6.0: Access ToolCallID from options
	fmt.Printf("[Tool Execution] ID: %s, Location: %s\n", opts.ToolCallID, location)

	// v6.0: Access user context passed through ExperimentalContext
	if opts.UserContext != nil {
		if ctx, ok := opts.UserContext.(map[string]interface{}); ok {
			fmt.Printf("[Tool Execution] User Context: %+v\n", ctx)
		}
	}

	// v6.0: Access accumulated usage
	if opts.Usage != nil && opts.Usage.TotalTokens != nil {
		fmt.Printf("[Tool Execution] Tokens used so far: %d\n", *opts.Usage.TotalTokens)
	}

	// Simulate weather API call
	return map[string]interface{}{
		"location":    location,
		"temperature": 72,
		"unit":        unit,
		"condition":   "sunny",
	}, nil
}

// UserContext demonstrates experimental context flow
type UserContext struct {
	UserID    string
	SessionID string
	Metadata  map[string]interface{}
}

func main() {
	ctx := context.Background()

	// Initialize OpenAI provider (supports all v6.0 features)
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable not set")
	}

	provider := openai.New(openai.Config{
		APIKey: apiKey,
	})
	model, err := provider.LanguageModel("gpt-4")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== AI SDK v6.0 Features Demo ===")

	// ========================================================================
	// Feature 1: Output Objects - Structured Output with Type Safety
	// ========================================================================
	fmt.Println("1. Output Objects (Structured Output)")
	fmt.Println("--------------------------------------")

	// Example 1a: Object Output with Schema Validation
	type Recipe struct {
		Name         string   `json:"name"`
		Ingredients  []string `json:"ingredients"`
		Instructions []string `json:"instructions"`
		PrepTime     int      `json:"prepTime"` // in minutes
	}

	recipeSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":         map[string]interface{}{"type": "string"},
			"ingredients":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"instructions": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"prepTime":     map[string]interface{}{"type": "integer"},
		},
		"required": []string{"name", "ingredients", "instructions", "prepTime"},
	})

	recipeOutput := ai.ObjectOutput[Recipe](ai.ObjectOutputOptions{
		Schema:      recipeSchema,
		Name:        "recipe",
		Description: "A cooking recipe with ingredients and instructions",
	})

	recipeResult, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Generate a simple recipe for chocolate chip cookies",
		Output: recipeOutput,
	})

	if err != nil {
		log.Printf("Error generating recipe: %v\n", err)
	} else {
		// Parse the structured output
		var recipe Recipe
		json.Unmarshal([]byte(recipeResult.Text), &recipe)
		fmt.Printf("Generated Recipe: %s\n", recipe.Name)
		fmt.Printf("  Prep Time: %d minutes\n", recipe.PrepTime)
		fmt.Printf("  Ingredients: %d items\n\n", len(recipe.Ingredients))
	}

	// ========================================================================
	// Feature 2: Detailed Usage Tracking
	// ========================================================================
	fmt.Println("2. Detailed Usage Tracking")
	fmt.Println("--------------------------")

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Explain quantum computing in one sentence",
	})

	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		usage := result.Usage

		// v6.0: Detailed token breakdown
		fmt.Printf("Total Tokens: %d\n", usage.GetTotalTokens())
		fmt.Printf("Input Tokens: %d\n", usage.GetInputTokens())
		fmt.Printf("Output Tokens: %d\n", usage.GetOutputTokens())

		// v6.0: Cache token details (if available)
		if usage.InputDetails != nil {
			if usage.InputDetails.CacheReadTokens != nil {
				fmt.Printf("  Cache Read Tokens: %d (saved cost!)\n", *usage.InputDetails.CacheReadTokens)
			}
			if usage.InputDetails.NoCacheTokens != nil {
				fmt.Printf("  No Cache Tokens: %d\n", *usage.InputDetails.NoCacheTokens)
			}
		}

		// v6.0: Reasoning token details (for models like o1)
		if usage.OutputDetails != nil {
			if usage.OutputDetails.ReasoningTokens != nil {
				fmt.Printf("  Reasoning Tokens: %d\n", *usage.OutputDetails.ReasoningTokens)
			}
			if usage.OutputDetails.TextTokens != nil {
				fmt.Printf("  Text Tokens: %d\n", *usage.OutputDetails.TextTokens)
			}
		}

		// v6.0: Raw provider data for full transparency
		if usage.Raw != nil {
			fmt.Printf("  Raw Provider Data: %v\n\n", usage.Raw)
		}
	}

	// ========================================================================
	// Feature 3: Enhanced Tool System
	// ========================================================================
	fmt.Println("3. Enhanced Tool System")
	fmt.Println("-----------------------")

	weatherTool := &WeatherTool{}

	toolResult, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "What's the weather like in San Francisco?",
		Tools:  []types.Tool{weatherTool.Definition()},

		// v6.0: Context flows through tool execution
		ExperimentalContext: UserContext{
			UserID:    "user-123",
			SessionID: "session-456",
			Metadata: map[string]interface{}{
				"source": "v6-example",
			},
		},

		// v6.0: Updated callback signatures with user context
		OnStepFinish: func(ctx context.Context, step types.StepResult, userContext interface{}) {
			fmt.Printf("[Callback] Step %d completed\n", step.StepNumber)
			if len(step.ToolCalls) > 0 {
				fmt.Printf("[Callback] Tools called: %d\n", len(step.ToolCalls))
			}
		},

		OnFinish: func(ctx context.Context, result *ai.GenerateTextResult, userContext interface{}) {
			fmt.Printf("[Callback] Generation finished\n")
			fmt.Printf("[Callback] Total steps: %d\n", len(result.Steps))

			// Access user context in callbacks
			if userCtx, ok := userContext.(UserContext); ok {
				fmt.Printf("[Callback] User: %s, Session: %s\n", userCtx.UserID, userCtx.SessionID)
			}
		},
	})

	if err != nil {
		log.Printf("Error with tool calling: %v\n", err)
	} else {
		fmt.Printf("\nFinal Response: %s\n", toolResult.Text)
		fmt.Printf("Tool Calls Made: %d\n", len(toolResult.ToolResults))
		fmt.Printf("Steps Taken: %d\n\n", len(toolResult.Steps))
	}

	// ========================================================================
	// Feature 4: Array Output for Lists
	// ========================================================================
	fmt.Println("4. Array Output (Structured Lists)")
	fmt.Println("-----------------------------------")

	type Task struct {
		Title          string `json:"title"`
		Priority       string `json:"priority"` // high, medium, low
		EstimatedHours int    `json:"estimatedHours"`
	}

	taskSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title":          map[string]interface{}{"type": "string"},
			"priority":       map[string]interface{}{"type": "string", "enum": []string{"high", "medium", "low"}},
			"estimatedHours": map[string]interface{}{"type": "integer"},
		},
		"required": []string{"title", "priority", "estimatedHours"},
	})

	arrayOutput := ai.ArrayOutput[Task](ai.ArrayOutputOptions[Task]{
		ElementSchema: taskSchema,
		Name:          "tasks",
		Description:   "List of tasks for a software project",
	})

	tasksResult, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Generate 3 tasks for building a TODO app",
		Output: arrayOutput,
	})

	if err != nil {
		log.Printf("Error generating tasks: %v\n", err)
	} else {
		var tasks []Task
		json.Unmarshal([]byte(tasksResult.Text), &tasks)
		fmt.Printf("Generated %d tasks:\n", len(tasks))
		for i, task := range tasks {
			fmt.Printf("  %d. %s (%s priority, %dh)\n",
				i+1, task.Title, task.Priority, task.EstimatedHours)
		}
		fmt.Println()
	}

	// ========================================================================
	// Feature 5: Choice Output for Enums
	// ========================================================================
	fmt.Println("5. Choice Output (Enum Selection)")
	fmt.Println("----------------------------------")

	type Sentiment string
	const (
		SentimentPositive Sentiment = "positive"
		SentimentNeutral  Sentiment = "neutral"
		SentimentNegative Sentiment = "negative"
	)

	choiceOutput := ai.ChoiceOutput[Sentiment](ai.ChoiceOutputOptions[Sentiment]{
		Options: []Sentiment{
			SentimentPositive,
			SentimentNeutral,
			SentimentNegative,
		},
		Name:        "sentiment",
		Description: "Sentiment analysis result",
	})

	sentimentResult, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Analyze sentiment: 'This product exceeded my expectations!'",
		Output: choiceOutput,
	})

	if err != nil {
		log.Printf("Error analyzing sentiment: %v\n", err)
	} else {
		fmt.Printf("Detected Sentiment: %s\n\n", sentimentResult.Text)
	}

	// ========================================================================
	// Summary
	// ========================================================================
	fmt.Println("=== Demo Complete ===")
	fmt.Println("\nv6.0 Features Demonstrated:")
	fmt.Println("✅ Output Objects (Object, Array, Choice)")
	fmt.Println("✅ Detailed Usage Tracking (cache + reasoning tokens)")
	fmt.Println("✅ Enhanced Tools (Title, InputExamples, Strict mode)")
	fmt.Println("✅ Context Flow (ExperimentalContext in callbacks and tools)")
	fmt.Println("\nFor migration guide and more examples, see:")
	fmt.Println("  planning/migration-guide-v6.md")
	fmt.Println("  planning/usage-tracking-guide.md")
}
