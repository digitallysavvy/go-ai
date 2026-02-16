# Go AI SDK

The [Go AI SDK](https://github.com/digitallysavvy/go-ai) is a comprehensive toolkit designed to help you build AI-powered applications and agents using Go. It provides 1:1 feature parity with the [Vercel AI SDK](https://ai-sdk.dev) for backend functionality.

To learn more about how to use the Go AI SDK, check out our [Documentation](./docs).

## Installation

You will need Go 1.21+ installed on your local development machine.

```bash
go get github.com/digitallysavvy/go-ai
```

## Unified Provider Architecture

The Go AI SDK provides a [unified API](./docs/02-foundations/02-providers-and-models.mdx) to interact with model providers like [OpenAI](https://platform.openai.com), [Anthropic](https://www.anthropic.com), [Google](https://ai.google.dev), and [more](#supported-providers).

```bash
go get github.com/digitallysavvy/go-ai/pkg/providers/openai
go get github.com/digitallysavvy/go-ai/pkg/providers/anthropic
go get github.com/digitallysavvy/go-ai/pkg/providers/google
```

## Usage

### Generating Text

```go
import (
    "context"
    "fmt"
    "os"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
    ctx := context.Background()

    provider := openai.New(openai.Config{
        APIKey: os.Getenv("OPENAI_API_KEY"),
    })
    model, _ := provider.LanguageModel("gpt-4")

    result, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
        Model:  model,
        Prompt: "What is an agent?",
    })

    fmt.Println(result.Text)
}
```

### Streaming Text

```go
stream, _ := ai.StreamText(ctx, ai.StreamTextOptions{
    Model:  model,
    Prompt: "Write a story about Go programming",
})

for chunk := range stream.TextChannel {
    fmt.Print(chunk)
}
```

### Generating Structured Data

```go
import "github.com/digitallysavvy/go-ai/pkg/schema"

type Recipe struct {
    Name        string   `json:"name"`
    Ingredients []string `json:"ingredients"`
    Steps       []string `json:"steps"`
}

recipeSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "name":        map[string]interface{}{"type": "string"},
        "ingredients": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
        "steps":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
    },
    "required": []string{"name", "ingredients", "steps"},
})

result, _ := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
    Model:  model,
    Prompt: "Generate a lasagna recipe.",
    Schema: recipeSchema,
})

var recipe Recipe
json.Unmarshal([]byte(result.Object), &recipe)
fmt.Printf("Recipe: %s\n", recipe.Name)
```

### Agents

Build autonomous agents with multi-step reasoning:

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/agent"
    "github.com/digitallysavvy/go-ai/pkg/provider/types"
)

myAgent := agent.New(agent.Config{
    Model:        model,
    Instructions: "You are a helpful research assistant.",
    Tools: []types.Tool{
        searchTool,
        calculatorTool,
    },
    MaxSteps: 10,
})

result, _ := myAgent.Execute(ctx, "What is the population of Tokyo?")
fmt.Println(result.Text)
```

### Tool Calling

Extend AI capabilities with custom tools:

```go
import "github.com/digitallysavvy/go-ai/pkg/provider/types"

weatherTool := types.Tool{
    Name:        "get_weather",
    Description: "Get current weather for a location",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "location": map[string]interface{}{
                "type":        "string",
                "description": "City name",
            },
        },
        "required": []string{"location"},
    },
    Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
        location := params["location"].(string)
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

Generate embeddings for semantic search:

```go
embeddingModel, _ := provider.EmbeddingModel("text-embedding-3-small")

result, _ := ai.Embed(ctx, ai.EmbedOptions{
    Model: embeddingModel,
    Input: "Go is great for building AI applications",
})

// result.Embedding contains the vector
```

### Image Generation

```go
imageModel, _ := provider.ImageModel("dall-e-3")

result, _ := ai.GenerateImage(ctx, ai.GenerateImageOptions{
    Model:  imageModel,
    Prompt: "A serene mountain landscape at sunset",
    Size:   "1024x1024",
})

// result.Image contains the generated image bytes
```

### Speech and Transcription

