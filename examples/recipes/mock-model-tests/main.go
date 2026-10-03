// Recipe: test code that calls a model, with no API key and no network.
//
// Run the tests: go test ./examples/recipes/mock-model-tests
// Run the program (needs OPENAI_API_KEY): go run ./examples/recipes/mock-model-tests
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Summarize is the code under test. It takes the model as a parameter, so a
// test can pass a mock.
func Summarize(ctx context.Context, model provider.LanguageModel, text string) (string, error) {
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: "Summarize the text in one sentence.",
		Prompt: text,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Text), nil
}

func main() {
	model, err := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")}).
		LanguageModel(openai.ModelGPT6Astra)
	if err != nil {
		log.Fatal(err)
	}
	summary, err := Summarize(context.Background(), model, "Go is a statically typed, compiled language designed at Google.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(summary)
}
