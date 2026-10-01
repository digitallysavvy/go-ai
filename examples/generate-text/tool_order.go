//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	if os.Getenv("GO_AI_RUN_LIVE_EXAMPLES") != "1" {
		fmt.Println("Set GO_AI_RUN_LIVE_EXAMPLES=1 and OPENAI_API_KEY to run the ToolOrder example.")
		return
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("Set OPENAI_API_KEY to run the ToolOrder example.")
		return
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, err := p.LanguageModel(openai.ModelGPT4oMini)
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:     model,
		Prompt:    "Use the best tool to inspect user state.",
		ToolOrder: []string{"readProfile", "listOrders"},
		Tools: []types.Tool{
			namedTool("listOrders", "List recent orders."),
			namedTool("readProfile", "Read the user profile."),
			namedTool("auditLog", "Read account audit logs."),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}

func namedTool(name, description string) types.Tool {
	return types.Tool{
		Name:        name,
		Description: description,
		Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"ok": true, "tool": name}, nil
		},
	}
}
