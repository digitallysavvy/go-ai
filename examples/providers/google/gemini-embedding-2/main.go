package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
)

func main() {
	if os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY") == "" {
		fmt.Println("set GOOGLE_GENERATIVE_AI_API_KEY to run")
		return
	}

	provider := google.New(google.Config{APIKey: os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")})
	model, err := provider.EmbeddingModel(google.EmbeddingModelGeminiEmbedding2)
	if err != nil {
		log.Fatal(err)
	}

	one, err := ai.Embed(context.Background(), ai.EmbedOptions{
		Model: model,
		Input: "sunny day at the beach",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("embedding length: %d\n", len(one.Embedding))

	many, err := ai.EmbedMany(context.Background(), ai.EmbedManyOptions{
		Model: model,
		Inputs: []string{
			"sunny day at the beach",
			"rainy afternoon in the city",
			"snowy night in the mountains",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("batch embeddings: %d\n", len(many.Embeddings))
}
