# Go AI SDK Documentation

Welcome to the Go AI SDK documentation. This is a complete Go implementation of the Vercel AI SDK with full feature parity for backend functionality.

Build AI-powered applications in Go with a unified API across 26+ model providers including OpenAI, Anthropic, Google, AWS Bedrock, Azure, Cohere, Mistral, and many more.

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
    ctx := context.Background()

    // Create provider
    provider := openai.New(openai.Config{
        APIKey: os.Getenv("OPENAI_API_KEY"),
    })

    // Get model
    model, _ := provider.LanguageModel("gpt-4")

    // Generate text
    result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
        Model:  model,
        Prompt: "What is love?",
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Text)
}
```

## Documentation Sections

### [Foundations](./foundations/01-overview.md)

Core concepts for understanding the Go AI SDK:

- [**Overview**](./foundations/01-overview.md) - Introduction to AI concepts
- [**Providers and Models**](./foundations/02-providers-and-models.md) - Available providers and model capabilities
- [**Prompts**](./foundations/03-prompts.md) - Text, message, and system prompts
- [**Tools**](./foundations/04-tools.md) - Function calling and tool usage
- [**Streaming**](./foundations/05-streaming.md) - Why and how to use streaming

### [Core API](./core/01-overview.md)

Main SDK functionality for building AI applications:

- [**Overview**](./core/01-overview.md) - Core API introduction
- [**Generating Text**](./core/02-generating-text.md) - `GenerateText` and `StreamText`
- [**Generating Structured Data**](./core/03-generating-structured-data.md) - `GenerateObject` and `StreamObject`
- [**Tools and Tool Calling**](./core/04-tools-and-tool-calling.md) - Multi-step tool execution
- [**Embeddings**](./core/05-embeddings.md) - `Embed` and `EmbedMany` with similarity functions
- [**Reranking**](./core/06-reranking.md) - Document reranking
- [**Image Generation**](./core/07-image-generation.md) - Text-to-image generation
- [**Speech Generation**](./core/08-speech-generation.md) - Text-to-speech
- [**Transcription**](./core/09-transcription.md) - Speech-to-text
- [**Settings**](./core/10-settings.md) - Model parameters and configuration
- [**Middleware**](./core/11-middleware.md) - Model wrapping and middleware
- [**Provider Management**](./core/12-provider-management.md) - Registry and dynamic model selection
- [**Error Handling**](./core/13-error-handling.md) - Error types and handling patterns
- [**Testing**](./core/14-testing.md) - Testing strategies
- [**Telemetry**](./core/15-telemetry.md) - OpenTelemetry integration

### [Agents](./agents/01-overview.md)

Build autonomous agents:

- [**Overview**](./agents/01-overview.md) - Agent concepts
- [**Tool Loop Agent**](./agents/02-tool-loop-agent.md) - Multi-step reasoning
- [**Advanced Patterns**](./agents/03-advanced-patterns.md) - Complex agent workflows

### [Advanced Topics](./advanced/01-prompt-engineering.md)

Advanced patterns and optimizations:

- [**Prompt Engineering**](./advanced/01-prompt-engineering.md) - Effective prompting strategies
- [**Caching**](./advanced/02-caching.md) - Response caching
- [**Rate Limiting**](./advanced/03-rate-limiting.md) - Rate limit handling
- [**Backpressure**](./advanced/04-backpressure.md) - Stream backpressure
- [**Sequential Generations**](./advanced/05-sequential-generations.md) - Chaining generations
- [**Model as Router**](./advanced/06-model-as-router.md) - Dynamic model selection

### [Providers](./providers/01-overview.md)

Provider-specific documentation for 26+ providers:

- OpenAI, Anthropic, Google, Azure, Bedrock, Cohere, Mistral, Groq, and more
- Configuration and setup
- Provider-specific features
- Model capabilities

### [API Reference](./reference/)

Complete API documentation:

- **[AI Package](./reference/ai/)** - Core functions (GenerateText, StreamText, etc.)
- **[Provider Interfaces](./reference/providers/)** - LanguageModel, EmbeddingModel, etc.
- **[Middleware](./reference/middleware/)** - Middleware types and functions
- **[Registry](./reference/registry/)** - Provider registry
- **[Schema](./reference/schema/)** - Schema validation
- **[Types](./reference/types/)** - Message, Tool, Error types

### [Examples](./examples/)

Practical examples:

- [**Text Generation**](./examples/01-text-generation.md)
- [**Streaming**](./examples/02-streaming.md)
- [**Structured Output**](./examples/03-structured-output.md)
- [**Tool Calling**](./examples/04-tool-calling.md)
- [**Embeddings**](./examples/05-embeddings.md)
- [**Agents**](./examples/06-agents.md)
- [**Middleware**](./examples/07-middleware.md)
- [**Multi-Provider**](./examples/08-multi-provider.md)

### [Migration](./migration/)

Migration guides:

- [**From TypeScript AI SDK**](./migration/from-typescript.md)
- [**From LangChain**](./migration/from-langchain.md)

### [Troubleshooting](./troubleshooting/)

Common issues and solutions:

- [**Common Errors**](./troubleshooting/common-errors.md)
- [**Rate Limits**](./troubleshooting/rate-limits.md)
- [**Debugging**](./troubleshooting/debugging.md)

## Key Features

- **Unified Provider API**: Switch between 26+ providers with zero code changes
- **Text Generation**: `GenerateText()` and `StreamText()` with multi-step tool calling
- **Structured Output**: `GenerateObject()` for JSON schema-validated responses
- **Embeddings**: `Embed()` and `EmbedMany()` with similarity search utilities
- **Image Generation**: `GenerateImage()` for text-to-image generation
- **Speech**: `GenerateSpeech()` and `Transcribe()` for audio processing
- **Agents**: `ToolLoopAgent` for autonomous multi-step reasoning
- **Middleware System**: Model wrapping for cross-cutting concerns
- **Telemetry**: Built-in OpenTelemetry integration
- **Registry System**: Resolve models by string ID

## Installation

```bash
go get github.com/digitallysavvy/go-ai
```

## Supported Providers

The Go AI SDK includes 26 built-in providers:

**Language Models**: OpenAI, Anthropic, Google, Azure, Bedrock, Cohere, Mistral, Groq, DeepSeek, xAI, Perplexity, Together AI, Fireworks, Replicate, Hugging Face, Ollama

**Image Models**: OpenAI, Azure, Bedrock, Stability AI, Black Forest Labs, Fal.ai, Together AI, Fireworks, Replicate

**Speech & Transcription**: OpenAI, Azure, ElevenLabs, Deepgram, AssemblyAI, Groq

**Infrastructure**: Baseten, Cerebras, DeepInfra, Vercel AI

## Go-Specific Advantages

- **Idiomatic Go**: Uses `context.Context`, channels, and error returns
- **Type Safety**: Strong typing with Go's type system
- **Performance**: Compiled language performance for production workloads
- **Concurrency**: Built-in support for concurrent operations with goroutines
- **Easy Deployment**: Single binary deployment without runtime dependencies
- **Standard Library**: Seamless integration with Go's standard library

## TypeScript Parity

This SDK maintains 1:1 feature parity with the [Vercel AI SDK](https://sdk.vercel.ai) for all server-side functionality:

- Same function signatures (`GenerateText`, `StreamText`, `GenerateObject`, etc.)
- Same provider interfaces and middleware patterns
- Same error types and handling patterns
- Compatible workflows and patterns

## Community

- [GitHub Repository](https://github.com/digitallysavvy/go-ai)
- [GitHub Issues](https://github.com/digitallysavvy/go-ai/issues) - Bug reports and feature requests
- [GitHub Discussions](https://github.com/digitallysavvy/go-ai/discussions) - Questions and ideas

## Contributing

Contributions are welcome! Please see our [Contributing Guide](../CONTRIBUTING.md) for details.

## License

Apache 2.0 - See [LICENSE](../LICENSE) for details.

---

## Navigation

- **New to AI development?** Start with [Foundations](./foundations/01-overview.md)
- **Ready to build?** Jump to [Core API](./core/01-overview.md)
- **Coming from TypeScript?** Check the [Migration Guide](./migration/from-typescript.md)
- **Need specific functionality?** Browse the [API Reference](./reference/)
