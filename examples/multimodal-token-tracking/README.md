# Multimodal Token Tracking Example

This example demonstrates how to track and differentiate between text and image input tokens for accurate cost tracking in multimodal AI applications.

## Features

- Text-only vs multimodal token usage comparison
- Accurate cost calculation based on token types
- Batch processing with token tracking
- Handling providers with different support levels

## Usage

```bash
# Set your OpenAI API key
export OPENAI_API_KEY="your-api-key-here"

# Run the example
go run main.go
```

## Expected Output

```
=== Multimodal Token Tracking Example ===

1. Text-only request:
=== Text-only Usage ===
Total input tokens: 12
Total output tokens: 5
  - Text input tokens: 12
  - Image input tokens: 0
Estimated cost: $0.000080

2. Multimodal request (text + image):
=== Multimodal Usage ===
Total input tokens: 1425
Total output tokens: 15
  - Text input tokens: 15
  - Image input tokens: 1410
Estimated cost: $0.010728

3. Cost comparison:
Text-only cost: $0.000080
Multimodal cost: $0.010728
Difference: $0.010648

4. Batch processing with token tracking:
Task 1: 45 tokens, $0.000563
Task 2: 78 tokens, $0.000975
Task 3: 52 tokens, $0.000650
Batch Summary:
Total tokens: 175
Total cost: $0.002188
Text input tokens: 175
Image input tokens: 0
```

## Key Concepts

### Token Differentiation

The SDK now tracks text and image tokens separately:

```go
if usage.InputDetails != nil &&
   usage.InputDetails.TextTokens != nil &&
   usage.InputDetails.ImageTokens != nil {
    textTokens := *usage.InputDetails.TextTokens
    imageTokens := *usage.InputDetails.ImageTokens
    // Use separate pricing for each
}
```

### Accurate Cost Calculation

Different token types have different pricing:

```go
const (
    textInputCost  = 0.0000025  // $2.50 per 1M tokens
    imageInputCost = 0.0000075  // ~$7.50 per 1M tokens
    outputCost     = 0.0000100  // $10.00 per 1M tokens
)

func calculateCost(usage types.Usage) float64 {
    var inputCost float64
    if usage.InputDetails != nil &&
       usage.InputDetails.TextTokens != nil &&
       usage.InputDetails.ImageTokens != nil {
        inputCost = float64(*usage.InputDetails.TextTokens)*textInputCost +
                    float64(*usage.InputDetails.ImageTokens)*imageInputCost
    } else {
        inputCost = float64(usage.GetInputTokens())*textInputCost
    }
    return inputCost + float64(usage.GetOutputTokens())*outputCost
}
```

### Provider Compatibility

The example handles providers that may not support detailed token breakdown:

```go
if usage.InputDetails != nil {
    // Detailed breakdown available
} else {
    // Fallback to total tokens
}
```

## Provider Support

| Provider | Text/Image Tokens | Notes |
|----------|-------------------|-------|
| OpenAI   | ✅ Yes            | Full support with GPT-4o and similar |
| XAI      | ✅ Yes            | Full support |
| Google   | ✅ Yes            | Parsed from modalityDetails |
| Anthropic| ❌ No             | Fields will be nil |
| Azure    | ✅ Yes            | OpenAI-compatible |
| Others   | Varies            | Check documentation |

## Related Examples

- [Simple Generate Text](../generate-text/) - Basic text generation
- [Vision Example](../vision/) - Working with images
- [Cost Tracking](../cost-tracking/) - Advanced cost monitoring

## Learn More

- [Usage Types Documentation](../../docs/07-reference/types/usage.mdx)
- [Migration Guide](../../docs/08-migration-guides/token-usage-differentiation.mdx)
- [Multimodal Guide](../../docs/05-providers/01-overview.mdx#multimodal-support)
