# Open Responses Provider

The Open Responses provider enables compatibility with local LLMs (like LMStudio, Ollama) and other services that implement the OpenAI Responses API format. This provider acts as a compatibility layer for non-hosted AI services.

## Features

- ✅ Text generation and streaming
- ✅ Tool calling (for capable models)
- ✅ Image understanding (for vision models)
- ✅ Multi-modal content support
- ✅ Reasoning content handling
- ✅ Detailed token usage tracking
- ✅ Custom base URLs and headers
- ✅ Optional authentication

## Supported Services

The Open Responses provider works with any service implementing the OpenAI Responses API format:

- **LMStudio** - Popular local model runner
- **Ollama** - Local model management (via OpenAI-compatible endpoint)
- **Jan** - Desktop AI application
- **LocalAI** - Self-hosted OpenAI API compatible server
- **Text Generation Web UI** - Gradio-based interface
- **vLLM** - High-performance inference engine
- **Any other service** implementing OpenAI Responses API

## Installation

```bash
go get github.com/digitallysavvy/go-ai
```

## Quick Start

### Basic Usage with LMStudio

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
)

func main() {
	// Create provider pointing to LMStudio
	provider := openresponses.New(openresponses.Config{
		BaseURL: "http://localhost:1234/v1",
		// No API key needed for LMStudio
	})

	// Get language model
	model, err := provider.LanguageModel("local-model")
	if err != nil {
		log.Fatal(err)
	}

	// Generate text
	result, err := ai.GenerateText(context.Background(), model, "Explain Go interfaces in simple terms")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Text)
}
```

### Streaming Text

```go
// Stream text generation
stream, err := ai.StreamText(context.Background(), model, "Write a story about...")
if err != nil {
	log.Fatal(err)
}
defer stream.Close()

// Read chunks
for {
	chunk, err := stream.Next()
	if err == io.EOF {
		break
	}
	if err != nil {
		log.Fatal(err)
	}

	if chunk.Type == provider.ChunkTypeText {
		fmt.Print(chunk.Text)
	}
}
```

### Tool Calling

```go
// Define tools
weatherTool := types.Tool{
	Name:        "get_weather",
	Description: "Get the current weather for a location",
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
	Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
		location := input["location"].(string)
		return fmt.Sprintf("Weather in %s: Sunny, 72°F", location), nil
	},
}

// Generate with tools
result, err := ai.GenerateText(
	context.Background(),
	model,
	"What's the weather in San Francisco?",
	ai.WithTools([]types.Tool{weatherTool}),
)
```

### Image Understanding (Vision Models)

```go
import "os"

// Read image
imageData, err := os.ReadFile("photo.jpg")
if err != nil {
	log.Fatal(err)
}

// Create message with image
messages := []types.Message{
	{
		Role: types.RoleUser,
		Content: []types.ContentPart{
			types.TextContent{Text: "What's in this image?"},
			types.ImageContent{
				Image:    imageData,
				MimeType: "image/jpeg",
			},
		},
	},
}

// Generate with vision model (e.g., LLaVA via LMStudio)
result, err := ai.GenerateText(
	context.Background(),
	model,
	messages,
)
```

## Configuration

### Provider Configuration

```go
type Config struct {
	// BaseURL is the base URL for the Open Responses API endpoint (required)
	// Examples:
	//   - LMStudio: "http://localhost:1234/v1"
	//   - Ollama: "http://localhost:11434/v1"
	//   - LocalAI: "http://localhost:8080/v1"
	BaseURL string

	// APIKey is the optional API key for authentication
	// Many local model servers don't require authentication
	APIKey string

	// Headers are custom HTTP headers to include in requests
	Headers map[string]string

	// Name is the provider name for identification (default: "open-responses")
	Name string
}
```

### Example Configurations

#### LMStudio
```go
provider := openresponses.New(openresponses.Config{
	BaseURL: "http://localhost:1234/v1",
})
```

#### Ollama (OpenAI-compatible mode)
```go
provider := openresponses.New(openresponses.Config{
	BaseURL: "http://localhost:11434/v1",
})
```

#### LocalAI
```go
provider := openresponses.New(openresponses.Config{
	BaseURL: "http://localhost:8080/v1",
	APIKey:  "your-api-key", // If configured
})
```

#### Custom Service with Headers
```go
provider := openresponses.New(openresponses.Config{
	BaseURL: "https://my-custom-service.com/v1",
	APIKey:  "your-api-key",
	Headers: map[string]string{
		"X-Custom-Header": "value",
	},
	Name: "my-custom-provider",
})
```

## Setting Up LMStudio

1. **Download LMStudio** from https://lmstudio.ai/
2. **Download a model**:
   - Open LMStudio
   - Go to "Discover" tab
   - Download a model (e.g., Mistral-7B, Llama-2-7B)
3. **Start the local server**:
   - Click "Local Server" in the left sidebar
   - Select your model
   - Click "Start Server"
   - Default endpoint: `http://localhost:1234/v1`
4. **Use with Go-AI SDK**: Connect using the configuration above

## Supported Features

### Text Generation
- ✅ Non-streaming generation
- ✅ Streaming generation
- ✅ System messages
- ✅ Multi-turn conversations
- ✅ Temperature control
- ✅ Max tokens
- ✅ Top-P sampling
- ✅ Frequency/presence penalties

