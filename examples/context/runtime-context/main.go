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
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func main() {
	p := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
	model, err := p.LanguageModel(openai.ModelGPT4oMini)
	if err != nil {
		log.Fatal(err)
	}

	weather := types.Tool{
		Name:        "weather",
		Description: "Return weather for the configured city.",
		Parameters:  schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{
				"runtime": opts.RuntimeContext,
				"tool":    opts.ToolContext,
				"summary": "sunny",
			}, nil
		},
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:          model,
		Prompt:         "Check the weather.",
		Tools:          []types.Tool{weather},
		RuntimeContext: map[string]interface{}{"request_id": "req_123", "user_id": "user_42"},
		ToolsContext: map[string]interface{}{
			"weather": map[string]interface{}{"city": "Lisbon", "units": "metric"},
		},
		StopWhen: []ai.StopCondition{ai.IsStepCount(3)},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Text)
}
