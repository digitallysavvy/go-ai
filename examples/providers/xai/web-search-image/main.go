package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

func main() {
	if os.Getenv("XAI_API_KEY") == "" {
		fmt.Println("set XAI_API_KEY to run")
		return
	}

	provider := xai.New(xai.Config{APIKey: os.Getenv("XAI_API_KEY")})
	model, err := provider.LanguageModel(xai.ModelGrok43)
	if err != nil {
		log.Fatal(err)
	}

	enableImageSearch := true
	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Show me images of SpaceX Starship on the launch pad.",
		Tools: []types.Tool{
			xai.WebSearch(xai.WebSearchConfig{
				EnableImageSearch: &enableImageSearch,
			}),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}
