// Command codemode demonstrates pkg/codemode: running model-written
// JavaScript in an isolated QuickJS-WASM sandbox (via wazero, pure Go, no
// cgo) that calls host tools programmatically instead of one tool call per
// model turn.
//
// This example needs no API key or network access: it calls
// codemode.RunCodeMode directly with two local host tools, then shows how
// to wire codemode.CodeModeTool into ai.ExperimentalToolCallers so a real
// model could drive the same sandbox through generateText/streamText.
//
// Run with: go run ./examples/codemode
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/codemode"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func main() {
	demoDirectRun()
	demoToolCallerWiring()
}

// demoDirectRun runs a small code-mode program directly, the way you might
// from a test or a non-agent workflow. The program calls two host tools
// and combines their results with Promise.all.
func demoDirectRun() {
	tools := codemode.ToolSet{
		"weather": {
			Name:        "weather",
			Description: "Look up the current temperature for a city.",
			Parameters: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"city"},
				"properties": map[string]interface{}{
					"city": map[string]interface{}{"type": "string"},
				},
			},
			OutputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"tempC"},
				"properties": map[string]interface{}{
					"tempC": map[string]interface{}{"type": "number"},
				},
			},
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				city, _ := input["city"].(string)
				temps := map[string]float64{"Berlin": 12, "Nairobi": 24, "Oslo": 3}
				temp, ok := temps[city]
				if !ok {
					temp = 15
				}
				return map[string]interface{}{"tempC": temp}, nil
			},
		},
		"convertCToF": {
			Name:        "convertCToF",
			Description: "Convert a Celsius temperature to Fahrenheit.",
			Parameters: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"c"},
				"properties": map[string]interface{}{
					"c": map[string]interface{}{"type": "number"},
				},
			},
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				c, _ := input["c"].(float64)
				return map[string]interface{}{"f": c*9/5 + 32}, nil
			},
		},
	}

	// The model would normally write this; here it stands in for one.
	js := `
		const cities = ["Berlin", "Nairobi", "Oslo"];
		const results = await Promise.all(cities.map(async (city) => {
			const { tempC } = await tools.weather({ city });
			const { f } = await tools.convertCToF({ c: tempC });
			return { city, tempC, tempF: f };
		}));
		return results;
	`

	result, err := codemode.RunCodeMode(context.Background(), codemode.RunInput{
		JS:    js,
		Tools: tools,
	})
	if err != nil {
		log.Fatalf("codemode.RunCodeMode: %v", err)
	}
	fmt.Println("Direct RunCodeMode result:")
	fmt.Printf("  %#v\n\n", result)
}

// demoToolCallerWiring shows the shape you'd use to let a real model
// choose between calling a tool directly and calling it through code mode,
// via the P1-2c local tool-caller API (ai.ExperimentalToolCallers). No
// model call is made here since that needs a configured provider; see the
// printed tools/config for what you'd pass to ai.GenerateText /
// ai.StreamText.
func demoToolCallerWiring() {
	weather := types.Tool{
		Name:        "weather",
		Description: "Look up the current temperature for a city.",
		Parameters: map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"city"},
			"properties": map[string]interface{}{
				"city": map[string]interface{}{"type": "string"},
			},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"tempC": 12}, nil
		},
	}

	codeMode := codemode.CodeModeTool(codemode.ToolCallerOptions{})

	toolCallers := ai.ExperimentalToolCallers{
		// The model may call "weather" directly, or route it through the
		// "codeMode" caller so a single tool call can drive several
		// weather lookups from one script.
		"weather": {ai.DirectToolCall, codemode.DefaultToolName},
	}

	tools := []types.Tool{weather, codeMode}
	resolved, err := ai.ResolveToolCallerConfiguration(tools, toolCallers)
	if err != nil {
		log.Fatalf("ai.ResolveToolCallerConfiguration: %v", err)
	}
	executionTools, modelTools, _ := ai.PrepareToolsForToolCallers(tools, resolved)

	fmt.Println("Tool-caller wiring for ai.GenerateText/ai.StreamText:")
	fmt.Printf("  tools exposed to the model:    %v\n", toolNames(modelTools))
	fmt.Printf("  tools available for execution: %v\n", toolNames(executionTools))
	fmt.Println("  (pass executionTools as the Tools option and toolCallers via" +
		" ai.ExperimentalToolCallers to a real generateText/streamText call)")
}

func toolNames(tools []types.Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}
