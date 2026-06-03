package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
)

func main() {
	if os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY") == "" {
		fmt.Println("set GOOGLE_GENERATIVE_AI_API_KEY to run")
		return
	}

	provider := google.New(google.Config{APIKey: os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")})
	model, err := provider.InteractionsAgent(google.InteractionsAgentDeepResearchPreview042026)
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Research three tradeoffs of retrieval augmented generation. Keep it brief.",
		ProviderOptions: map[string]interface{}{
			"google": google.GoogleInteractionsProviderOptions{
				PollingTimeoutMs: 120000,
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}
