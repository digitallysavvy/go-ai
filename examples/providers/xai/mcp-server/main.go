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

	// Get MCP server URL from environment
	mcpServerURL := os.Getenv("MCP_SERVER_URL")
	if mcpServerURL == "" {
		// Use a default example URL if not provided
		mcpServerURL = "https://api.example.com/mcp"
		fmt.Printf("⚠️  MCP_SERVER_URL not set, using example URL: %s\n", mcpServerURL)
		fmt.Println("   (This will likely fail without a real MCP server)")
		fmt.Println()
	}

	// Get optional MCP authorization token
	mcpAuth := os.Getenv("MCP_AUTHORIZATION")

	// Create xAI provider
	provider := xai.New(xai.Config{
		APIKey: apiKey,
	})

	// Get the language model
	model, err := provider.LanguageModel("grok-beta")
	if err != nil {
		log.Fatalf("Failed to get language model: %v", err)
	}

	// Create the MCP Server tool with configuration
	var mcpTool types.Tool
	if mcpAuth != "" {
		// With authentication
		mcpTool = xai.MCPServer(xai.MCPServerConfig{
			ServerURL:     mcpServerURL,
			Authorization: mcpAuth,
			AllowedTools:  []string{"search", "summarize", "translate"},
			Headers: map[string]string{
				"X-API-Version": "v1",
			},
		})
	} else {
		// Simple configuration without auth
		mcpTool = xai.MCPServerSimple(mcpServerURL)
	}

	fmt.Println("🔌 xAI MCP Server Example")
	fmt.Println("=" + string(make([]byte, 40)))
	fmt.Println()
	fmt.Println("This example demonstrates using xAI's MCP Server tool to connect")
	fmt.Println("to remote Model Context Protocol (MCP) servers.")
	fmt.Println("The tool is executed by xAI's servers (provider-executed).")
	fmt.Println()
	fmt.Printf("MCP Server URL: %s\n", mcpServerURL)
	fmt.Println()

	// Example query
	query := "Search for information about Go programming language best practices"

	fmt.Printf("Query: %s\n\n", query)

	// Generate text with the MCP server tool
	maxTokens := 1500
	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:      model,
		Prompt:     query,
		Tools:      []types.Tool{mcpTool},
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
			fmt.Printf("   Arguments:\n")
			for key, value := range toolCall.Arguments {
				fmt.Printf("     - %s: %v\n", key, value)
			}
			fmt.Println()
		}
	}

	// Display usage information
	fmt.Println("Usage:")
	fmt.Println("-" + string(make([]byte, 40)))
	if result.Usage.InputTokens != nil {
		fmt.Printf("Input tokens: %d\n", *result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != nil {
		fmt.Printf("Output tokens: %d\n", *result.Usage.OutputTokens)
	}
	if result.Usage.TotalTokens != nil {
		fmt.Printf("Total tokens: %d\n", *result.Usage.TotalTokens)
	}

	fmt.Println()
	fmt.Println("✅ MCP Server integration completed successfully")
	fmt.Println()
	fmt.Println("💡 Tips:")
	fmt.Println("   - Set MCP_SERVER_URL to your MCP server endpoint")
	fmt.Println("   - Set MCP_AUTHORIZATION if your server requires authentication")
	fmt.Println("   - Configure AllowedTools to restrict which tools can be used")
	fmt.Println("   - Add custom headers for API versioning or other requirements")
}
