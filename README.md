# Go AI SDK

A complete Go implementation of the Vercel AI SDK with 1:1 feature parity for backend functionality.

## Features

### ✅ Implemented
- **Text Generation**: `GenerateText()` and `StreamText()` with multi-step tool calling
- **Structured Output**: `GenerateObject()` for JSON schema-validated responses
- **Embeddings**: `Embed()` and `EmbedMany()` with similarity search utilities
- **Agents**: `ToolLoopAgent` for autonomous multi-step reasoning
- **3 Major Providers**: OpenAI (GPT-4), Anthropic (Claude), Google (Gemini)
- **Registry System**: Resolve models by string ID (e.g., "openai:gpt-4")
- **Streaming**: Channel-based streaming with context cancellation
- **Tool Calling**: Automatic tool execution with approval workflows

### 🚧 In Progress
- **27+ Additional Providers**: Azure, Bedrock, Cohere, Mistral, etc.
- **Image Generation**: `GenerateImage()` for text-to-image
- **Speech**: `GenerateSpeech()` and `Transcribe()` for audio
- **Middleware System**: Model wrapping for cross-cutting concerns
- **Telemetry**: OpenTelemetry integration

## Installation

```bash
go get github.com/yourusername/go-ai
```

## Quick Start

### Basic Text Generation

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/yourusername/go-ai/pkg/ai"
    "github.com/yourusername/go-ai/pkg/providers/openai"
)

func main() {
    ctx := context.Background()

    // Create OpenAI provider
    provider := openai.New(openai.Config{
        APIKey: "your-api-key",
    })

    // Get language model
    model, _ := provider.LanguageModel("gpt-4")

    // Generate text
    result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
        Model:  model,
        Prompt: "Tell me a joke about programming",
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Text)
}
```

### Using Multiple Providers

```go
// OpenAI (GPT-4)
openaiProvider := openai.New(openai.Config{APIKey: "..."})
gpt4, _ := openaiProvider.LanguageModel("gpt-4")

// Anthropic (Claude)
anthropicProvider := anthropic.New(anthropic.Config{APIKey: "..."})
claude, _ := anthropicProvider.LanguageModel("claude-3-sonnet-20240229")

// Google (Gemini)
googleProvider := google.New(google.Config{APIKey: "..."})
gemini, _ := googleProvider.LanguageModel("gemini-pro")
```

### Streaming

```go
result, _ := ai.StreamText(ctx, ai.StreamTextOptions{
    Model:  model,
    Prompt: "Write a haiku about Go",
})
defer result.Close()

// Stream chunks
for chunk := range result.Chunks() {
    if chunk.Type == provider.ChunkTypeText {
        fmt.Print(chunk.Text)
    }
}
```

### Tool Calling

```go
weatherTool := types.Tool{
    Name:        "get_weather",
    Description: "Get current weather for a location",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "location": map[string]interface{}{"type": "string"},
        },
        "required": []string{"location"},
    },
    Execute: func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
        location := input["location"].(string)
        return map[string]interface{}{
            "temperature": 72,
            "condition":   "sunny",
        }, nil
    },
}

result, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:  model,
    Prompt: "What's the weather in San Francisco?",
    Tools:  []types.Tool{weatherTool},
})
```

### Embeddings

```go
embeddingModel, _ := provider.EmbeddingModel("text-embedding-ada-002")

// Single embedding
result, _ := ai.Embed(ctx, ai.EmbedOptions{
    Model: embeddingModel,
    Input: "Go is a great language",
})

// Batch embeddings
results, _ := ai.EmbedMany(ctx, ai.EmbedManyOptions{
    Model:  embeddingModel,
    Inputs: []string{"doc1", "doc2", "doc3"},
})

// Similarity search
idx, score, _ := ai.FindMostSimilar(queryEmbedding, results.Embeddings)
```

### Agents

```go
agentConfig := agent.AgentConfig{
    Model:  model,
    System: "You are a helpful assistant",
    Tools:  []types.Tool{calculatorTool, weatherTool},
    MaxSteps: 5,
}

agentInstance := agent.NewToolLoopAgent(agentConfig)
result, _ := agentInstance.Execute(ctx, "What's 15 times 23?")
fmt.Println(result.Text)
```

## Project Structure

```
go-ai/
├── pkg/
│   ├── ai/           # Main SDK package (public API)
│   ├── provider/     # Provider interface definitions
│   ├── providers/    # Provider implementations (30+)
│   ├── agent/        # Agent implementations
│   ├── middleware/   # Middleware system
│   ├── telemetry/    # Observability
│   ├── streaming/    # Streaming utilities
│   ├── registry/     # Model registry
│   ├── schema/       # Schema validation
│   └── internal/     # Internal utilities
└── examples/         # Example usage
```

## Status

✅ **Phases 1-7 Complete** (~50% toward feature parity)

### What Works Now
- ✅ Text generation (streaming & non-streaming)
- ✅ Structured output with JSON schemas
- ✅ Multi-step tool calling
- ✅ Embeddings with similarity utilities
- ✅ Autonomous agents (ToolLoopAgent)
- ✅ 3 major providers (OpenAI, Anthropic, Google)
- ✅ Registry system
- ✅ Provider utilities (SSE, tool conversion, prompt conversion)

### Next Steps
- ⏳ 27+ additional providers
- ⏳ Middleware system
- ⏳ Telemetry integration
- ⏳ Image & speech generation
- ⏳ Comprehensive testing

See [PROGRESS.md](PROGRESS.md) for detailed status.

## License

Apache 2.0
