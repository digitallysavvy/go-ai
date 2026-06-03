package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

func main() {
	if os.Getenv("AZURE_API_KEY") == "" || os.Getenv("AZURE_RESOURCE_NAME") == "" {
		fmt.Println("set AZURE_API_KEY and AZURE_RESOURCE_NAME to run")
		return
	}

	provider := azure.New(azure.Config{
		APIKey:       os.Getenv("AZURE_API_KEY"),
		ResourceName: os.Getenv("AZURE_RESOURCE_NAME"),
		APIVersion:   env("AZURE_OPENAI_API_VERSION", "v1"),
	})
	model, err := provider.LanguageModel(env("AZURE_OPENAI_DEPLOYMENT", "gpt-5.4-mini"))
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.StreamText(context.Background(), ai.StreamTextOptions{
		Model:  model,
		Prompt: "Summarize how to upgrade AI SDK from 5 to 6.",
		Tools: []types.Tool{
			azure.WebSearch(openaitool.WebSearchConfig{
				SearchContextSize: "low",
				Filters: &openaitool.WebSearchFilters{
					AllowedDomains: []string{"ai-sdk.dev"},
				},
			}),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer result.Close()

	for chunk := range result.Chunks() {
		fmt.Print(chunk.Text)
	}
	if err := result.Err(); err != nil {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
