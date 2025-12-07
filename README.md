# Go AI SDK

A complete Go implementation of the [Vercel AI SDK](https://sdk.vercel.ai) with full feature parity for backend functionality.

Build AI-powered applications in Go with a unified API across 30+ model providers including OpenAI, Anthropic, Google, AWS Bedrock, Azure, Cohere, Mistral, and many more.

## Features

- **Unified Provider API**: Switch between 30+ providers with zero code changes
- **Text Generation**: `GenerateText()` and `StreamText()` with multi-step tool calling
- **Structured Output**: `GenerateObject()` for JSON schema-validated responses
- **Embeddings**: `Embed()` and `EmbedMany()` with similarity search utilities
- **Image Generation**: `GenerateImage()` for text-to-image generation
- **Speech**: `GenerateSpeech()` and `Transcribe()` for audio processing
- **Agents**: `ToolLoopAgent` for autonomous multi-step reasoning
- **Middleware System**: Model wrapping for cross-cutting concerns (logging, caching, rate limiting)
- **Telemetry**: Built-in OpenTelemetry integration
- **Registry System**: Resolve models by string ID (e.g., `"openai:gpt-4"`)

## Installation

```bash
go get github.com/digitallysavvy/go-ai
```

## Unified Provider Architecture

The Go AI SDK provides a unified interface across all providers, making it easy to switch between models or combine them in your application:

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/providers/openai"
    "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
    "github.com/digitallysavvy/go-ai/pkg/providers/google"
    "github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
)

// All providers implement the same interface
openaiProvider := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
anthropicProvider := anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")})
googleProvider := google.New(google.Config{APIKey: os.Getenv("GOOGLE_API_KEY")})
bedrockProvider := bedrock.New(bedrock.Config{Region: "us-east-1"})

// Get models with the same API
gpt4, _ := openaiProvider.LanguageModel("gpt-4")
claude, _ := anthropicProvider.LanguageModel("claude-3-sonnet-20240229")
gemini, _ := googleProvider.LanguageModel("gemini-pro")
titan, _ := bedrockProvider.LanguageModel("amazon.titan-text-express-v1")
```

## Usage Examples

### Generating Text

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

    // Get language model
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

### Streaming Text

```go
result, _ := ai.StreamText(ctx, ai.StreamTextOptions{
    Model:  model,
    Prompt: "Write a poem about Go programming",
})
defer result.Close()

// Stream text chunks as they arrive
for chunk := range result.Chunks() {
    fmt.Print(chunk.Text)
}
```

### Generating Structured Data

Use `GenerateObject` to generate JSON with schema validation:

```go
type Recipe struct {
    Name        string   `json:"name"`
    Ingredients []string `json:"ingredients"`
    Steps       []string `json:"steps"`
}

schema := map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "name":        map[string]interface{}{"type": "string"},
        "ingredients": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
        "steps":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
    },
    "required": []string{"name", "ingredients", "steps"},
}

result, _ := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
    Model:  model,
    Prompt: "Generate a recipe for chocolate chip cookies",
    Schema: schema,
})

var recipe Recipe
json.Unmarshal(result.Object, &recipe)
fmt.Printf("Recipe: %s\n", recipe.Name)
```

### Tool Calling

Enable AI models to call functions:

```go
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
    Execute: func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
        location := input["location"].(string)
        // Fetch actual weather data here
        return map[string]interface{}{
            "temperature": 72,
            "condition":   "sunny",
            "location":    location,
        }, nil
    },
}

result, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:  model,
    Prompt: "What's the weather in San Francisco?",
    Tools:  []types.Tool{weatherTool},
})
```

### Building Agents

Create autonomous agents that reason through multi-step tasks:

```go
import "github.com/digitallysavvy/go-ai/pkg/agent"

agentConfig := agent.AgentConfig{
    Model:    model,
    System:   "You are a helpful research assistant.",
    Tools:    []types.Tool{searchTool, calculatorTool, weatherTool},
    MaxSteps: 10,
}

researchAgent := agent.NewToolLoopAgent(agentConfig)

result, _ := researchAgent.Execute(ctx,
    "Find the population of Tokyo and calculate what percentage it is of Japan's total population")

fmt.Println(result.Text)
// Agent automatically:
// 1. Searches for Tokyo's population
// 2. Searches for Japan's population
// 3. Uses calculator to compute percentage
// 4. Returns formatted answer
```

### Embeddings

Generate embeddings for semantic search and similarity matching:

```go
embeddingModel, _ := provider.EmbeddingModel("text-embedding-3-small")

// Single embedding
result, _ := ai.Embed(ctx, ai.EmbedOptions{
    Model: embeddingModel,
    Input: "Go is a statically typed, compiled language",
})

// Batch embeddings
documents := []string{
    "Python is great for data science",
    "Go excels at concurrent programming",
    "Rust provides memory safety guarantees",
}

results, _ := ai.EmbedMany(ctx, ai.EmbedManyOptions{
    Model:  embeddingModel,
    Inputs: documents,
})

// Find most similar document
queryEmbedding, _ := ai.Embed(ctx, ai.EmbedOptions{
    Model: embeddingModel,
    Input: "best language for parallelism",
})

idx, score, _ := ai.FindMostSimilar(queryEmbedding.Embedding, results.Embeddings)
fmt.Printf("Most similar: %s (score: %.3f)\n", documents[idx], score)
// Output: Most similar: Go excels at concurrent programming (score: 0.892)
```

### Image Generation

Generate images from text prompts:

```go
imageModel, _ := openaiProvider.ImageModel("dall-e-3")