```go
// Generate speech
speechModel, _ := provider.SpeechModel("tts-1")
result, _ := ai.GenerateSpeech(ctx, ai.GenerateSpeechOptions{
    Model: speechModel,
    Text:  "Hello, welcome to the Go AI SDK!",
    Voice: "alloy",
})

// Transcribe audio
transcriptionModel, _ := provider.TranscriptionModel("whisper-1")
transcript, _ := ai.Transcribe(ctx, ai.TranscribeOptions{
    Model: transcriptionModel,
    Audio: audioBytes,
})
```

### Memory Optimization

Reduce memory consumption by 50-80% for image-heavy or large-context workloads using retention settings:

```go
import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// Enable memory optimization
retention := &types.RetentionSettings{
    RequestBody:  types.BoolPtr(false), // Don't retain request
    ResponseBody: types.BoolPtr(false), // Don't retain response
}

result, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:                 model,
    Prompt:                "Analyze this image...",
    ExperimentalRetention: retention,
})

// Result still has usage, finish reason, text, etc.
// But raw request/response bodies are excluded to save memory
```

Perfect for:
- 🖼️ Image processing workloads
- 📄 Large document analysis
- 🔒 Privacy-sensitive applications
- ⚡ Long-running services

See [examples/features/retention](./examples/features/retention) for detailed usage.

## Supported Providers

The Go AI SDK supports 26+ providers:

| Provider         | Language Models             | Embeddings | Images           | Speech       |
| ---------------- | --------------------------- | ---------- | ---------------- | ------------ |
| **OpenAI**       | GPT-4, GPT-3.5, O1          | ✓          | DALL-E           | TTS, Whisper |
| **Anthropic**    | Claude 3.5 Sonnet, Claude 3 | -          | -                | -            |
| **Google**       | Gemini Pro, Flash           | ✓          | -                | -            |
| **AWS Bedrock**  | Claude, Titan, Llama        | ✓          | -                | -            |
| **Azure OpenAI** | Azure-hosted models         | ✓          | ✓                | ✓            |
| **Mistral**      | Large, Medium, Small        | ✓          | -                | -            |
| **Cohere**       | Command R+, Command         | ✓          | -                | -            |
| **Groq**         | Llama, Mixtral              | -          | -                | Whisper      |
| **Together AI**  | Llama, Mixtral, Qwen        | -          | Stable Diffusion | -            |
| **Fireworks**    | Llama, Mixtral              | ✓          | -                | -            |
| **Perplexity**   | Sonar models                | -          | -                | -            |
| **DeepSeek**     | DeepSeek Chat, Coder        | -          | -                | -            |
| **xAI**          | Grok                        | -          | -                | -            |
| **Replicate**    | All hosted models           | -          | ✓                | -            |
| **Hugging Face** | Inference API               | -          | -                | -            |
| **Ollama**       | Local models                | ✓          | -                | -            |

And many more...

## Features

- ✅ **Unified API** - One interface for 26+ providers
- ✅ **Text Generation** - `GenerateText()` and `StreamText()`
- ✅ **Structured Output** - Type-safe `GenerateObject()` with JSON validation
- ✅ **Tool Calling** - Extend models with custom functions
- ✅ **Agents** - Autonomous multi-step reasoning with `ToolLoopAgent`
- ✅ **Embeddings** - Generate and search with vector embeddings
- ✅ **Image Generation** - Text-to-image with multiple providers
- ✅ **Speech** - TTS and transcription capabilities
- ✅ **Middleware** - Logging, caching, rate limiting, and more
- ✅ **Telemetry** - Built-in OpenTelemetry integration
- ✅ **Registry** - Resolve models by string ID (e.g., `"openai:gpt-4"`)
- ✅ **Context Support** - Native Go context cancellation and timeouts
- ✅ **Streaming** - Real-time responses with automatic backpressure
- ✅ **Memory Optimization** - Retention settings for 50-80% memory reduction

## Why Go for AI?

While Python dominates AI/ML model training, **Go excels at building production AI applications**:

- 🚀 **Performance** - Fast execution and low memory overhead
- ⚡ **Concurrency** - Native goroutines and channels for parallel processing
- 📦 **Single Binary** - No dependencies, easy deployment
- 🔒 **Type Safety** - Catch errors at compile time
- ☁️ **Cloud Native** - Perfect for Kubernetes, Docker, microservices
- 🏢 **Production Ready** - Built for scalable backend systems

