package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	ctx := context.Background()

	fmt.Println("=== Go AI SDK - Comprehensive Example ===")

	// Example 1: Multiple Providers
	fmt.Println("1. Using Multiple Providers")
	multiProviderExample(ctx)

	// Example 2: Embeddings and Similarity Search
	fmt.Println("\n2. Embeddings and Similarity Search")
	embeddingExample(ctx)

	// Example 3: Agent with Tools
	fmt.Println("\n3. Autonomous Agent with Tools")
	agentExample(ctx)
}

func multiProviderExample(ctx context.Context) {
	// Try OpenAI
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		fmt.Println("  Using OpenAI (GPT-4):")
		provider := openai.New(openai.Config{APIKey: apiKey})
		model, _ := provider.LanguageModel("gpt-4")

		result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model:  model,
			Prompt: "Say hello in one sentence",
		})
		if err != nil {
			log.Printf("    Error: %v", err)
		} else {
			fmt.Printf("    Response: %s\n", result.Text)
		}
	}

	// Try Anthropic
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		fmt.Println("  Using Anthropic (Claude):")
		provider := anthropic.New(anthropic.Config{APIKey: apiKey})
		model, _ := provider.LanguageModel("claude-sonnet-4-5")

		result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model:  model,
			Prompt: "Say hello in one sentence",
		})
		if err != nil {
			log.Printf("    Error: %v", err)
		} else {
			fmt.Printf("    Response: %s\n", result.Text)
		}
	}

	// Try Google
	if apiKey := os.Getenv("GOOGLE_API_KEY"); apiKey != "" {
		fmt.Println("  Using Google (Gemini):")
		provider := google.New(google.Config{APIKey: apiKey})
		model, _ := provider.LanguageModel(google.ModelGemini20Flash)

		result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model:  model,
			Prompt: "Say hello in one sentence",
		})
		if err != nil {
			log.Printf("    Error: %v", err)
		} else {
			fmt.Printf("    Response: %s\n", result.Text)
		}
	}
}

func embeddingExample(ctx context.Context) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("  Skipped (OPENAI_API_KEY not set)")
		return
	}

	provider := openai.New(openai.Config{APIKey: apiKey})
	embeddingModel, _ := provider.EmbeddingModel("text-embedding-3-small")

	// Create embeddings for a knowledge base
	documents := []string{
		"The Go programming language is efficient and concurrent",
		"Python is great for data science and machine learning",
		"JavaScript is the language of the web",
		"Rust provides memory safety without garbage collection",
	}

	fmt.Println("  Creating embeddings for documents...")
	result, err := ai.EmbedMany(ctx, ai.EmbedManyOptions{
		Model:  embeddingModel,
		Inputs: documents,
	})
	if err != nil {
		log.Printf("    Error: %v", err)
		return
	}

	// Create query embedding
	query := "What language should I use for concurrent programming?"
	queryResult, err := ai.Embed(ctx, ai.EmbedOptions{
		Model: embeddingModel,
		Input: query,
	})
	if err != nil {
		log.Printf("    Error: %v", err)
		return
	}

	// Find most similar document
	idx, similarity, err := ai.FindMostSimilar(queryResult.Embedding, result.Embeddings)
	if err != nil {
		log.Printf("    Error: %v", err)
		return
	}

	fmt.Printf("  Query: %s\n", query)
	fmt.Printf("  Most similar document: %s\n", documents[idx])
	fmt.Printf("  Similarity score: %.4f\n", similarity)
}

func agentExample(ctx context.Context) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("  Skipped (OPENAI_API_KEY not set)")
		return
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, _ := p.LanguageModel("gpt-4")

	// Define tools for the agent
	calculatorTool := types.Tool{
		Name:        "calculator",
		Description: "Performs basic arithmetic operations",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type": "string",
					"enum": []string{"add", "subtract", "multiply", "divide"},
				},
				"a": map[string]interface{}{"type": "number"},
				"b": map[string]interface{}{"type": "number"},
			},
			"required": []string{"operation", "a", "b"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			op := params["operation"].(string)
			a := params["a"].(float64)
			b := params["b"].(float64)

			switch op {
			case "add":
				return a + b, nil
			case "subtract":
				return a - b, nil
			case "multiply":
				return a * b, nil
			case "divide":
				if b == 0 {
					return nil, fmt.Errorf("division by zero")
				}
				return a / b, nil
			default:
				return nil, fmt.Errorf("unknown operation: %s", op)
			}
		},
	}

	weatherTool := types.Tool{
		Name:        "get_weather",
		Description: "Gets the current weather for a location",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "City name",
				},
			},
			"required": []string{"location"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			location := params["location"].(string)
			// Simulate API call
			return map[string]interface{}{
				"location":    location,
				"temperature": 72,
				"condition":   "sunny",
				"humidity":    45,
			}, nil
		},
	}

	// Create agent
	agentInstance := agent.NewToolLoopAgent(agent.AgentConfig{
		Model:    model,
		System:   "You are a helpful assistant that can use tools to answer questions.",
		Tools:    []types.Tool{calculatorTool, weatherTool},
		MaxSteps: 5,
	})

	// Execute agent
	result, err := agentInstance.Execute(ctx, "What's 15 times 23, and what's the weather in San Francisco?")
	if err != nil {
		log.Printf("    Error: %v", err)
		return
	}

	fmt.Printf("  Final Answer: %s\n", result.Text)
	fmt.Printf("  Steps taken: %d\n", len(result.Steps))
	if len(result.ToolResults) > 0 {
		fmt.Printf("  Tools called: %d\n", len(result.ToolResults))
	}
}
