//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
)

func main() {
	if os.Getenv("GO_AI_RUN_LIVE_EXAMPLES") != "1" {
		fmt.Println("Set GO_AI_RUN_LIVE_EXAMPLES=1 and ALIBABA_API_KEY to call Alibaba text embeddings.")
		return
	}
	apiKey := os.Getenv("ALIBABA_API_KEY")
	if apiKey == "" {
		fmt.Println("Set ALIBABA_API_KEY to call Alibaba text embeddings.")
		return
	}

	p := alibaba.New(alibaba.Config{APIKey: apiKey})
	model, err := p.EmbeddingModel(alibaba.AlibabaEmbeddingTextV4)
	if err != nil {
		log.Fatalf("create embedding model: %v", err)
	}

	result, err := ai.EmbedMany(context.Background(), ai.EmbedManyOptions{
		Model:  model,
		Inputs: []string{"red apple", "green apple", "city skyline"},
		ProviderOptions: map[string]interface{}{
			"alibaba": alibaba.AlibabaEmbeddingModelOptions{
				TextType:   "document",
				OutputType: alibaba.AlibabaEmbeddingOutputDense,
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("embeddings=%d dimensions=%d\n", len(result.Embeddings), len(result.Embeddings[0]))
}