result, _ := ai.GenerateImage(ctx, ai.GenerateImageOptions{
    Model:  imageModel,
    Prompt: "A serene mountain landscape at sunset",
    Size:   "1024x1024",
})

// result.Image contains the image bytes
// result.URL contains the image URL (if available)
```

### Speech Generation and Transcription

```go
speechModel, _ := openaiProvider.SpeechModel("tts-1")

// Generate speech
result, _ := ai.GenerateSpeech(ctx, ai.GenerateSpeechOptions{
    Model: speechModel,
    Text:  "Hello, welcome to the Go AI SDK!",
    Voice: "alloy",
})

// result.Audio contains the audio bytes

// Transcribe audio
transcriptionModel, _ := openaiProvider.TranscriptionModel("whisper-1")

transcript, _ := ai.Transcribe(ctx, ai.TranscribeOptions{
    Model: transcriptionModel,
    Audio: audioBytes,
})

fmt.Println(transcript.Text)
```

### Using the Registry

The registry allows model resolution by string ID:

```go
import "github.com/digitallysavvy/go-ai/pkg/registry"

// Register providers
reg := registry.New()
reg.RegisterProvider("openai", openaiProvider)
reg.RegisterProvider("anthropic", anthropicProvider)
reg.RegisterProvider("google", googleProvider)

// Resolve models by string ID
model, _ := reg.LanguageModel("openai:gpt-4")
model2, _ := reg.LanguageModel("anthropic:claude-3-opus-20240229")
model3, _ := reg.LanguageModel("google:gemini-pro")

// Use consistently
result, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:  model,
    Prompt: "Hello!",
})
```

### Middleware

Add cross-cutting concerns like logging, caching, and rate limiting:

```go
import "github.com/digitallysavvy/go-ai/pkg/middleware"

// Create logging middleware
loggingMiddleware := &middleware.LanguageModelMiddleware{
    WrapGenerate: func(ctx context.Context, doGenerate func() (*types.GenerateResult, error),
        params *provider.GenerateParams, model provider.LanguageModel) (*types.GenerateResult, error) {

        log.Printf("Generating with model: %s", model.ModelID())
        start := time.Now()
        result, err := doGenerate()
        log.Printf("Generation completed in %v", time.Since(start))
        return result, err
    },
}

// Wrap model with middleware
wrappedModel := middleware.WrapLanguageModel(model, []*middleware.LanguageModelMiddleware{
    loggingMiddleware,
}, nil, nil)
```

## Supported Providers

| Provider | Language Models | Embedding Models | Image Models | Speech Models |
|----------|-----------------|------------------|--------------|---------------|
| OpenAI | GPT-4, GPT-3.5, o1 | text-embedding-3 | DALL-E 3 | TTS, Whisper |
| Anthropic | Claude 3 (Opus, Sonnet, Haiku) | - | - | - |
| Google | Gemini Pro, Gemini Flash | text-embedding | Imagen | - |
| Azure OpenAI | All Azure-hosted models | Azure embeddings | Azure DALL-E | Azure TTS |
| AWS Bedrock | Claude, Titan, Llama | Titan Embeddings | Stable Diffusion | - |
| Cohere | Command, Command-R | embed-english | - | - |
| Mistral | Mistral Large, Medium, Small | mistral-embed | - | - |
| Groq | Llama, Mixtral | - | - | Whisper |
| Together | Llama, Mixtral, Qwen | - | Stable Diffusion | - |
| Fireworks | Llama, Mixtral | nomic-embed | - | - |
| Perplexity | Sonar models | - | - | - |
| DeepSeek | DeepSeek Chat, Coder | - | - | - |
| xAI | Grok | - | - | - |
| Replicate | All hosted models | - | All image models | - |
| Hugging Face | Inference API models | - | - | - |
| Ollama | Local models | Local embeddings | - | - |
| LM Studio | Local models | - | - | - |
| ... and more | | | | |

## Project Structure

```
go-ai/
├── pkg/
│   ├── ai/           # Main SDK package (public API)
│   ├── provider/     # Provider interface definitions
│   ├── providers/    # Provider implementations (30+)
│   │   ├── openai/
│   │   ├── anthropic/
│   │   ├── google/
│   │   ├── bedrock/
│   │   ├── azure/
│   │   └── ...
│   ├── agent/        # Agent implementations
│   ├── middleware/   # Middleware system
│   ├── telemetry/    # OpenTelemetry integration
│   ├── streaming/    # Streaming utilities
│   ├── registry/     # Model registry
│   ├── schema/       # JSON Schema validation
│   └── internal/     # Internal utilities
└── examples/         # Example applications
```

## TypeScript Parity

This SDK maintains 1:1 feature parity with the [Vercel AI SDK](https://sdk.vercel.ai):

- Same function signatures (`GenerateText`, `StreamText`, `GenerateObject`, etc.)
- Same provider interfaces and middleware patterns
- Same embedding model metadata (`MaxEmbeddingsPerCall`, `SupportsParallelCalls`)
- Same error types and handling patterns
- Compatible with AI SDK workflows

## Community

Join our community to get help, share feedback, and contribute:

- [GitHub Issues](https://github.com/digitallysavvy/go-ai/issues) - Bug reports and feature requests
- [GitHub Discussions](https://github.com/digitallysavvy/go-ai/discussions) - Questions and ideas

## Contributing

Contributions are welcome! Please read our contributing guidelines before submitting a pull request.

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

## Authors

- [digitallysavvy](https://github.com/digitallysavvy)
