# Anthropic Provider

The Anthropic provider enables access to Claude models including Opus 4.6, Sonnet 4.6, and other Claude variants.

## Features

- Full Claude API support
- Computer use tools
- Code execution capabilities
- Tool search for large tool catalogs
- Streaming and non-streaming responses
- Tool calling and function execution
- Automatic prompt caching
- Structured output via `output_config.format`

## Installation

```bash
go get github.com/digitallysavvy/go-ai
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
    // Create provider
    provider := anthropic.New(anthropic.Config{
        APIKey: "your-api-key",
    })

    // Get model
    model, err := provider.LanguageModel("claude-opus-4-20250514")
    if err != nil {
        log.Fatal(err)
    }

    // Generate text
    result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
        Model: model,
        Messages: []ai.Message{
            {
                Role: ai.RoleUser,
                Content: ai.MessageContent{Text: stringPtr("Hello!")},
            },
        },
    })

    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Text)
}

func stringPtr(s string) *string { return &s }
```

## Available Models

Use the model ID constants from this package to avoid typos:

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"

model, err := provider.LanguageModel(anthropic.ClaudeSonnet4_6)
```

### Claude Opus 4.6 (latest)
- Constant: `anthropic.ClaudeOpus4_6` → `"claude-opus-4-6"`
- Best for: Complex tasks, extended thinking, fast mode

### Claude Sonnet 4.6 (new)
- Constant: `anthropic.ClaudeSonnet4_6` → `"claude-sonnet-4-6"`
- Best for: Balanced performance and capability

### Claude Opus 4.5
- Constant: `anthropic.ClaudeOpus4_5` → `"claude-opus-4-5"`
- Best for: Complex tasks, advanced reasoning
- Supports: Computer use, tool search, code execution

### Claude Sonnet 4.5
- Constant: `anthropic.ClaudeSonnet4_5` → `"claude-sonnet-4-5"`
- Best for: Balanced performance and cost
- Supports: Tool search, code execution

### Claude Haiku 4.5
- Constant: `anthropic.ClaudeHaiku4_5` → `"claude-haiku-4-5"`
- Best for: Fast, cost-effective tasks

## Computer Use Tools

Anthropic provides specialized tools for computer control, code execution, and tool discovery.

### Computer Use

Control computers through mouse, keyboard, and screenshots:

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"

computerTool := tools.Computer20251124(tools.Computer20251124Args{
    DisplayWidthPx:  1920,
    DisplayHeightPx: 1080,
    EnableZoom:      true,
})

result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Messages: []ai.Message{{
        Role: ai.RoleUser,
        Content: ai.MessageContent{
            Text: stringPtr("Take a screenshot"),
        },
    }},
    Tools: map[string]ai.Tool{
        "computer": computerTool,
    },
})
```

### Code Execution

Run Python and bash code in sandboxed environments:

```go
codeExecTool := tools.CodeExecution20250825()

result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Messages: []ai.Message{{
        Role: ai.RoleUser,
        Content: ai.MessageContent{
            Text: stringPtr("Create a bar chart of sales data"),
        },
    }},
    Tools: map[string]ai.Tool{
        "codeExecution": codeExecTool,
    },
})
```

### Tool Search

Work with large tool catalogs (hundreds/thousands of tools):

```go
// BM25 natural language search
toolSearchBM25 := tools.ToolSearchBm2520251119()

// Regex pattern search
toolSearchRegex := tools.ToolSearchRegex20251119()

// Combine with deferred tools
allTools := map[string]ai.Tool{
    "toolSearch": toolSearchBM25,
    // ... hundreds of other tools marked for deferred loading
}

result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Messages: []ai.Message{{
        Role: ai.RoleUser,
        Content: ai.MessageContent{
            Text: stringPtr("Find weather tools and get forecast"),
        },
    }},
    Tools: allTools,
    MaxSteps: intPtr(10),
})
```

## Automatic Caching

Anthropic supports **automatic prompt caching** — the API identifies and caches reusable
prompt segments without requiring explicit `cache_control` markers on individual messages.

### Quick Start

```go
model, err := provider.LanguageModelWithOptions(anthropic.ClaudeSonnet4_6, &anthropic.ModelOptions{
    AutomaticCaching: true,
})

result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:    model,
    Messages: messages,
})
```

When `AutomaticCaching` is `true`, the SDK:
1. Sends the `anthropic-beta: prompt-caching-2024-07-31` header
2. Adds `cache_control: {type: "auto"}` to the request body

