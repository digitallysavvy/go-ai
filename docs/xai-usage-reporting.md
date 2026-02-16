# XAI Advanced Usage Reporting

The Go-AI SDK provides sophisticated token usage tracking for XAI (formerly Twitter/X AI) models, including support for cached tokens, reasoning tokens, and multi-modal inputs.

## Overview

XAI models report detailed token usage information that goes beyond basic prompt/completion counts:

- **Cached tokens**: Tokens read from cache (prompt caching)
- **Reasoning tokens**: Tokens used for extended reasoning (Grok models)
- **Text input tokens**: Text-only portion of input
- **Image input tokens**: Image portion of input (multi-modal)

## Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

func main() {
    provider := xai.New(xai.Config{
        APIKey: "your-xai-api-key",
    })

    model, _ := provider.LanguageModel("grok-beta")

    result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
        Model:  model,
        Prompt: "Explain quantum computing",
    })

    if err != nil {
        log.Fatal(err)
    }

    // Access detailed usage information
    usage := result.Usage

    fmt.Printf("Total tokens: %d\n", *usage.TotalTokens)
    fmt.Printf("Input tokens: %d\n", *usage.InputTokens)
    fmt.Printf("Output tokens: %d\n", *usage.OutputTokens)

    // Access detailed breakdowns if available
    if usage.InputDetails != nil {
        if usage.InputDetails.CacheReadTokens != nil {
            fmt.Printf("Cache hits: %d tokens\n", *usage.InputDetails.CacheReadTokens)
        }
        if usage.InputDetails.NoCacheTokens != nil {
            fmt.Printf("Non-cached: %d tokens\n", *usage.InputDetails.NoCacheTokens)
        }
    }

    if usage.OutputDetails != nil {
        if usage.OutputDetails.ReasoningTokens != nil {
            fmt.Printf("Reasoning: %d tokens\n", *usage.OutputDetails.ReasoningTokens)
        }
    }
}
```

## Usage Structure

```go
type Usage struct {
    // Basic counts
    InputTokens  *int64  // Total input tokens
    OutputTokens *int64  // Total output tokens (including reasoning)
    TotalTokens  *int64  // Sum of input and output

    // Detailed breakdowns
    InputDetails  *InputTokenDetails
    OutputDetails *OutputTokenDetails

    // Raw API response data
    Raw map[string]interface{}
}

type InputTokenDetails struct {
    NoCacheTokens    *int64  // Tokens not from cache
    CacheReadTokens  *int64  // Tokens read from cache
    CacheWriteTokens *int64  // Tokens written to cache
    TextTokens       *int64  // Text-only input tokens
    ImageTokens      *int64  // Image input tokens
}

type OutputTokenDetails struct {
    TextTokens      *int64  // Regular output tokens
    ReasoningTokens *int64  // Extended reasoning tokens
}
```

## Cached Token Tracking

The SDK automatically handles two different cached token reporting patterns from the XAI API:

### Inclusive Caching (Most Common)

When `cached_tokens ≤ prompt_tokens`, cached tokens are **part of** the prompt tokens:

```go
// API Response:
// prompt_tokens: 200
// cached_tokens: 150

// SDK converts to:
InputTokens: 200  // Total (cached are included)
InputDetails: {
    NoCacheTokens:   50,   // 200 - 150
    CacheReadTokens: 150,  // From cache
}
```

### Exclusive Caching (Edge Cases)

When `cached_tokens > prompt_tokens`, cached tokens are **additional to** prompt tokens:

```go
// API Response:
// prompt_tokens: 4142
// cached_tokens: 4328 (larger than prompt!)

// SDK converts to:
InputTokens: 8470  // 4142 + 4328 (sum)
InputDetails: {
    NoCacheTokens:   4142,  // All prompt tokens
    CacheReadTokens: 4328,  // Additional cached tokens
}
```

The SDK automatically detects which pattern is being used and calculates correctly.

## Reasoning Token Tracking

For models with extended reasoning (like Grok with reasoning mode), reasoning tokens are **additive** to completion tokens:

```go
// API Response:
// completion_tokens: 50
// reasoning_tokens: 228

// SDK converts to:
OutputTokens: 278  // 50 + 228 (sum)
OutputDetails: {
    TextTokens:      50,   // Regular output
    ReasoningTokens: 228,  // Extended reasoning
}
```

This follows the XAI Chat API pattern where reasoning is additional compute.

## Multi-Modal Token Tracking

For inputs with images and text:

```go
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Messages: []types.Message{
        {
            Role: types.RoleUser,
            Content: []types.ContentPart{
                types.TextContent{Text: "What's in this image?"},
                types.ImageContent{URL: "https://example.com/image.jpg"},
            },
        },
    },
})