### Tool Calling
- ✅ Function tool definitions
- ✅ Tool execution
- ✅ Tool choice control (auto/none/required/specific)
- ✅ Parallel tool calls
- ✅ Tool result handling

### Content Types
- ✅ Text content
- ✅ Image content (URL and base64)
- ✅ Reasoning content
- ✅ Tool results

### Unsupported Features
The following features are not supported by the OpenAI Responses API format and will generate warnings:
- ❌ Stop sequences
- ❌ Top-K sampling
- ❌ Seed value

## Error Handling

The provider returns clear error messages for common issues:

```go
result, err := ai.GenerateText(ctx, model, prompt)
if err != nil {
	// Handle errors
	if errors.Is(err, ErrServiceNotRunning) {
		fmt.Println("Local service is not running. Please start LMStudio.")
	} else if errors.Is(err, ErrModelNotLoaded) {
		fmt.Println("Model is not loaded. Please load a model in LMStudio.")
	} else {
		log.Fatal(err)
	}
}
```

Common error scenarios:
- **Model not loaded/available**: Ensure the model is loaded in your local service
- **Service not running**: Verify the local service (LMStudio, Ollama, etc.) is running
- **Insufficient VRAM**: Check if your model fits in available GPU memory
- **Unsupported features**: Some models may not support tools or vision
- **Connection failures**: Verify the base URL and network connectivity

## Token Usage

The provider tracks detailed token usage:

```go
result, err := ai.GenerateText(ctx, model, prompt)
if err != nil {
	log.Fatal(err)
}

// Access usage information
fmt.Printf("Input tokens: %d\n", *result.Usage.InputTokens)
fmt.Printf("Output tokens: %d\n", *result.Usage.OutputTokens)
fmt.Printf("Total tokens: %d\n", *result.Usage.TotalTokens)

// Detailed breakdown (if available)
if result.Usage.InputDetails != nil {
	fmt.Printf("Cached tokens: %d\n", *result.Usage.InputDetails.CacheReadTokens)
}
if result.Usage.OutputDetails != nil {
	fmt.Printf("Reasoning tokens: %d\n", *result.Usage.OutputDetails.ReasoningTokens)
}
```

## Troubleshooting

### Service Connection Issues

**Problem**: Connection refused or timeout errors

**Solutions**:
1. Verify the service is running: `curl http://localhost:1234/v1/models`
2. Check the base URL matches your service configuration
3. Ensure no firewall is blocking the connection
4. Try accessing the service from a browser

### Model Not Found

**Problem**: Model not found or not loaded

**Solutions**:
1. Verify the model is loaded in your local service
2. Check the model ID/name matches what's loaded
3. List available models: `curl http://localhost:1234/v1/models`

### Tool Calling Not Working

**Problem**: Model doesn't call tools or ignores tool definitions

**Solutions**:
1. Verify your model supports tool calling (not all models do)
2. Check tool definitions are properly formatted
3. Use a model known to support tools (e.g., Mistral-7B-Instruct)
4. Review the tool description and parameters for clarity

### Image Understanding Not Working

**Problem**: Model doesn't process images

**Solutions**:
1. Verify you're using a vision model (e.g., LLaVA)
2. Check image format is supported (JPEG, PNG)
3. Verify image data is properly encoded as base64
4. Try with a smaller image size

## Performance Considerations

### Local Inference
- **GPU Memory**: Ensure sufficient VRAM for your model size
- **CPU vs GPU**: GPU inference is significantly faster
- **Model Size**: Smaller models (7B) are faster but less capable than larger models (13B, 70B)

### Optimization Tips
1. Use smaller models for faster inference
2. Reduce max_tokens for quicker responses
3. Enable GPU acceleration in your local service
4. Consider quantized models (4-bit, 8-bit) for better performance

## Examples

See the `examples/` directory for more detailed examples:
- `basic-chat.go` - Simple text generation
- `chatbot.go` - Multi-turn conversation
- `tool-calling.go` - Using tools with local models
- `vision.go` - Image understanding with LLaVA
- `streaming.go` - Streaming text generation

## API Reference

### Provider Methods

#### `New(cfg Config) *Provider`
Creates a new Open Responses provider with the given configuration.

#### `LanguageModel(modelID string) (provider.LanguageModel, error)`
Returns a language model by ID. The model ID should match the model name in your local service.

### Language Model Methods

#### `DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error)`
Performs non-streaming text generation.

#### `DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error)`
Performs streaming text generation.

#### `SupportsTools() bool`
Returns whether the model supports tool calling (returns true, actual support depends on the model).

#### `SupportsImageInput() bool`
Returns whether the model accepts image inputs (returns true, actual support depends on the model).

## Contributing

Contributions are welcome! Please see the main repository for contribution guidelines.

## License

See the main repository LICENSE file.

## Support

For issues and questions:
- GitHub Issues: https://github.com/digitallysavvy/go-ai/issues
- Documentation: https://github.com/digitallysavvy/go-ai

## Related Resources

- [LMStudio Documentation](https://lmstudio.ai/docs)
- [Ollama Documentation](https://ollama.ai/docs)
- [OpenAI Responses API Specification](https://platform.openai.com/docs/api-reference/responses)
- [AI SDK Documentation](https://github.com/digitallysavvy/go-ai)