### Checking Cache Usage

```go
if result.Usage != nil && result.Usage.InputDetails != nil {
    cacheRead  := result.Usage.InputDetails.CacheReadTokens
    cacheWrite := result.Usage.InputDetails.CacheWriteTokens
    fmt.Printf("Cache read: %d tokens, cache write: %d tokens\n", *cacheRead, *cacheWrite)
}
```

---

## Structured Output (JSON Mode)

Use `ResponseFormat` to request JSON output from the model. The SDK uses the
`output_config.format` API field (not the deprecated `output_format`).

```go
schema := map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "name":  map[string]interface{}{"type": "string"},
        "score": map[string]interface{}{"type": "number"},
    },
}

result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Messages: messages,
    ResponseFormat: &provider.ResponseFormat{
        Type:   "json",
        Schema: schema,
    },
})
```

### Migration Note: `output_format` → `output_config.format`

**Anthropic removed the `output_format` API field.** If you were previously using a custom
integration that set `output_format: "json"`, update to:

```json
// Old (no longer accepted by Anthropic API):
{ "output_format": "json" }

// New (correct):
{ "output_config": { "format": { "type": "json_schema", "schema": { ... } } } }
```

The Go SDK handles this automatically via the `ResponseFormat` field on `GenerateOptions`.

---

## Tool Caching (Prompt Caching)

Reduce costs by up to 90% for repeated tool definitions using Anthropic's prompt caching feature.

### Quick Start

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/provider/types"
    "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// Enable caching on tool definitions
weatherTool := types.Tool{
    Name:        "get_weather",
    Description: "Get current weather for a location",
    Parameters:  weatherSchema,
    Execute:     getWeatherFunc,
    ProviderOptions: anthropic.WithToolCache("5m"), // Cache for 5 minutes
}

result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Messages: messages,
    Tools: map[string]types.Tool{
        "get_weather": weatherTool,
    },
})
```

### Cache TTL Options

**5 minutes** (default) - Best for short interactive sessions
```go
ProviderOptions: anthropic.WithToolCache("5m")
```

**1 hour** - Best for longer sessions (requires Claude 4.5 models)
```go
ProviderOptions: anthropic.WithToolCache("1h")
```

### Cost Savings

Tool caching dramatically reduces costs for repeated tool definitions:

| Scenario | Without Caching | With Caching | Savings |
|----------|----------------|--------------|---------|
| 10 tool definitions per request | 100% cost | 10% cost | 90% |
| 50 tool definitions per request | 100% cost | 2% cost | 98% |

**Example:** With 10 tools totaling 5,000 tokens:
- First request: 5,000 input tokens (full cost)
- Subsequent requests (within TTL): 500 input tokens (90% savings)

### Best Practices

1. **Cache stable tools** - Tools with unchanging definitions benefit most
2. **Use 5m for short sessions** - Interactive sessions, quick tasks
3. **Use 1h for long sessions** - Complex workflows, extended conversations
4. **Batch similar requests** - Maximize cache hits within TTL window
5. **Monitor cache metrics** - Check usage.cache_read_tokens in responses

### Advanced Configuration

```go
// Custom cache control
tool := types.Tool{
    Name:        "complex_tool",
    Description: "Complex multi-step tool",
    Parameters:  schema,
    ProviderOptions: &anthropic.ToolOptions{
        CacheControl: &anthropic.CacheControl{
            Type: "ephemeral",
            TTL:  "1h",
        },
    },
}
```

### Checking Cache Usage

```go
result, err := ai.GenerateText(ctx, options)
if err != nil {
    log.Fatal(err)
}

