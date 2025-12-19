package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

type Agent struct {
	name  string
	role  string
	tools []types.Tool
	model provider.LanguageModel
}

func (a *Agent) Process(ctx context.Context, task string) (string, error) {
	fmt.Printf("\n[%s] Processing: %s\n", a.name, task)

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  a.model,
		Prompt: task,
		System: fmt.Sprintf("You are %s. %s", a.name, a.role),
		Tools:  a.tools,
	})

	if err != nil {
		return "", err
	}

	return result.Text, nil
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, _ := p.LanguageModel("gpt-4")

	// Create multiple agents with different roles
	researcher := &Agent{
		name:  "Researcher",
		role:  "Research topics and gather information",
		model: model,
	}

	writer := &Agent{
		name:  "Writer",
		role:  "Write clear, concise content",
		model: model,
	}

	reviewer := &Agent{
		name:  "Reviewer",
		role:  "Review and improve content quality",
		model: model,
	}

	ctx := context.Background()

	fmt.Println("=== Multi-Agent System ===")

	// Agent 1: Research
	research, _ := researcher.Process(ctx, "Research the benefits of Go programming language")

	// Agent 2: Write
	draft, _ := writer.Process(ctx, fmt.Sprintf("Write a short article based on: %s", research))

	// Agent 3: Review
	final, _ := reviewer.Process(ctx, fmt.Sprintf("Review and improve: %s", draft))

	fmt.Println("\n=== Final Output ===")
	fmt.Println(final)
}
