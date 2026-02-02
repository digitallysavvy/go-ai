package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	// Create Anthropic provider
	provider := anthropic.New(anthropic.Config{
		APIKey: apiKey,
	})

	// Get language model
	model, err := provider.LanguageModel("claude-opus-4-20250514")
	if err != nil {
		log.Fatalf("Failed to get model: %v", err)
	}

	// Create code execution tool
	codeExecTool := tools.CodeExecution20250825()

	// Example 1: Data analysis with Python
	fmt.Println("=== Example 1: Data Analysis ===")
	runDataAnalysisExample(model, codeExecTool)

	fmt.Println("\n=== Example 2: Bash Commands ===")
	runBashExample(model, codeExecTool)

	fmt.Println("\n=== Example 3: File Operations ===")
	runFileOperationsExample(model, codeExecTool)
}

func runDataAnalysisExample(model ai.LanguageModel, tool ai.Tool) {
	ctx := context.Background()
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Messages: []ai.Message{
			{
				Role: ai.RoleUser,
				Content: ai.MessageContent{
					Text: stringPtr("Create a simple bar chart showing sales data: Q1=100, Q2=150, Q3=120, Q4=180. Use matplotlib and save it as sales.png"),
				},
			},
		},
		Tools: map[string]ai.Tool{
			"codeExecution": tool,
		},
		MaxSteps: intPtr(5),
	})

	if err != nil {
		log.Printf("Error in data analysis: %v", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
	printToolCalls(result.ToolCalls)
}

func runBashExample(model ai.LanguageModel, tool ai.Tool) {
	ctx := context.Background()
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Messages: []ai.Message{
			{
				Role: ai.RoleUser,
				Content: ai.MessageContent{
					Text: stringPtr("Use bash commands to list the current directory contents and show disk usage"),
				},
			},
		},
		Tools: map[string]ai.Tool{
			"codeExecution": tool,
		},
		MaxSteps: intPtr(5),
	})

	if err != nil {
		log.Printf("Error in bash example: %v", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
	printToolCalls(result.ToolCalls)
}

func runFileOperationsExample(model ai.LanguageModel, tool ai.Tool) {
	ctx := context.Background()
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Messages: []ai.Message{
			{
				Role: ai.RoleUser,
				Content: ai.MessageContent{
					Text: stringPtr("Create a file called 'config.json' with sample configuration data, then read it back to verify"),
				},
			},
		},
		Tools: map[string]ai.Tool{
			"codeExecution": tool,
		},
		MaxSteps: intPtr(5),
	})

	if err != nil {
		log.Printf("Error in file operations: %v", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
	printToolCalls(result.ToolCalls)
}

func printToolCalls(toolCalls []ai.ToolCall) {
	if len(toolCalls) > 0 {
		fmt.Println("\nTool Calls:")
		for i, toolCall := range toolCalls {
			fmt.Printf("  %d. %s\n", i+1, toolCall.ToolName)
			fmt.Printf("     Arguments: %v\n", toolCall.Arguments)
		}
	}
}

func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}
