// Recipe: use tools from an MCP server.
//
// Run: MCP_URL=https://your-server.example/mcp ANTHROPIC_API_KEY=... go run ./examples/recipes/mcp-tools
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/mcp"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
	ctx := context.Background()

	transport := mcp.NewHTTPTransport(mcp.HTTPTransportConfig{
		URL:       os.Getenv("MCP_URL"),
		TimeoutMS: 30000,
	})
	client := mcp.NewMCPClient(transport, mcp.MCPClientConfig{
		ClientName:    "recipe",
		ClientVersion: "1.0.0",
	})
	if err := client.Connect(ctx); err != nil {
		log.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	// Every tool the server lists becomes a go-ai tool.
	tools, err := mcp.NewMCPToolConverter(client).ConvertToGoAITools(ctx)
	if err != nil {
		log.Fatal(err)
	}

	model, err := anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")}).
		LanguageModel(anthropic.ClaudeSonnet5_5)
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Prompt:   "Use your tools to answer: what can you do?",
		Tools:    tools,
		StopWhen: []ai.StopCondition{ai.IsStepCount(5)},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}
