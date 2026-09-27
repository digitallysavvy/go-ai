//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/quiverai"
)

// This example demonstrates QuiverAI's caller-executed "custom" tool
// support: a provider-defined tool (id "quiverai.custom") whose input the
// model streams as raw text rather than JSON function arguments -- useful
// for asking the model to return freeform text such as SVG markup.
// Prerequisites:
// 1. A QuiverAI API key, set via QUIVERAI_API_KEY or quiverai.Config.APIKey.

func main() {
	provider := quiverai.New(quiverai.Config{})

	model, err := provider.LanguageModel(quiverai.ModelArrow2)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// A "provider" tool with ProviderID "quiverai.custom" is encoded as a
	// custom tool (type: "custom") in the QuiverAI Responses request instead
	// of a JSON-schema function tool. args.description and args.format
	// (either {type:"text"} or {type:"grammar", syntax, definition}) are
	// forwarded to the API.
	svgTool := types.Tool{
		Type:       "provider",
		ProviderID: "quiverai.custom",
		Name:       "write_svg",
		ProviderArgs: map[string]interface{}{
			"description": "Return raw SVG markup for the requested icon.",
			"format":      map[string]interface{}{"type": "text"},
		},
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:      model,
		Prompt:     "Draw a minimal sun icon.",
		Tools:      []types.Tool{svgTool},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "write_svg"},
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, call := range result.ToolCalls {
		fmt.Printf("Tool: %s\n", call.ToolName)
		fmt.Printf("Input: %s\n", call.RawArguments)
	}
}
