package main

import (
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

