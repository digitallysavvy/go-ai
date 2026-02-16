package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("XAI_API_KEY")
	if apiKey == "" {
		log.Fatal("XAI_API_KEY environment variable is required")
	}

	// Get vector store IDs from environment or use default
	vectorStoreID := os.Getenv("XAI_VECTOR_STORE_ID")
	if vectorStoreID == "" {
		log.Fatal("XAI_VECTOR_STORE_ID environment variable is required")
	}

	// Create xAI provider
	provider := xai.New(xai.Config{
		APIKey: apiKey,
	})

	// Get the language model
	model, err := provider.LanguageModel("grok-beta")
	if err != nil {
		log.Fatalf("Failed to get language model: %v", err)
	}

	// Create the FileSearch tool with configuration
	fileSearchTool := xai.FileSearch(xai.FileSearchConfig{
		VectorStoreIDs: []string{vectorStoreID},
		MaxNumResults:  5,
	})

	fmt.Println("🔍 xAI File Search Example")
	fmt.Println("=" + string(make([]byte, 40)))
	fmt.Println()
	fmt.Println("This example demonstrates using xAI's FileSearch tool to search vector stores.")
	fmt.Println("The tool is executed by xAI's servers (provider-executed).")
	fmt.Println()

	// Example query
	query := "What are the main features of the product?"

	fmt.Printf("Query: %s\n\n", query)

	// Generate text with the file search tool
	maxTokens := 1000
	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:      model,
		Prompt:     query,
		Tools:      []types.Tool{fileSearchTool},
		ToolChoice: types.AutoToolChoice(),
		MaxTokens:  &maxTokens,
	})

	if err != nil {
		log.Fatalf("Failed to generate text: %v", err)
	}

	// Display results
	fmt.Println("Response:")
	fmt.Println("-" + string(make([]byte, 40)))
	fmt.Println(result.Text)
	fmt.Println()

	// Display tool calls if any
	if len(result.ToolCalls) > 0 {
		fmt.Println("Tool Calls:")
		fmt.Println("-" + string(make([]byte, 40)))
		for i, toolCall := range result.ToolCalls {
			fmt.Printf("%d. Tool: %s\n", i+1, toolCall.ToolName)
			fmt.Printf("   ID: %s\n", toolCall.ID)
			fmt.Printf("   Arguments: %v\n", toolCall.Arguments)
			fmt.Println()
		}
	}

	// Display usage information
	fmt.Println("Usage:")
	fmt.Println("-" + string(make([]byte, 40)))
	fmt.Printf("Input tokens: %d\n", result.Usage.GetInputTokens())
	fmt.Printf("Output tokens: %d\n", result.Usage.GetOutputTokens())
	fmt.Printf("Total tokens: %d\n", result.Usage.GetTotalTokens())

	fmt.Println()
	fmt.Println("✅ File search completed successfully")
}
