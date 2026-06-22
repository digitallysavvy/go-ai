//go:build ignore

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
	if os.Getenv("GO_AI_RUN_LIVE_EXAMPLES") != "1" {
		fmt.Println("Set GO_AI_RUN_LIVE_EXAMPLES=1 with Azure settings to call Azure OpenAI with Entra ID.")
		return
	}
	resource := os.Getenv("AZURE_RESOURCE_NAME")
	deployment := os.Getenv("AZURE_OPENAI_DEPLOYMENT")
	token := os.Getenv("AZURE_OPENAI_ENTRA_TOKEN")
	if resource == "" || deployment == "" || token == "" {
		fmt.Println("Set AZURE_RESOURCE_NAME, AZURE_OPENAI_DEPLOYMENT, and AZURE_OPENAI_ENTRA_TOKEN to call Azure OpenAI with Entra ID.")
		return
	}

	p, err := azure.New(azure.Config{
		ResourceName: resource,
		ADTokenProvider: func(ctx context.Context) (string, error) {
			return token, nil
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	model, err := p.LanguageModel(deployment)
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Write one sentence about Entra ID authentication.",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}
