// Package main demonstrates the OpenAI Responses API tool_search tool.
//
// The tool_search tool allows the model to search across deferred tools. There
// are two execution modes:
//
//   - Server mode (default): OpenAI resolves tool matches internally. No
//     tool_search_call event is emitted; the config is sent in the request.
//
//   - Client mode: The model emits a tool_search_call event. The client's
//     Execute function is called with the search arguments and should return
//     matching tool names.
//
// Run with:
//
//	go run main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

func main() {
	fmt.Println("=== OpenAI Responses API: Tool Search Examples ===")
	fmt.Println()

	fmt.Println("--- Example 1: Server Mode (default) ---")
	serverModeExample()

	fmt.Println()

	fmt.Println("--- Example 2: Client Mode ---")
	clientModeExample()

	fmt.Println()

	fmt.Println("--- Example 3: Client Mode Execute Simulation ---")
	clientModeExecuteExample()
}

// serverModeExample shows a tool_search tool in server execution mode.
// OpenAI handles the search internally — no client-side Execute is needed.
func serverModeExample() {
	searchTool := openaitool.ToolSearch(openaitool.ToolSearchArgs{})

	prepared := responses.PrepareTools([]types.Tool{searchTool})
	data, err := json.MarshalIndent(prepared, "", "  ")
	if err != nil {
		log.Fatalf("marshal failed: %v", err)
	}
	fmt.Printf("Wire format (server mode):\n%s\n", data)
}

// clientModeExample shows a tool_search tool in client execution mode.
// The model emits tool_search_call events which are routed to the Execute function.
func clientModeExample() {
	searchTool := openaitool.ToolSearch(openaitool.ToolSearchArgs{
		Execution:   "client",
		Description: "Find tools matching a natural-language query",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Natural language description of the desired tool",
				},
			},
			"required": []string{"query"},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			query, _ := input["query"].(string)
			fmt.Printf("  Tool search query: %q (call_id: %s)\n", query, opts.ToolCallID)
			// Return tool names matching the query
			return []string{"get_weather", "search_web"}, nil
		},
	})

	prepared := responses.PrepareTools([]types.Tool{searchTool})
	data, err := json.MarshalIndent(prepared, "", "  ")
	if err != nil {
		log.Fatalf("marshal failed: %v", err)
	}
	fmt.Printf("Wire format (client mode):\n%s\n", data)
}

// clientModeExecuteExample demonstrates how the client Execute function is invoked
// when the model emits a tool_search_call event.
func clientModeExecuteExample() {
	searchTool := openaitool.ToolSearch(openaitool.ToolSearchArgs{
		Execution:   "client",
		Description: "Search for tools by capability",
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			query, _ := input["query"].(string)
			// In a real application, you'd search your registered tools here.
			if query == "weather" {
				return []string{"get_weather", "get_forecast"}, nil
			}
			return []string{}, nil
		},
	})

	// Simulate the model emitting a tool_search_call event
	ctx := context.Background()
	result, err := searchTool.Execute(ctx, map[string]interface{}{"query": "weather"}, types.ToolExecutionOptions{
		ToolCallID: "call_search_001",
	})
	if err != nil {
		log.Fatalf("Execute failed: %v", err)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Fatalf("marshal result failed: %v", err)
	}
	fmt.Printf("Execute result for query 'weather':\n%s\n", data)
}