// Access multi-modal token breakdown
if result.Usage.InputDetails != nil {
    fmt.Printf("Text tokens: %d\n", *result.Usage.InputDetails.TextTokens)
    fmt.Printf("Image tokens: %d\n", *result.Usage.InputDetails.ImageTokens)
}
```

## Complete Example: Cost Tracking

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

// XAI pricing (example - check current pricing)
const (
    COST_PER_1K_INPUT  = 0.01
    COST_PER_1K_OUTPUT = 0.03
    COST_PER_1K_CACHED = 0.001  // Cached tokens are cheaper
)

func calculateCost(usage types.Usage) float64 {
    var cost float64

    // Input cost (accounting for cache)
    if usage.InputDetails != nil {
        // Non-cached input tokens
        if usage.InputDetails.NoCacheTokens != nil {
            tokens := float64(*usage.InputDetails.NoCacheTokens) / 1000.0
            cost += tokens * COST_PER_1K_INPUT
        }

        // Cached tokens (cheaper)
        if usage.InputDetails.CacheReadTokens != nil {
            tokens := float64(*usage.InputDetails.CacheReadTokens) / 1000.0
            cost += tokens * COST_PER_1K_CACHED
        }
    } else if usage.InputTokens != nil {
        // Fallback if no detailed breakdown
        tokens := float64(*usage.InputTokens) / 1000.0
        cost += tokens * COST_PER_1K_INPUT
    }

    // Output cost
    if usage.OutputTokens != nil {
        tokens := float64(*usage.OutputTokens) / 1000.0
        cost += tokens * COST_PER_1K_OUTPUT
    }

    return cost
}

func main() {
    provider := xai.New(xai.Config{
        APIKey: "your-xai-api-key",
    })

    model, _ := provider.LanguageModel("grok-beta")

    result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
        Model:  model,
        Prompt: "Write a detailed analysis of...",
    })

    if err != nil {
        log.Fatal(err)
    }

    // Calculate cost
    cost := calculateCost(result.Usage)

    // Print detailed breakdown
    fmt.Println("=== Token Usage ===")
    fmt.Printf("Total tokens: %d\n", *result.Usage.TotalTokens)

    if result.Usage.InputDetails != nil {
        fmt.Println("\nInput breakdown:")
        if result.Usage.InputDetails.NoCacheTokens != nil {
            fmt.Printf("  Non-cached: %d tokens\n", *result.Usage.InputDetails.NoCacheTokens)
        }
        if result.Usage.InputDetails.CacheReadTokens != nil {
            fmt.Printf("  Cached: %d tokens\n", *result.Usage.InputDetails.CacheReadTokens)
        }
        if result.Usage.InputDetails.TextTokens != nil {
            fmt.Printf("  Text: %d tokens\n", *result.Usage.InputDetails.TextTokens)
        }
        if result.Usage.InputDetails.ImageTokens != nil {
            fmt.Printf("  Images: %d tokens\n", *result.Usage.InputDetails.ImageTokens)
        }
    }

    if result.Usage.OutputDetails != nil {
        fmt.Println("\nOutput breakdown:")
        if result.Usage.OutputDetails.TextTokens != nil {
            fmt.Printf("  Text: %d tokens\n", *result.Usage.OutputDetails.TextTokens)
        }
        if result.Usage.OutputDetails.ReasoningTokens != nil {
            fmt.Printf("  Reasoning: %d tokens\n", *result.Usage.OutputDetails.ReasoningTokens)
        }
    }

    fmt.Printf("\nEstimated cost: $%.4f\n", cost)
}
```

## Advanced: Budget Enforcement

```go
type UsageBudget struct {
    MaxInputTokens  int64
    MaxOutputTokens int64
    MaxTotalCost    float64
}

func (b *UsageBudget) Check(usage types.Usage) error {
    if usage.InputTokens != nil && *usage.InputTokens > b.MaxInputTokens {
        return fmt.Errorf("input tokens (%d) exceeds budget (%d)",
            *usage.InputTokens, b.MaxInputTokens)
    }

    if usage.OutputTokens != nil && *usage.OutputTokens > b.MaxOutputTokens {
        return fmt.Errorf("output tokens (%d) exceeds budget (%d)",
            *usage.OutputTokens, b.MaxOutputTokens)
    }

    cost := calculateCost(usage)
    if cost > b.MaxTotalCost {
        return fmt.Errorf("cost ($%.4f) exceeds budget ($%.2f)",
            cost, b.MaxTotalCost)
    }

    return nil
}

// Usage
budget := UsageBudget{
    MaxInputTokens:  5000,
    MaxOutputTokens: 2000,
    MaxTotalCost:    0.50,
}

result, err := ai.GenerateText(ctx, opts)
if err != nil {
    log.Fatal(err)
}

if err := budget.Check(result.Usage); err != nil {
    log.Printf("Budget violation: %v", err)
}
```

