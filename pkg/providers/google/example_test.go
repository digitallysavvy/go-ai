package google_test

import (
	"context"
	"fmt"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
)

// This example needs GOOGLE_GENERATIVE_AI_API_KEY and network access, so it compiles but does not run.
func ExampleNew() {
	ctx := context.Background()

	provider := google.New(google.Config{
		APIKey: os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY"),
	})
	model, err := provider.LanguageModel(google.ModelGemini31FlashLitePreview)
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
