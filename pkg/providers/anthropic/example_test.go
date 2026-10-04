package anthropic_test

import (
	"context"
	"fmt"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// This example needs ANTHROPIC_API_KEY and network access, so it compiles but does not run.
func ExampleNew() {
	ctx := context.Background()

	provider := anthropic.New(anthropic.Config{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
	})
	model, err := provider.LanguageModel(anthropic.ClaudeSonnet5_5)
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
