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
	if os.Getenv("ANTHROPIC_AWS_API_KEY") == "" || os.Getenv("ANTHROPIC_AWS_WORKSPACE_ID") == "" || os.Getenv("AWS_REGION") == "" {
		fmt.Println("set ANTHROPIC_AWS_API_KEY, ANTHROPIC_AWS_WORKSPACE_ID, and AWS_REGION to run")
		return
	}

	provider, err := anthropicaws.New(anthropicaws.Config{
		Region:      os.Getenv("AWS_REGION"),
		WorkspaceID: os.Getenv("ANTHROPIC_AWS_WORKSPACE_ID"),
		APIKey:      os.Getenv("ANTHROPIC_AWS_API_KEY"),
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
		Prompt: "Write one sentence about Claude Platform on AWS.",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}
