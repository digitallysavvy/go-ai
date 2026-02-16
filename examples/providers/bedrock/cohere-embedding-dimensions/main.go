package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
)

func main() {
	// AWS credentials from environment
	accessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	region := os.Getenv("AWS_REGION")

	if accessKeyID == "" || secretAccessKey == "" || region == "" {
		log.Fatal("AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, and AWS_REGION environment variables are required")
	}

	ctx := context.Background()
	provider := bedrock.New(bedrock.Config{
		AWSAccessKeyID:     accessKeyID,
		AWSSecretAccessKey: secretAccessKey,
		Region:             region,
	})

	// Example 1: Default dimensions (model default)
	fmt.Println("=== Example 1: Default Dimensions ===")
	defaultModel, err := provider.EmbeddingModel("cohere.embed-english-v3")
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
	smallOptions := &bedrock.EmbeddingOptions{
		CohereOptions: &bedrock.CohereEmbeddingOptions{
			OutputDimension: &dim256,
			InputType:       cohere.InputTypeSearchDocument,
		},
	}

	smallModel, err := provider.EmbeddingModelWithOptions("cohere.embed-english-v3", smallOptions)
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
	mediumOptions := &bedrock.EmbeddingOptions{
		CohereOptions: &bedrock.CohereEmbeddingOptions{
			OutputDimension: &dim512,
			InputType:       cohere.InputTypeSearchQuery,
		},
	}

	mediumModel, err := provider.EmbeddingModelWithOptions("cohere.embed-multilingual-v3", mediumOptions)
	if err != nil {
		log.Fatalf("Failed to create medium dimension model: %v", err)
	}

	result3, err := ai.Embed(ctx, ai.EmbedOptions{
		Model: mediumModel,
		Input: "Quelle est la capitale de la France?",
	})
	if err != nil {
		log.Fatalf("Failed to generate medium embedding: %v", err)
	}
	fmt.Printf("Medium embedding dimension: %d\n", len(result3.Embedding))
	fmt.Printf("Tokens used: %d\n\n", result3.Usage.InputTokens)

	// Example 4: Large dimension (1536) with truncation
	fmt.Println("=== Example 4: Large Dimension (1536) with Truncation ===")
	dim1536 := cohere.Dimension1536
	largeOptions := &bedrock.EmbeddingOptions{
		CohereOptions: &bedrock.CohereEmbeddingOptions{
			OutputDimension: &dim1536,
			InputType:       cohere.InputTypeClustering,
			Truncate:        cohere.TruncateEnd,
		},
	}

	largeModel, err := provider.EmbeddingModelWithOptions("cohere.embed-english-v3", largeOptions)
	if err != nil {
		log.Fatalf("Failed to create large dimension model: %v", err)
	}

	result4, err := ai.Embed(ctx, ai.EmbedOptions{
		Model: largeModel,
		Input: "Deep learning uses neural networks to model complex patterns in data",
	})
	if err != nil {
		log.Fatalf("Failed to generate large embedding: %v", err)
	}
	fmt.Printf("Large embedding dimension: %d\n", len(result4.Embedding))
	fmt.Printf("Tokens used: %d\n\n", result4.Usage.InputTokens)

	// Example 5: Batch processing with custom dimensions
	fmt.Println("=== Example 5: Batch Processing with Custom Dimensions ===")
	dim1024 := cohere.Dimension1024
	batchOptions := &bedrock.EmbeddingOptions{
		CohereOptions: &bedrock.CohereEmbeddingOptions{
			OutputDimension: &dim1024,
			InputType:       cohere.InputTypeClassification,
		},
	}

	batchModel, err := provider.EmbeddingModelWithOptions("cohere.embed-english-v3", batchOptions)
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

	fmt.Println("\n=== Summary ===")
	fmt.Println("Successfully demonstrated Cohere embedding dimensions on AWS Bedrock:")
	fmt.Println("- 256 dimensions: Optimal for storage and speed")
	fmt.Println("- 512 dimensions: Balanced performance")
	fmt.Println("- 1024 dimensions: Rich semantic representation")
	fmt.Println("- 1536 dimensions: Maximum information preservation")
}
