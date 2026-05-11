//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	goprovider "github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
)

func main() {
	p, err := gateway.New(gateway.Config{
		APIKey:        os.Getenv("AI_GATEWAY_API_KEY"),
		QuotaEntityID: "tenant_123",
	})
	if err != nil {
		log.Fatal(err)
	}

	ranker, err := p.RerankingModel("cohere/rerank-v3.5")
	if err != nil {
		log.Fatal(err)
	}

	topN := 2
	result, err := ranker.DoRerank(context.Background(), &goprovider.RerankOptions{
		Query: "go concurrency",
		Documents: []string{
			"Goroutines are lightweight units of concurrency.",
			"SQL joins combine table rows.",
			"Channels let goroutines communicate.",
		},
		TopN: &topN,
		ProviderOptions: gateway.GatewayProviderOptions{
			Sort:          "price",
			QuotaEntityID: "tenant_123",
		}.ToProviderOptions(),
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, item := range result.Ranking {
		fmt.Printf("doc[%d] score=%.4f\n", item.Index, item.RelevanceScore)
	}
}
