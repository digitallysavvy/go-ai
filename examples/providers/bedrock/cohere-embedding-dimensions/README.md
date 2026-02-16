# AWS Bedrock Cohere Embedding Dimensions Example

This example demonstrates how to use the `outputDimension` parameter with Cohere embedding models on AWS Bedrock to control the size of the output embedding vector.

## Features

- Default dimensions (model default)
- Small dimension (256) for cost/storage optimization
- Medium dimension (512) for balanced performance
- Large dimension (1536) for maximum semantic information
- Batch processing with custom dimensions

## Supported Dimensions

Cohere v3 embedding models on Bedrock support the following dimensions:
- **256**: Smallest, fastest, lowest storage cost
- **512**: Balanced performance and accuracy
- **1024**: Default, good semantic representation
- **1536**: Largest, maximum information preservation

## Supported Models

AWS Bedrock Cohere models:
- `cohere.embed-english-v3`
- `cohere.embed-multilingual-v3`

## Prerequisites

Set your AWS credentials:

```bash
export AWS_ACCESS_KEY_ID=your_access_key
export AWS_SECRET_ACCESS_KEY=your_secret_key
export AWS_REGION=us-east-1
```

Ensure you have access to Cohere models in AWS Bedrock. You may need to request access in the AWS Console.

## Running the Example

```bash
cd examples/providers/bedrock/cohere-embedding-dimensions
go run main.go
```

## Example Output

```
=== Example 1: Default Dimensions ===
Default embedding dimension: 1024
Tokens used: 12

=== Example 2: Small Dimension (256) ===
Small embedding dimension: 256
Tokens used: 10

=== Example 3: Medium Dimension (512) ===
Medium embedding dimension: 512
Tokens used: 9

=== Example 4: Large Dimension (1536) with Truncation ===
Large embedding dimension: 1536
Tokens used: 15

=== Example 5: Batch Processing with Custom Dimensions ===
Text 1: 1024 dimensions
Text 2: 1024 dimensions
Text 3: 1024 dimensions
Total tokens used: 35

=== Summary ===
Successfully demonstrated Cohere embedding dimensions on AWS Bedrock:
- 256 dimensions: Optimal for storage and speed
- 512 dimensions: Balanced performance
- 1024 dimensions: Rich semantic representation
- 1536 dimensions: Maximum information preservation
```

## When to Use Different Dimensions

### Use Smaller Dimensions (256, 512) when:
- Building large-scale vector databases (storage costs)
- Real-time similarity search is required (speed)
- Semantic precision is less critical
- Cost optimization is a priority

### Use Larger Dimensions (1024, 1536) when:
- High-precision semantic search is required
- Working with complex, domain-specific content
- Maximum information preservation is needed
- Accuracy is more important than storage/speed

## Options

The `CohereEmbeddingOptions` struct supports:

- **OutputDimension**: Controls the embedding vector size (256, 512, 1024, 1536)
- **InputType**: Optimizes embeddings for specific use cases (required for Cohere on Bedrock)
  - `search_document`: For storing in vector database
  - `search_query`: For queries against vector database
  - `classification`: For text classification
  - `clustering`: For clustering algorithms
- **Truncate**: Controls how oversized inputs are handled
  - `NONE`: Error if input exceeds max length
  - `START`: Truncate from the start
  - `END`: Truncate from the end

## Code Example

```go
provider := bedrock.New(bedrock.Config{
    AWSAccessKeyID:     accessKeyID,
    AWSSecretAccessKey: secretAccessKey,
    Region:             region,
})

// Create model with 512 dimensions
dim512 := cohere.Dimension512
options := &bedrock.EmbeddingOptions{
    CohereOptions: &bedrock.CohereEmbeddingOptions{
        OutputDimension: &dim512,
        InputType:       cohere.InputTypeSearchQuery,
    },
}

model, err := provider.EmbeddingModelWithOptions("cohere.embed-english-v3", options)

result, err := ai.Embed(ctx, ai.EmbedOptions{
    Model: model,
    Value: "Your text here",
})

fmt.Printf("Embedding dimension: %d\n", len(result.Embedding))
```

## Performance Impact

- **API latency**: Roughly the same across all dimensions
- **Storage**: Smaller dimensions = lower storage costs (256 is 6x smaller than 1536)
- **Search speed**: Smaller dimensions = faster similarity search
- **Semantic quality**: Larger dimensions = better semantic representation

## AWS Bedrock Specifics

- Bedrock processes embeddings one at a time (batch size = 1)
- `InputType` is required for Cohere models on Bedrock
- Token usage is approximate (Cohere on Bedrock doesn't always return token counts)

## Related Examples

- [Cohere Direct API Embeddings](../../cohere/embedding-dimensions)
- [Bedrock Titan Embeddings](../titan-embeddings)
