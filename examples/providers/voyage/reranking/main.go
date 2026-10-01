//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	goprovider "github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/voyage"
)

func main() {
	provider := voyage.New(voyage.Config{APIKey: os.Getenv("VOYAGE_API_KEY")})
	ranker, err := provider.RerankingModel(voyage.ModelRerank25)
	if err != nil {
		log.Fatal(err)
	}

	topN := 2
	result, err := ranker.DoRerank(context.Background(), &goprovider.RerankOptions{
		Query: "go concurrency",
		Documents: []string{
			"Go has goroutines and channels.",
			"SQL uses joins to combine rows.",
			"Concurrency can improve throughput.",
		},
		TopN: &topN,
	})
	if err != nil {
		log.Fatal(err)
	}

	for i, item := range result.Ranking {
		fmt.Printf("%d: doc[%d] score=%.4f\n", i+1, item.Index, item.RelevanceScore)
	}
}
