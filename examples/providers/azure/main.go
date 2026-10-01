package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
)

func main() {
	if os.Getenv("AZURE_API_KEY") == "" || os.Getenv("AZURE_RESOURCE_NAME") == "" {
		fmt.Println("set AZURE_API_KEY and AZURE_RESOURCE_NAME to run")
		return
	}

	provider, err := azure.New(azure.Config{
		APIKey:       os.Getenv("AZURE_API_KEY"),
		ResourceName: os.Getenv("AZURE_RESOURCE_NAME"),
		APIVersion:   "v1",
	})
	if err != nil {
		log.Fatal(err)
	}

	model, err := provider.LanguageModel("gpt-5.4-mini")
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Write a short Azure OpenAI migration checklist.",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Text)
}