// Check if cache was used
if result.Usage != nil && result.Usage.InputDetails != nil {
    cacheRead := result.Usage.InputDetails.CacheReadTokens
    cacheWrite := result.Usage.InputDetails.CacheWriteTokens

    fmt.Printf("Cache read: %d tokens\n", *cacheRead)
    fmt.Printf("Cache write: %d tokens\n", *cacheWrite)
}
```

### When to Use Tool Caching

✅ **Use caching when:**
- Tool definitions are large (>1000 tokens)
- Using 5+ tools repeatedly
- Running agentic workflows with multiple requests
- Tool definitions don't change between requests

❌ **Don't use caching when:**
- Tool definitions change frequently
- Using only 1-2 small tools
- Making infrequent, isolated requests
- TTL is shorter than request intervals

### Model Compatibility

| Model | 5m TTL | 1h TTL |
|-------|--------|--------|
| Claude Opus 4.5 | ✅ | ✅ |
| Claude Sonnet 4.5 | ✅ | ✅ |
| Claude Sonnet 4 | ✅ | ❌ |
| Claude Haiku 4 | ✅ | ❌ |

See [Anthropic Prompt Caching Docs](https://docs.anthropic.com/claude/docs/prompt-caching) for more details.

## Context Management (Beta)

Automatically clean up conversation history to prevent context window overflow in long conversations.

### Quick Start

```go
// Enable context management when creating the model
model, err := provider.LanguageModelWithOptions("claude-sonnet-4", &anthropic.ModelOptions{
    ContextManagement: &anthropic.ContextManagement{
        Strategies: []string{anthropic.StrategyClearToolUses},
    },
})

result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:    model,
    Messages: longConversation, // Many tool calls or thinking blocks
})

// Check what was cleaned up
if result.ContextManagement != nil {
    cm := result.ContextManagement.(*anthropic.ContextManagementResponse)
    fmt.Printf("Tool uses cleared: %d\n", cm.ToolUsesCleared)
    fmt.Printf("Messages deleted: %d\n", cm.MessagesDeleted)
}
```

### Available Strategies

**`clear_tool_uses`** - Remove old tool call/result pairs
Best for: Agentic workflows, multi-turn tool use
Keeps: Final tool results, assistant reasoning

**`clear_thinking`** - Remove extended thinking blocks
Best for: Complex analysis, long-form reasoning
Keeps: Final conclusions, removes thinking process

**Both strategies** - Maximum context savings
Best for: Very long conversations with tools AND reasoning

### Why Use Context Management?

- **Avoid hitting context limits** - Long conversations can exceed 200K tokens
- **Reduce costs** - Fewer tokens = lower costs (thinking blocks use 10x tokens)
- **Maintain performance** - Smaller context = faster responses
- **No code changes** - Just configure strategies, Anthropic handles the rest

### Example Response

```go
type ContextManagementResponse struct {
    MessagesDeleted       int // Complete messages removed
    MessagesTruncated     int // Messages partially truncated
    ToolUsesCleared       int // Tool uses removed (clear_tool_uses)
    ThinkingBlocksCleared int // Thinking blocks removed (clear_thinking)
}
```

See [examples/providers/anthropic/context-management/](../../../examples/providers/anthropic/context-management/) for detailed examples.

## Available Tools

See [tools/README.md](tools/README.md) for complete documentation.

| Tool | Version | Description |
|------|---------|-------------|
| Computer | `20251124` | Mouse, keyboard, screenshot control with zoom |
| Bash | `20250124` | Persistent shell session |
| Text Editor | `20250728` | File viewing and editing |
| Code Execution | `20250825` | Python/bash in sandbox |
| Tool Search BM25 | `20251119` | Natural language tool discovery |
| Tool Search Regex | `20251119` | Pattern-based tool discovery |

## Configuration

### Provider Config

```go
provider := anthropic.New(anthropic.Config{
    APIKey:     "your-api-key",
    BaseURL:    "https://api.anthropic.com",  // Optional
    APIVersion: "2023-06-01",                  // Optional
})
```

### Environment Variables

```bash
export ANTHROPIC_API_KEY=your_api_key
```

## Streaming

```go
stream, err := ai.StreamText(ctx, ai.StreamTextOptions{
    Model: model,
    Messages: messages,
})

for chunk := range stream.TextStream() {
    fmt.Print(chunk)
}

if err := stream.Err(); err != nil {
    log.Fatal(err)
}
```

## Examples

See the `/examples/providers/anthropic/` directory for complete examples:
- `computer-use/` - Computer control
- `code-execution/` - Python and bash execution
- `tool-search/` - Large-scale tool discovery
- `context-management/` - Automatic conversation history cleanup

## Testing

```bash
cd pkg/providers/anthropic
go test -v ./...
```

## Documentation

- [Anthropic API Docs](https://docs.anthropic.com)
- [Computer Use Guide](https://docs.anthropic.com/en/docs/agents-and-tools/computer-use)
- [Code Execution Guide](https://docs.anthropic.com/en/docs/agents-and-tools/code-execution)
- [Tool Search Guide](https://docs.anthropic.com/en/docs/agents-and-tools/tool-search-tool)

## License

See the main repository LICENSE file.