## Raw API Data

All raw token data from the API is preserved in the `Raw` field:

```go
result, err := ai.GenerateText(ctx, opts)
if err != nil {
    log.Fatal(err)
}

// Access raw API response
rawData := result.Usage.Raw

// Get direct API fields
if cachedTokens, ok := rawData["cached_tokens"].(int64); ok {
    fmt.Printf("Raw cached_tokens: %d\n", cachedTokens)
}

if reasoningTokens, ok := rawData["reasoning_tokens"].(int64); ok {
    fmt.Printf("Raw reasoning_tokens: %d\n", reasoningTokens)
}

// Nested structures
if details, ok := rawData["prompt_tokens_details"].(map[string]interface{}); ok {
    fmt.Printf("Prompt token details: %+v\n", details)
}
```

## Token Calculation Examples

### Example 1: Basic Request (No Cache, No Reasoning)

```
API Response:
  prompt_tokens: 100
  completion_tokens: 50
  total_tokens: 150

SDK Output:
  InputTokens: 100
  OutputTokens: 50
  TotalTokens: 150
  InputDetails: nil
  OutputDetails: nil
```

### Example 2: With Cache (Inclusive)

```
API Response:
  prompt_tokens: 200
  cached_tokens: 150

SDK Output:
  InputTokens: 200
  InputDetails: {
    NoCacheTokens: 50    (200 - 150)
    CacheReadTokens: 150
  }
  TotalTokens: 250  (recalculated)
```

### Example 3: With Cache (Exclusive) + Reasoning

```
API Response:
  prompt_tokens: 4142
  cached_tokens: 4328 (> prompt)
  completion_tokens: 100
  reasoning_tokens: 200

SDK Output:
  InputTokens: 8470     (4142 + 4328)
  OutputTokens: 300     (100 + 200)
  TotalTokens: 8770     (8470 + 300)
  InputDetails: {
    NoCacheTokens: 4142
    CacheReadTokens: 4328
  }
  OutputDetails: {
    TextTokens: 100
    ReasoningTokens: 200
  }
```

### Example 4: Multi-Modal

```
API Response:
  prompt_tokens: 500
  text_input_tokens: 100
  image_input_tokens: 400

SDK Output:
  InputTokens: 500
  InputDetails: {
    TextTokens: 100
    ImageTokens: 400
  }
```

## Best Practices

1. **Always check for nil**: Token details may not always be present
   ```go
   if usage.InputDetails != nil && usage.InputDetails.CacheReadTokens != nil {
       // Use cached token count
   }
   ```

2. **Use detailed breakdowns for cost calculation**: Don't rely on basic counts alone

3. **Monitor cache effectiveness**:
   ```go
   if usage.InputDetails != nil {
       cacheRate := float64(*usage.InputDetails.CacheReadTokens) /
                    float64(*usage.InputTokens) * 100
       fmt.Printf("Cache hit rate: %.1f%%\n", cacheRate)
   }
   ```

4. **Track reasoning token usage**: Reasoning tokens can significantly increase costs

5. **Log raw data for debugging**: The `Raw` field contains all original API data

## Comparison with TypeScript SDK

The Go implementation matches the TypeScript AI SDK's usage tracking:

| TypeScript | Go | Notes |
|------------|-----|-------|
| `usage.inputTokens.total` | `InputTokens` | Total input |
| `usage.inputTokens.noCache` | `InputDetails.NoCacheTokens` | Non-cached |
| `usage.inputTokens.cacheRead` | `InputDetails.CacheReadTokens` | Cached |
| `usage.outputTokens.total` | `OutputTokens` | Total output |
| `usage.outputTokens.text` | `OutputDetails.TextTokens` | Text output |
| `usage.outputTokens.reasoning` | `OutputDetails.ReasoningTokens` | Reasoning |
| `usage.raw` | `Raw` | Raw API data |

The cached token inclusivity logic and reasoning token additive behavior match the TypeScript implementation exactly.

## See Also

- [XAI Provider](./providers/xai.md)
- [Cost Optimization](./cost-optimization.md)
- [Usage Tracking](./usage-tracking.md)
- [Prompt Caching](./prompt-caching.md)