## Examples

We provide **50+ production-ready examples** covering every feature. See the [examples directory](./examples) for complete working code.

### 🚀 HTTP Servers (5 examples)
- **[http-server](./examples/http-server)** - Standard `net/http` with SSE streaming
- **[gin-server](./examples/gin-server)** - Gin framework integration
- **[echo-server](./examples/echo-server)**, **[fiber-server](./examples/fiber-server)**, **[chi-server](./examples/chi-server)** - More frameworks

### 📦 Structured Output (4 examples)
- **[generate-object](./examples/generate-object)** - Type-safe JSON generation (basic, validation, complex)
- **[stream-object](./examples/stream-object)** - Real-time structured streaming

### 🤖 Provider Features (8 examples)
- **[OpenAI](./examples/providers/openai)** - Reasoning (o1), structured outputs, vision
- **[Anthropic](./examples/providers/anthropic)** - Caching, extended thinking, PDF support
- **[Google](./examples/providers/google)**, **[Azure](./examples/providers/azure)** - Integration patterns

### 🧠 Agents (5 examples)
- **[math-agent](./examples/agents/math-agent)** - Multi-tool math solver
- **[web-search-agent](./examples/agents/web-search-agent)** - Research and fact-checking
- **[streaming-agent](./examples/agents/streaming-agent)** - Real-time step visualization
- **[multi-agent](./examples/agents/multi-agent)**, **[supervisor-agent](./examples/agents/supervisor-agent)** - Coordinated systems

### 🛠️ Production Patterns (7 examples)
- **[Middleware](./examples/middleware)** - Logging, caching, rate limiting, retry, telemetry
- **[Testing](./examples/testing)** - Unit and integration test patterns
- **[MCP](./examples/mcp)** - Model Context Protocol (stdio, HTTP, auth, tools)

### 🎨 Multimodal (5 examples)
- **[Image Generation](./examples/image-generation)** - DALL-E, Stable Diffusion
- **[Speech](./examples/speech)** - Text-to-speech and speech-to-text
- **[Multimodal Audio](./examples/multimodal/audio)** - Audio analysis patterns

### 🔬 Advanced (4 examples)
- **[Reranking](./examples/rerank)** - Document reranking for search quality
- **[Semantic Router](./examples/complex/semantic-router)** - AI-based intent classification
- **[Benchmarks](./examples/benchmarks)** - Throughput and latency measurement

**All examples:**
- ✅ Compile and pass `go vet`
- ✅ Include comprehensive README with usage
- ✅ Follow Go best practices
- ✅ Work with real API calls

[Browse all 50+ examples →](./examples)

## Documentation

- **[Getting Started](./docs/02-getting-started)** - Quick start guide
- **[Foundations](./docs/02-foundations)** - Core concepts
- **[AI SDK Core](./docs/03-ai-sdk-core)** - Complete API reference
- **[Agents](./docs/03-agents)** - Building autonomous agents
- **[Advanced](./docs/06-advanced)** - Production patterns

## TypeScript Parity

This SDK maintains 1:1 feature parity with the [Vercel AI SDK](https://ai-sdk.dev) for backend functionality:

- Same function signatures and patterns
- Same provider interfaces
- Same middleware system
- Compatible workflows
- Feature complete for server-side use

## Community

The Go AI SDK community can be found on GitHub where you can ask questions, voice ideas, and share your projects:

- **[GitHub Discussions](https://github.com/digitallysavvy/go-ai/discussions)** - Ask questions and share ideas
- **[GitHub Issues](https://github.com/digitallysavvy/go-ai/issues)** - Report bugs and request features

## Contributing

Contributions to the Go AI SDK are welcome and highly appreciated. However, before you jump right into it, we would like you to review our [Contribution Guidelines](./CONTRIBUTING.md) to make sure you have a smooth experience contributing to the Go AI SDK.

## License

Apache 2.0 - See [LICENSE](./LICENSE) for details.

## Authors

This library is created by [@digitallysavvy](https://github.com/digitallysavvy), inspired by the [Vercel AI SDK](https://ai-sdk.dev), with contributions from the [Open Source Community](https://github.com/digitallysavvy/go-ai/graphs/contributors).
