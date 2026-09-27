package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	gatewaytools "github.com/digitallysavvy/go-ai/pkg/providers/gateway/tools"
)

func main() {
	apiKey := os.Getenv("AI_GATEWAY_API_KEY")
	if apiKey == "" {
		log.Fatal("AI_GATEWAY_API_KEY is required")
	}

	provider, err := gateway.New(gateway.Config{APIKey: apiKey})
	if err != nil {
		log.Fatal(err)
	}
	model, err := provider.LanguageModel("openai/gpt-5")
	if err != nil {
		log.Fatal(err)
	}

	exaSearch := gatewaytools.NewExaSearch(gatewaytools.ExaSearchConfig{
		Type:       "auto",
		NumResults: intPtr(5),
		Category:   "news",
		Contents: &gatewaytools.ExaSearchContentsConfig{
			Text: true,
			Extras: &gatewaytools.ExaSearchExtrasConfig{
				Links: intPtr(2),
			},
		},
	})

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Find recent AI developer platform announcements and summarize the common themes.",
		Tools: []types.Tool{
			exaSearch.ToTool(),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Text)
}

func intPtr(v int) *int {
	return &v
}
