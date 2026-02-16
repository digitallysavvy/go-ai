# Cohere Embedding Dimensions Example

This example demonstrates how to use the `outputDimension` parameter with Cohere embedding models to control the size of the output embedding vector.

## Features

- Default dimensions (model default, typically 1024)
- Small dimension (256) for cost/storage optimization
- Medium dimension (512) for balanced performance
- Large dimension (1536) for maximum semantic information
- Batch embeddings with custom dimensions

## Supported Dimensions

Cohere v3 embedding models support the following dimensions:
- **256**: Smallest, fastest, lowest storage cost
- **512**: Balanced performance and accuracy
- **1024**: Default, good semantic representation
- **1536**: Largest, maximum information preservation

## Supported Models

- `embed-english-v3.0`
- `embed-multilingual-v3.0`
- `embed-english-light-v3.0`
- `embed-multilingual-light-v3.0`

**Note:** Output dimension control is only available for v3.0 and newer models.

## Prerequisites

Set your Cohere API key:

```bash
export COHERE_API_KEY=your_api_key_here
```

## Running the Example

```bash
cd examples/providers/cohere/embedding-dimensions
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

=== Example 4: Large Dimension (1536) ===
Large embedding dimension: 1536
Tokens used: 14

=== Example 5: Batch Embeddings with Custom Dimensions ===
Text 1: 1024 dimensions
Text 2: 1024 dimensions
Text 3: 1024 dimensions
Total tokens used: 35
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

The `EmbeddingOptions` struct supports:

- **OutputDimension**: Controls the embedding vector size (256, 512, 1024, 1536)
- **InputType**: Optimizes embeddings for specific use cases
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
provider := cohere.New(cohere.Config{
    APIKey: apiKey,
})

// Create model with 512 dimensions
dim512 := cohere.Dimension512
model, err := provider.EmbeddingModelWithOptions("embed-english-v3.0", cohere.EmbeddingOptions{
    OutputDimension: &dim512,
    InputType:       cohere.InputTypeSearchQuery,
})

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

## Related Examples

- [Cohere Basic Embeddings](../basic-embeddings)
- [Bedrock Cohere Embeddings](../../bedrock/cohere-embedding-dimensions)
