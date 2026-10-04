package openai_test

import (
	"context"
	"fmt"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// This example needs OPENAI_API_KEY and network access, so it compiles but does not run.
func ExampleNew() {
	ctx := context.Background()

	provider := openai.New(openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	model, err := provider.LanguageModel(openai.ModelGPT6Astra)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Name one benefit of Go for backend services.",
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(result.Text)
}
