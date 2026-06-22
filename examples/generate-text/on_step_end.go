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
		fmt.Println("Set GO_AI_RUN_LIVE_EXAMPLES=1 and OPENAI_API_KEY to run the OnStepEnd example.")
		return
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("Set OPENAI_API_KEY to run the OnStepEnd example.")
		return
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, err := p.LanguageModel(openai.ModelGPT4oMini)
	if err != nil {
		log.Fatal(err)
	}

	maxSteps := 2
	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:    model,
		Prompt:   "Get the current inventory count, then summarize it.",
		MaxSteps: &maxSteps,
		Tools: []types.Tool{{
			Name:        "inventory",
			Description: "Return inventory count for a SKU.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"sku": map[string]interface{}{"type": "string"},
				},
				"required": []string{"sku"},
			},
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"sku": input["sku"], "count": 42}, nil
			},
		}},
		OnStepEnd: func(ctx context.Context, step types.StepResult, userContext interface{}) {
			fmt.Printf("step=%d finish=%s input=%v output=%v\n",
				step.StepNumber, step.FinishReason, step.Usage.InputTokens, step.Usage.OutputTokens)
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}
