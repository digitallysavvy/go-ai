# Fireworks Kimi K2.5: Extended Reasoning

Fireworks AI's Kimi K2.5 model supports extended reasoning and thinking capabilities, allowing the model to spend additional compute on complex problems before generating a response.

## Overview

The Kimi K2.5 model provides two key features:

1. **Thinking Mode**: Enable extended reasoning with configurable token budgets
2. **Reasoning History**: Control how intermediate reasoning steps are included in responses

## Model IDs

```go
// Kimi K2 variants
"accounts/fireworks/models/kimi-k2-instruct"   // Standard instruction following
"accounts/fireworks/models/kimi-k2-thinking"   // Thinking-optimized variant
"accounts/fireworks/models/kimi-k2p5"          // Kimi K2.5 (latest)
```

## Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
)

func main() {
    provider := fireworks.New(fireworks.Config{
        APIKey: "your-api-key",
    })

    model, _ := provider.LanguageModel("accounts/fireworks/models/kimi-k2p5")

    result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
        Model:  model,
        Prompt: "Solve this complex math problem step by step: ...",

        // Enable thinking with default settings
        ProviderOptions: map[string]interface{}{
            "thinking": map[string]interface{}{
                "type": "enabled",
            },
        },
    })

    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Text)
}
```

## Thinking Options

### Enable/Disable Thinking

```go
// Enable thinking
ProviderOptions: map[string]interface{}{
    "thinking": map[string]interface{}{
        "type": "enabled",
    },
}

// Disable thinking (default)
ProviderOptions: map[string]interface{}{
    "thinking": map[string]interface{}{
        "type": "disabled",
    },
}
```

### Set Token Budget

Control how many tokens the model can use for reasoning:

```go
ProviderOptions: map[string]interface{}{
    "thinking": map[string]interface{}{
        "type":         "enabled",
        "budgetTokens": 2048,  // Minimum: 1024
    },
}
```

**Important**: `budgetTokens` must be at least 1024. The SDK automatically enforces this minimum.

## Reasoning History Modes

Control how intermediate reasoning steps appear in the response:

### Disabled (Default)

Don't include reasoning history in the response:

```go
ProviderOptions: map[string]interface{}{
    "reasoningHistory": "disabled",
}
```

### Interleaved

Mix reasoning steps with the final response:

```go
ProviderOptions: map[string]interface{}{
    "reasoningHistory": "interleaved",
}
```

The response will contain both the reasoning process and the final answer interwoven.

### Preserved

Keep reasoning separate from the final response:

```go
ProviderOptions: map[string]interface{}{
    "reasoningHistory": "preserved",
}
```

The reasoning steps are preserved but clearly separated from the final answer.

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
)

func main() {
    provider := fireworks.New(fireworks.Config{
        APIKey: "your-fireworks-api-key",
    })

    model, err := provider.LanguageModel("accounts/fireworks/models/kimi-k2p5")
    if err != nil {
        log.Fatal(err)
    }

    // Complex problem requiring deep reasoning
    prompt := `
    A train leaves Station A at 60 mph heading east.
    Another train leaves Station B (240 miles east of A) at 40 mph heading west.
    A bird starts at Station A and flies at 100 mph between the trains until they meet.
    How far does the bird travel?
    `

    result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
        Model:  model,
        Prompt: prompt,

        // Enable extended reasoning with large token budget
        ProviderOptions: map[string]interface{}{
            "thinking": map[string]interface{}{
                "type":         "enabled",
                "budgetTokens": 3072,  // Allow extensive reasoning
            },
            "reasoningHistory": "preserved",  // Show the thinking process
        },

        // Optional: Adjust temperature for reasoning
        Temperature: floatPtr(0.7),
    })

    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Problem: %s\n\n", prompt)
    fmt.Printf("Solution:\n%s\n", result.Text)

    // Check token usage
    if result.Usage.InputTokens != nil {
        fmt.Printf("\nInput tokens: %d\n", *result.Usage.InputTokens)
    }
    if result.Usage.OutputTokens != nil {
        fmt.Printf("Output tokens: %d\n", *result.Usage.OutputTokens)
    }
}

func floatPtr(f float64) *float64 {
    return &f
}
```

## Combined Configuration

