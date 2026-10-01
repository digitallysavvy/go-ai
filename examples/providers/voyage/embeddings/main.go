//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/providers/voyage"
)

func main() {
	provider := voyage.New(voyage.Config{APIKey: os.Getenv("VOYAGE_API_KEY")})
	model, err := provider.EmbeddingModel(voyage.ModelVoyage3Large)
	if err != nil {
		log.Fatal(err)
	}

	result, err := model.DoEmbedMany(context.Background(), []string{
		"Go supports concurrency with goroutines.",
		"Embeddings are useful for semantic retrieval.",
	}, nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Embeddings: %d vectors\n", len(result.Embeddings))
	fmt.Printf("Total tokens: %d\n", result.Usage.TotalTokens)
}
