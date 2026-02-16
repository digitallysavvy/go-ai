package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
)

// Example 3: Tool Calling with Google Vertex AI
// Demonstrates function calling capabilities

func main() {
	// Create Google Vertex AI provider
	project := os.Getenv("GOOGLE_VERTEX_PROJECT")
	location := os.Getenv("GOOGLE_VERTEX_LOCATION")
	token := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN")

	if project == "" || location == "" || token == "" {
		log.Fatal("Missing required environment variables")
	}

	prov, err := googlevertex.New(googlevertex.Config{
		Project:     project,
		Location:    location,
		AccessToken: token,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create language model
	model, err := prov.LanguageModel(googlevertex.ModelGemini15Pro)
	if err != nil {
		log.Fatal(err)
	}

	// Define tools
	tools := []types.Tool{
		{
			Name:        "weather",
			Description: "Get the weather in a location",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{
						"type":        "string",
						"description": "The location to get the weather for",
					},
				},
				"required": []string{"location"},
			},
		},
	}

	// Generate with tools
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "What is the weather in New York City?"},
					},
				},
			},
		},
		Tools: tools,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Check for tool calls
	if len(result.ToolCalls) > 0 {
		for _, toolCall := range result.ToolCalls {
			fmt.Printf("Tool called: %s\n", toolCall.ToolName)
			fmt.Printf("Arguments: %v\n", toolCall.Arguments)

			// Execute the tool (mock implementation)
			if toolCall.ToolName == "weather" {
				location := toolCall.Arguments["location"]
				temp := 72 + rand.Intn(21) - 10
				fmt.Printf("Getting weather for %s: %d°F\n", location, temp)
			}
		}
	}

	// Print the response
	if result.Text != "" {
		fmt.Printf("\nResponse: %s\n", result.Text)
	}

	fmt.Printf("\nToken usage: %d total\n", result.Usage.GetTotalTokens())
}