Use both thinking and reasoning history together:

```go
ProviderOptions: map[string]interface{}{
    // Extended reasoning
    "thinking": map[string]interface{}{
        "type":         "enabled",
        "budgetTokens": 2048,
    },

    // Show the reasoning process
    "reasoningHistory": "preserved",
}
```

## Token Budget Guidelines

Choose token budgets based on problem complexity:

| Problem Type | Recommended Budget |
|--------------|-------------------|
| Simple calculations | 1024 (minimum) |
| Medium complexity | 2048 |
| Complex reasoning | 3072 - 4096 |
| Very complex/multi-step | 4096+ |

**Note**: Higher budgets allow more thorough reasoning but increase costs and latency.

## API Parameter Conversion

The SDK automatically converts Go-style camelCase parameters to the API's snake_case format:

| Go Parameter | API Parameter |
|--------------|---------------|
| `budgetTokens` | `budget_tokens` |
| `reasoningHistory` | `reasoning_history` |

You don't need to worry about this conversion - it's handled automatically.

## Cost Considerations

Extended reasoning uses additional tokens:

```go
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Prompt: "Complex problem...",
    ProviderOptions: map[string]interface{}{
        "thinking": map[string]interface{}{
            "type": "enabled",
            "budgetTokens": 2048,
        },
    },
})

// Check actual token usage
if result.Usage.OutputTokens != nil {
    fmt.Printf("Output tokens used: %d\n", *result.Usage.OutputTokens)
}

// The model may use up to budgetTokens additional tokens for reasoning
// beyond the normal response length
```

## Use Cases

### 1. Mathematical Problem Solving

```go
ProviderOptions: map[string]interface{}{
    "thinking": map[string]interface{}{
        "type": "enabled",
        "budgetTokens": 3072,
    },
    "reasoningHistory": "preserved",  // Show work
}
```

### 2. Code Review and Analysis

```go
ProviderOptions: map[string]interface{}{
    "thinking": map[string]interface{}{
        "type": "enabled",
        "budgetTokens": 4096,  // Thorough analysis
    },
    "reasoningHistory": "interleaved",  // Mix analysis with findings
}
```

### 3. Strategic Planning

```go
ProviderOptions: map[string]interface{}{
    "thinking": map[string]interface{}{
        "type": "enabled",
        "budgetTokens": 2048,
    },
    "reasoningHistory": "disabled",  // Only final plan
}
```

### 4. Simple Queries (No Thinking)

```go
// For simple queries, disable thinking to save tokens
ProviderOptions: map[string]interface{}{
    "thinking": map[string]interface{}{
        "type": "disabled",
    },
}
```

## Best Practices

1. **Match budget to complexity**: Don't use large budgets for simple problems
2. **Monitor token usage**: Track costs, especially with large budgets
3. **Choose reasoning history mode carefully**:
   - Use `"preserved"` for educational/debugging purposes
   - Use `"disabled"` for production to reduce output tokens
   - Use `"interleaved"` for detailed explanations
4. **Test with different budgets**: Find the optimal budget for your use case
5. **Combine with temperature**: Lower temperature (0.5-0.7) often works well with thinking mode

## Error Handling

```go
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Prompt: prompt,
    ProviderOptions: map[string]interface{}{
        "thinking": map[string]interface{}{
            "type": "enabled",
            "budgetTokens": 1024,
        },
    },
})

if err != nil {
    // Handle errors
    log.Printf("Generation failed: %v", err)
    return
}

// Check for warnings
if len(result.Warnings) > 0 {
    for _, warning := range result.Warnings {
        log.Printf("Warning: %s", warning.Message)
    }
}
```

## Comparison with Other Providers

| Feature | Fireworks Kimi K2.5 | OpenAI o1 | Anthropic Claude |
|---------|---------------------|-----------|------------------|
| Extended reasoning | ✅ Configurable | ✅ Automatic | ❌ Not available |
| Token budget control | ✅ Yes | ❌ No | N/A |
| Reasoning history | ✅ 3 modes | ✅ Limited | N/A |

## See Also

- [Fireworks AI Provider](./providers/fireworks.md)
- [Provider Options](./provider-options.md)
- [Cost Optimization](./cost-optimization.md)
