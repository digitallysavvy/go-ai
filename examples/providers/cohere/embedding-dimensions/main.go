package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
)

func main() {
	apiKey := os.Getenv("COHERE_API_KEY")
	if apiKey == "" {
		log.Fatal("COHERE_API_KEY environment variable is required")
	}

	ctx := context.Background()
	provider := cohere.New(cohere.Config{
		APIKey: apiKey,
	})

	// Example 1: Default dimensions (model default, typically 1024)
	fmt.Println("=== Example 1: Default Dimensions ===")
	defaultModel, err := provider.EmbeddingModel("embed-english-v3.0")
	if err != nil {
		log.Fatalf("Failed to create default model: %v", err)
	}

	result1, err := ai.Embed(ctx, ai.EmbedOptions{
		Model: defaultModel,
		Input: "The quick brown fox jumps over the lazy dog",
	})
	if err != nil {
		log.Fatalf("Failed to generate default embedding: %v", err)
	}
	fmt.Printf("Default embedding dimension: %d\n", len(result1.Embedding))
	fmt.Printf("Tokens used: %d\n\n", result1.Usage.InputTokens)

	// Example 2: Small dimension (256) for cost/storage optimization
	fmt.Println("=== Example 2: Small Dimension (256) ===")
	dim256 := cohere.Dimension256
	smallModel, err := provider.EmbeddingModelWithOptions("embed-english-v3.0", cohere.EmbeddingOptions{
		OutputDimension: &dim256,
		InputType:       cohere.InputTypeSearchDocument,
	})
	if err != nil {
		log.Fatalf("Failed to create small dimension model: %v", err)
	}

	result2, err := ai.Embed(ctx, ai.EmbedOptions{
		Model: smallModel,
		Input: "Machine learning is a subset of AI",
	})
	if err != nil {
		log.Fatalf("Failed to generate small embedding: %v", err)
	}
	fmt.Printf("Small embedding dimension: %d\n", len(result2.Embedding))
	fmt.Printf("Tokens used: %d\n\n", result2.Usage.InputTokens)

	// Example 3: Medium dimension (512) for balanced performance
	fmt.Println("=== Example 3: Medium Dimension (512) ===")
	dim512 := cohere.Dimension512
	mediumModel, err := provider.EmbeddingModelWithOptions("embed-english-v3.0", cohere.EmbeddingOptions{
		OutputDimension: &dim512,
		InputType:       cohere.InputTypeSearchQuery,
	})
	if err != nil {
		log.Fatalf("Failed to create medium dimension model: %v", err)
	}

	result3, err := ai.Embed(ctx, ai.EmbedOptions{
		Model: mediumModel,
		Input: "What is the capital of France?",
	})
	if err != nil {
		log.Fatalf("Failed to generate medium embedding: %v", err)
	}
	fmt.Printf("Medium embedding dimension: %d\n", len(result3.Embedding))
	fmt.Printf("Tokens used: %d\n\n", result3.Usage.InputTokens)

	// Example 4: Large dimension (1536) for maximum semantic information
	fmt.Println("=== Example 4: Large Dimension (1536) ===")
	dim1536 := cohere.Dimension1536
	largeModel, err := provider.EmbeddingModelWithOptions("embed-english-v3.0", cohere.EmbeddingOptions{
		OutputDimension: &dim1536,
		InputType:       cohere.InputTypeClustering,
	})
	if err != nil {
		log.Fatalf("Failed to create large dimension model: %v", err)
	}

	result4, err := ai.Embed(ctx, ai.EmbedOptions{
		Model: largeModel,
		Input: "Deep learning uses neural networks to model complex patterns",
	})
	if err != nil {
		log.Fatalf("Failed to generate large embedding: %v", err)
	}
	fmt.Printf("Large embedding dimension: %d\n", len(result4.Embedding))
	fmt.Printf("Tokens used: %d\n\n", result4.Usage.InputTokens)

	// Example 5: Batch embeddings with custom dimensions
	fmt.Println("=== Example 5: Batch Embeddings with Custom Dimensions ===")
	dim1024 := cohere.Dimension1024
	batchModel, err := provider.EmbeddingModelWithOptions("embed-english-v3.0", cohere.EmbeddingOptions{
		OutputDimension: &dim1024,
		InputType:       cohere.InputTypeClassification,
		Truncate:        cohere.TruncateEnd,
	})
	if err != nil {
		log.Fatalf("Failed to create batch model: %v", err)
	}

	texts := []string{
		"Machine learning is a subset of AI",
		"Deep learning uses neural networks",
		"Natural language processing handles text",
	}

	result5, err := ai.EmbedMany(ctx, ai.EmbedManyOptions{
		Model:  batchModel,
		Inputs: texts,
	})
	if err != nil {
		log.Fatalf("Failed to generate batch embeddings: %v", err)
	}

	for i, emb := range result5.Embeddings {
		fmt.Printf("Text %d: %d dimensions\n", i+1, len(emb))
	}
	fmt.Printf("Total tokens used: %d\n", result5.Usage.InputTokens)
}
