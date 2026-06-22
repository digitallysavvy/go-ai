//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/policy"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	if os.Getenv("GO_AI_RUN_LIVE_EXAMPLES") != "1" {
		fmt.Println("Set GO_AI_RUN_LIVE_EXAMPLES=1, OPENAI_API_KEY, and OPA_URL to run tool approval against OPA.")
		return
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	opaURL := os.Getenv("OPA_URL")
	if apiKey == "" || opaURL == "" {
		fmt.Println("Set OPENAI_API_KEY and OPA_URL to run tool approval against OPA.")
		return
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, err := p.LanguageModel(openai.ModelGPT4oMini)
	if err != nil {
		log.Fatal(err)
	}

	client := policy.HTTPPolicyClient(opaURL)
	approval := policy.OPAPolicy(client, policy.OPAPolicyOptions{Path: "ai/tool/approval"})

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:        model,
		Prompt:       "Check the deployment status.",
		ToolApproval: approval,
		Tools: []types.Tool{{
			Name:        "deploymentStatus",
			Description: "Return deployment status for an environment.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"environment": map[string]interface{}{"type": "string"},
				},
				"required": []string{"environment"},
			},
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"status": "healthy"}, nil
			},
		}},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}
