// Package main demonstrates the OpenAI Responses API custom tool with grammar format.
//
// A custom tool constrains the model's output to a specific format (text or grammar).
// This example shows:
//   - Building a custom tool with a grammar format (lark syntax)
//   - Building a custom tool with regex grammar
//   - Serializing tools to the wire format expected by the OpenAI Responses API
//
// Run with:
//
//	go run main.go
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

func main() {
	fmt.Println("=== OpenAI Responses API: Custom Tool Examples ===")
	fmt.Println()

	// Example 1: Custom tool with a Lark grammar
	fmt.Println("--- Example 1: Custom Tool with Lark Grammar ---")
	larkGrammarExample()

	fmt.Println()

	// Example 2: Custom tool with regex grammar
	fmt.Println("--- Example 2: Custom Tool with Regex Grammar ---")
	regexGrammarExample()

	fmt.Println()

	// Example 3: Custom tool with text format (no grammar constraints)
	fmt.Println("--- Example 3: Custom Tool with Text Format ---")
	textFormatExample()

	fmt.Println()

	// Example 4: Mixed tools — function + custom
	fmt.Println("--- Example 4: Mixed Function and Custom Tools ---")
	mixedToolsExample()
}

// larkGrammarExample shows a custom tool that constrains output to a Lark grammar.
// The model will produce output matching the grammar definition.
func larkGrammarExample() {
	syntax := "lark"
	definition := `
// Grammar for JSON with only string values
start: object
object: "{" pair ("," pair)* "}"
pair: STRING ":" STRING
STRING: "\"" /[^"]*/ "\""
`

	ct := openaitool.NewCustomTool("json-extractor",
		openaitool.WithDescription("Extract key-value pairs from text as JSON"),
		openaitool.WithFormat(openaitool.CustomToolFormat{
			Type:       "grammar",
			Syntax:     &syntax,
			Definition: &definition,
		}),
	)

	printToolWireFormat("json-extractor (lark grammar)", ct.ToTool())
}

// regexGrammarExample shows a custom tool using a regex grammar to constrain output.
func regexGrammarExample() {
	syntax := "regex"
	definition := `^\d{4}-\d{2}-\d{2}$` // ISO date format: YYYY-MM-DD

	ct := openaitool.NewCustomTool("date-extractor",
		openaitool.WithDescription("Extract a date from the text in YYYY-MM-DD format"),
		openaitool.WithFormat(openaitool.CustomToolFormat{
			Type:       "grammar",
			Syntax:     &syntax,
			Definition: &definition,
		}),
	)

	printToolWireFormat("date-extractor (regex grammar)", ct.ToTool())
}

// textFormatExample shows a custom tool with unconstrained text output.
func textFormatExample() {
	ct := openaitool.NewCustomTool("sentiment-analyzer",
		openaitool.WithDescription("Analyze the sentiment of the provided text"),
		openaitool.WithFormat(openaitool.CustomToolFormat{
			Type: "text",
		}),
	)

	printToolWireFormat("sentiment-analyzer (text format)", ct.ToTool())
}

// mixedToolsExample shows PrepareTools handling both standard functions and custom tools.
func mixedToolsExample() {
	ct := openaitool.NewCustomTool("json-extractor",
		openaitool.WithDescription("Extract JSON from text"),
		openaitool.WithFormat(openaitool.CustomToolFormat{Type: "text"}),
	)

	allTools := []types.Tool{
		// Standard function tool
		{
			Name:        "get_weather",
			Description: "Get the current weather for a location",
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
		},
		// Custom tool
		ct.ToTool(),
	}

	prepared := responses.PrepareTools(allTools)
	data, err := json.MarshalIndent(prepared, "", "  ")
	if err != nil {
		log.Fatalf("marshal failed: %v", err)
	}
	fmt.Printf("Wire format for %d tools:\n%s\n", len(prepared), data)
}

// printToolWireFormat shows the JSON wire format for a single tool.
func printToolWireFormat(label string, tool types.Tool) {
	prepared := responses.PrepareTools([]types.Tool{tool})
	if len(prepared) == 0 {
		fmt.Printf("%s: (no tools prepared)\n", label)
		return
	}

	data, err := json.MarshalIndent(prepared[0], "", "  ")
	if err != nil {
		log.Fatalf("marshal failed: %v", err)
	}
	fmt.Printf("Wire format for %s:\n%s\n", label, data)
}
