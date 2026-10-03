# Go AI SDK Documentation

Welcome to the Go AI SDK documentation. This is a complete Go implementation of the Vercel AI SDK with full feature parity for backend functionality.

Build AI-powered applications in Go with a unified API across 49 model providers including OpenAI, Anthropic, Google, AWS Bedrock, Azure, Cohere, Mistral, and many more.

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

### [Introduction](./00-introduction/index.mdx)

What the Go AI SDK is and how it maps to the Vercel AI SDK.

### [Foundations](./02-foundations/index.mdx)

Core concepts for understanding the Go AI SDK:

- [**Overview**](./02-foundations/01-overview.mdx) - Introduction to AI concepts
- [**Providers and Models**](./02-foundations/02-providers-and-models.mdx) - Available providers and model capabilities
- [**Prompts**](./02-foundations/03-prompts.mdx) - Text, message, and system prompts
- [**Tools**](./02-foundations/04-tools.mdx) - Function calling and tool usage
- [**Streaming**](./02-foundations/05-streaming.mdx) - Why and how to use streaming

### [Getting Started](./02-getting-started/index.mdx)

Installation and your first request with the Go AI SDK.

### [Core API](./03-ai-sdk-core/index.mdx)

Main SDK functionality for building AI applications:

- [**Overview**](./03-ai-sdk-core/01-overview.mdx) - Core API introduction
- [**Generating Text**](./03-ai-sdk-core/05-generating-text.mdx) - `GenerateText` and `StreamText`
- [**Generating Structured Data**](./03-ai-sdk-core/10-generating-structured-data.mdx) - `GenerateObject` and `StreamObject`
- [**Tools and Tool Calling**](./03-ai-sdk-core/15-tools-and-tool-calling.mdx) - Multi-step tool execution
- [**Embeddings**](./03-ai-sdk-core/30-embeddings.mdx) - `Embed` and `EmbedMany` with similarity functions
- [**Reranking**](./03-ai-sdk-core/31-reranking.mdx) - Document reranking
- [**Image Generation**](./03-ai-sdk-core/35-image-generation.mdx) - Text-to-image generation
- [**Speech Generation**](./03-ai-sdk-core/37-speech.mdx) - Text-to-speech
- [**Transcription**](./03-ai-sdk-core/36-transcription.mdx) - Speech-to-text
- [**Settings**](./03-ai-sdk-core/25-settings.mdx) - Model parameters and configuration
- [**Middleware**](./03-ai-sdk-core/40-middleware.mdx) - Model wrapping and middleware
- [**Provider Management**](./03-ai-sdk-core/45-provider-management.mdx) - Registry and dynamic model selection
- [**Error Handling**](./03-ai-sdk-core/50-error-handling.mdx) - Error types and handling patterns
- [**Testing**](./03-ai-sdk-core/55-testing.mdx) - Testing strategies
- [**Telemetry**](./03-ai-sdk-core/60-telemetry.mdx) - OpenTelemetry integration

### [Agents](./03-agents/index.mdx)

Build autonomous agents:

- [**Overview**](./03-agents/01-overview.mdx) - Agent concepts
- [**Building Agents**](./03-agents/02-building-agents.mdx) - `ToolLoopAgent` and multi-step reasoning
- [**Workflows**](./03-agents/03-workflows.mdx) - Composing agents into workflows

### [Advanced Guides](./04-advanced/index.mdx)

In-depth application patterns:

- [**Memory Management**](./04-advanced/memory-management.mdx) - Sliding window, compaction, and external memory stores
- [**Coding Agents**](./04-advanced/coding-agents.mdx) - Generate, execute, and refine code

### [Providers](./05-providers/index.mdx)

Provider-specific documentation for 45+ providers:

- OpenAI, Anthropic, Google, Azure, Bedrock, Cohere, Mistral, Groq, and more
- Configuration and setup
- Provider-specific features
- Model capabilities

### [Advanced](./06-advanced/index.mdx)

Advanced topics and concepts:

- [**Prompt Engineering**](./06-advanced/01-prompt-engineering.mdx) - Effective prompting strategies
- [**Caching**](./06-advanced/04-caching.mdx) - Response caching
- [**Rate Limiting**](./06-advanced/06-rate-limiting.mdx) - Rate limit handling
- [**Backpressure**](./06-advanced/03-backpressure.mdx) - Stream backpressure
- [**Sequential Generations**](./06-advanced/09-sequential-generations.mdx) - Chaining generations
- [**Model as Router**](./06-advanced/08-model-as-router.mdx) - Dynamic model selection

### [API Reference](./07-reference/index.mdx)

Complete API documentation:

- **[AI Package](./07-reference/ai/)** - Core functions (GenerateText, StreamText, etc.)
- **[Provider Interfaces](./07-reference/providers/)** - LanguageModel, EmbeddingModel, etc.
- **[Middleware](./07-reference/middleware/)** - Middleware types and functions
- **[Registry](./07-reference/registry/)** - Provider registry
- **[Schema](./07-reference/schema/)** - Schema validation
- **[Types](./07-reference/types/)** - Message, Tool, Error types

### [Examples](https://github.com/digitallysavvy/go-ai/tree/main/examples)

Practical, runnable examples live in the [`examples/`](https://github.com/digitallysavvy/go-ai/tree/main/examples) directory at the
repository root, organized by feature (`generate-text/`, `agents/`, `embed/`, `mcp/`,
and more).

### [Migration Guides](./08-migration-guides/)

- [**From TypeScript AI SDK**](./08-migration-guides/from-typescript-ai-sdk.mdx)
- [**From v0.4.x to v0.5.0**](./08-migration-guides/from-v0.4-to-v0.5.mdx)

### [Troubleshooting](./09-troubleshooting/index.mdx)

Common issues and solutions:

- [**Common Errors**](./09-troubleshooting/01-common-errors.mdx)
- [**Rate Limits**](./09-troubleshooting/03-rate-limits.mdx)
- [**Debugging**](./09-troubleshooting/09-debugging.mdx)

## Key Features

- **Unified Provider API**: Switch between 49 providers with zero code changes
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

Contributions are welcome! Please see our [Contributing Guide](https://github.com/digitallysavvy/go-ai/blob/main/CONTRIBUTING.md) for details.

## License

Apache 2.0 - See [LICENSE](https://github.com/digitallysavvy/go-ai/blob/main/LICENSE) for details.

## Trademarks

Go is a trademark of Google.  
The Go gopher, whenever used, is an original creation by Renée French.

---

## Navigation

- **New to AI development?** Start with [Foundations](./02-foundations/01-overview.mdx)
- **Ready to build?** Jump to [Core API](./03-ai-sdk-core/01-overview.mdx)
- **Coming from TypeScript?** Check the [Migration Guide](./08-migration-guides/from-typescript-ai-sdk.mdx)
- **Need specific functionality?** Browse the [API Reference](./07-reference/)
