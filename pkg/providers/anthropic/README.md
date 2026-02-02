# Anthropic Provider

The Anthropic provider enables access to Claude models including Opus 4.5, Sonnet 4.5, and other Claude variants.

## Features

- Full Claude API support
- Computer use tools
- Code execution capabilities
- Tool search for large tool catalogs
- Streaming and non-streaming responses
- Tool calling and function execution

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

### Claude Opus 4.5
- Model ID: `claude-opus-4-20250514`
- Best for: Complex tasks, advanced reasoning
- Supports: Computer use, tool search, code execution

### Claude Sonnet 4.5
- Model ID: `claude-sonnet-4-20250514`
- Best for: Balanced performance and cost
- Supports: Tool search, code execution

### Claude Sonnet 4
- Model ID: `claude-sonnet-4-20241022`
- Best for: General purpose tasks

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
