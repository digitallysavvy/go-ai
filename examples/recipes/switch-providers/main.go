// Recipe: change providers without changing code.
//
// Run: PROVIDER_MODEL=anthropic:claude-sonnet-5-5 ANTHROPIC_API_KEY=... go run ./examples/recipes/switch-providers
// Or:  PROVIDER_MODEL=openai:gpt-6-astra OPENAI_API_KEY=... go run ./examples/recipes/switch-providers
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/registry"
)

func main() {
	// Register each provider once, at startup.
	registry.RegisterProvider("anthropic", anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")}))
	registry.RegisterProvider("openai", openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")}))
	registry.RegisterProvider("google", google.New(google.Config{APIKey: os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")}))

	// The rest of the program only sees a "provider:model" string.
	id := os.Getenv("PROVIDER_MODEL")
	if id == "" {
		id = "anthropic:" + anthropic.ClaudeSonnet5_5
	}
	model, err := registry.ResolveLanguageModel(id)
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Say hello in one short sentence.",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s: %s\n", id, result.Text)
}
