package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// Example demonstrating tool result content arrays and tool references

func main() {
	// Example 1: Simple content blocks
	fmt.Println("=== Example 1: Simple Content Blocks ===")
	simpleContentExample()

	// Example 2: Mixed content (text + images)
	fmt.Println("\n=== Example 2: Mixed Content ===")
	mixedContentExample()

	// Example 3: Tool references
	fmt.Println("\n=== Example 3: Tool References ===")
	toolReferenceExample()

	// Example 4: Error handling
	fmt.Println("\n=== Example 4: Error Handling ===")
	errorHandlingExample()
}

func simpleContentExample() {
	// Old style - still supported
	oldStyleResult := types.SimpleTextResult("call_1", "search", "Found 3 results")
	fmt.Printf("Old style: %+v\n", oldStyleResult)

	// New style - recommended
	newStyleResult := types.ContentResult("call_2", "search",
		types.TextContentBlock{Text: "Search results:"},
		types.TextContentBlock{Text: "1. First result"},
		types.TextContentBlock{Text: "2. Second result"},
		types.TextContentBlock{Text: "3. Third result"},
	)
	fmt.Printf("New style: %+v\n", newStyleResult.Output)
}

func mixedContentExample() {
	// Simulated image and file data
	imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
	pdfData := []byte{0x25, 0x50, 0x44, 0x46}   // PDF header

	result := types.ContentResult("call_3", "analyze",
		types.TextContentBlock{Text: "Analysis complete"},
		types.ImageContentBlock{
			Data:      imageData,
			MediaType: "image/png",
		},
		types.FileContentBlock{
			Data:      pdfData,
			MediaType: "application/pdf",
			Filename:  "report.pdf",
		},
		types.TextContentBlock{Text: "See attached chart and report"},
	)

	fmt.Printf("Mixed content result with %d blocks\n", len(result.Output.Content))
	for i, block := range result.Output.Content {
		fmt.Printf("  Block %d: %s\n", i+1, block.ToolResultContentType())
	}
}

func toolReferenceExample() {
	// Create tool references
	result := types.ContentResult("call_4", "search_tools",
		types.TextContentBlock{Text: "Found 3 math tools:"},
		anthropic.ToolReference("add"),
		anthropic.ToolReference("multiply"),
		anthropic.ToolReference("divide"),
	)

	fmt.Printf("Tool reference result with %d blocks\n", len(result.Output.Content))

	// Extract tool references
	toolNames := anthropic.ExtractToolReferences(result)
	fmt.Printf("Referenced tools: %v\n", strings.Join(toolNames, ", "))

	// Inspect individual blocks
	for i, block := range result.Output.Content {
		if customBlock, ok := block.(types.CustomContentBlock); ok {
			if toolName, isRef := anthropic.IsToolReference(customBlock); isRef {
				fmt.Printf("  Block %d is tool reference: %s\n", i+1, toolName)
			}
		}
	}
}

func errorHandlingExample() {
	// Error result
	errorResult := types.ErrorResult("call_5", "broken_tool", "Network timeout")
	fmt.Printf("Error result: is_error=%v, message=%s\n",
		errorResult.Error != "",
		errorResult.Error,
	)

	// Success result for comparison
	successResult := types.ContentResult("call_6", "working_tool",
		types.TextContentBlock{Text: "Operation completed successfully"},
	)
	fmt.Printf("Success result: is_error=%v\n", successResult.Error != "")
}

// Example tool implementations

func createSearchTool() types.Tool {
	return types.Tool{
		Name:        "search",
		Description: "Search for information",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query",
				},
			},
			"required": []string{"query"},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			query := input["query"].(string)

			return types.ContentResult(opts.ToolCallID, "search",
				types.TextContentBlock{Text: fmt.Sprintf("Searching for: %s", query)},
				types.TextContentBlock{Text: "Found 3 results:"},
				types.TextContentBlock{Text: "1. First result"},
				types.TextContentBlock{Text: "2. Second result"},
				types.TextContentBlock{Text: "3. Third result"},
			), nil
		},
	}
}

func createToolSearchTool(availableTools []types.Tool) types.Tool {
	return types.Tool{
		Name:        "search_tools",
		Description: "Search for available tools",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query",
				},
			},
			"required": []string{"query"},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			query := strings.ToLower(input["query"].(string))

			// Search for matching tools
			var matchingTools []string
			for _, tool := range availableTools {
				if strings.Contains(strings.ToLower(tool.Name), query) ||
					strings.Contains(strings.ToLower(tool.Description), query) {
					matchingTools = append(matchingTools, tool.Name)
				}
			}

			if len(matchingTools) == 0 {
				return types.ContentResult(opts.ToolCallID, "search_tools",
					types.TextContentBlock{Text: "No tools found"},
				), nil
			}

			// Build result with tool references
			blocks := []types.ToolResultContentBlock{
				types.TextContentBlock{
					Text: fmt.Sprintf("Found %d tool(s):", len(matchingTools)),
				},
			}

			for _, toolName := range matchingTools {
				blocks = append(blocks, anthropic.ToolReference(toolName))
			}

			return types.ContentResult(opts.ToolCallID, "search_tools", blocks...), nil
		},
	}
}

func createAnalyzeTool() types.Tool {
	return types.Tool{
		Name:        "analyze",
		Description: "Analyze data and generate report",
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			// Simulate analysis
			chartData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
			reportData := []byte{0x25, 0x50, 0x44, 0x46} // PDF header

			return types.ContentResult(opts.ToolCallID, "analyze",
				types.TextContentBlock{Text: "Analysis complete:"},
				types.TextContentBlock{Text: "- Processed 1000 records"},
				types.TextContentBlock{Text: "- Found 5 outliers"},
				types.ImageContentBlock{
					Data:      chartData,
					MediaType: "image/png",
				},
				types.FileContentBlock{
					Data:      reportData,
					MediaType: "application/pdf",
					Filename:  "analysis-report.pdf",
				},
				types.TextContentBlock{Text: "See attached chart and report for details"},
			), nil
		},
	}
}
