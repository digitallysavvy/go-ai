package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/youcom"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}

	provider := openai.New(openai.Config{APIKey: apiKey})
	model, err := provider.LanguageModel(openai.ModelGPT4oMini)
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Extract the main article content and outbound links from https://example.com.",
		Tools: []types.Tool{
			youcom.YouContents(),
		},
		MaxSteps: intPtr(3),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}

func intPtr(v int) *int {
	return &v
}
