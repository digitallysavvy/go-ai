# OpenAI Prompt Cache Retention Example

This example demonstrates how to use OpenAI's prompt cache retention feature with the Go-AI SDK.

## What is Prompt Cache Retention?

Prompt cache retention allows you to cache parts of your prompts (especially large context/system messages) to reduce costs and improve latency on repeated calls. OpenAI offers two retention policies:

- **`in_memory`** (default): Standard prompt caching behavior
- **`24h`**: Extended caching that keeps cached prefixes active for up to 24 hours (available for gpt-5.1 and gpt-5.2 series models)

## Benefits

1. **Cost Savings**: Cached tokens are significantly cheaper than regular input tokens
2. **Reduced Latency**: Cached prompts don't need to be reprocessed
3. **Improved Performance**: Ideal for applications with large, repetitive context

## Use Cases

- Chatbots with large system prompts
- Document Q&A with fixed document context
- Code assistants with project context
- Multi-turn conversations with stable context
- Batch processing with shared instructions

## Supported Models

The `24h` retention policy is currently supported on:
- gpt-5.1
- gpt-5.1-chat-latest
- gpt-5.1-codex variants
- gpt-5.2
- gpt-5.2-pro

## Usage

### Basic Usage with High-Level API

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

p := openai.New(openai.Config{
    APIKey: "your-api-key",
})

model, _ := p.LanguageModel("gpt-5.1")

result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Prompt: "Your prompt here...",
    ProviderOptions: map[string]interface{}{
        "openai": map[string]interface{}{
            "promptCacheRetention": "24h",
        },
    },
})
```

### Low-Level API Usage

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/provider"
    "github.com/digitallysavvy/go-ai/pkg/provider/types"
)

result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
    Prompt: types.Prompt{
        Messages: []types.Message{
            {
                Role: types.RoleUser,
                Content: []types.ContentPart{
                    types.TextContent{Text: "Your prompt here..."},
                },
            },
        },
    },
    ProviderOptions: map[string]interface{}{
        "openai": map[string]interface{}{
            "promptCacheRetention": "24h",
        },
    },
})
```

## Running the Example

```bash
export OPENAI_API_KEY=your-api-key-here
go run main.go
```

## Examples in This Demo

1. **Default Cache Retention**: Shows standard caching behavior
2. **Extended Cache Retention**: Demonstrates 24h caching with large prompts
3. **Repeated Calls**: Shows how cache is reused across multiple API calls

## Token Usage

When using prompt caching, you'll see different token types in the usage:

```go
// First call - writes to cache
Input tokens: 500
Cache read tokens: 0
Output tokens: 100

// Second call with same prefix - reads from cache
Input tokens: 500
Cache read tokens: 480 (saved!)
No-cache tokens: 20
Output tokens: 150
```

## Best Practices

1. **Use consistent prefixes**: Keep your large context/instructions at the start of prompts
2. **Batch similar requests**: Group requests that share context to maximize cache hits
3. **Monitor token usage**: Track `CacheReadTokens` to verify caching is working
4. **Use with long-running sessions**: 24h retention is ideal for persistent applications

## Cost Optimization

Cached tokens typically cost about **50% less** than regular input tokens, making this feature especially valuable for:
- Applications with large system prompts (1000+ tokens)
- High-volume API usage
- Real-time applications requiring low latency

## Notes

- The first call with a new prompt will write to the cache (normal input token cost)
- Subsequent calls with the same prefix will read from cache (reduced cost)
- Cache expires after 24 hours with `"24h"` retention
- Cache is automatically managed by OpenAI
