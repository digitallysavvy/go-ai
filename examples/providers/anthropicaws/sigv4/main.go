package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropicaws"
)

func main() {
	if os.Getenv("ANTHROPIC_AWS_WORKSPACE_ID") == "" || os.Getenv("AWS_REGION") == "" || os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
		fmt.Println("set ANTHROPIC_AWS_WORKSPACE_ID, AWS_REGION, AWS_ACCESS_KEY_ID, and AWS_SECRET_ACCESS_KEY to run")
		return
	}

	provider, err := anthropicaws.New(anthropicaws.Config{
		Region:          os.Getenv("AWS_REGION"),
		WorkspaceID:     os.Getenv("ANTHROPIC_AWS_WORKSPACE_ID"),
		AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
	})
	if err != nil {
		log.Fatal(err)
	}

	model, err := provider.LanguageModel(anthropic.ClaudeOpus4_8)
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Summarize SigV4 authentication in one sentence.",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}
