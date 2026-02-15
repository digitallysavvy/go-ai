# Moonshot AI Provider

The Moonshot AI provider for Go-AI SDK enables integration with Moonshot AI's Kimi models, supporting thinking/reasoning capabilities, prompt caching, and tool calling through an OpenAI-compatible API.

## Features

- ✅ **Multiple Models**: Support for 4 Kimi model variants with different context windows
- ✅ **Thinking/Reasoning**: K2.5 and thinking models with explicit reasoning token tracking
- ✅ **Prompt Caching**: Automatic caching with cache hit/miss token reporting
- ✅ **Tool Calling**: OpenAI-compatible function calling (single and parallel)
- ✅ **Structured Output**: JSON mode and schema-based generation
- ✅ **Comprehensive Usage Tracking**: Detailed token breakdowns for optimization

## Supported Models

| Model ID | Context Window | Thinking Support | Description |
|----------|---------------|------------------|-------------|
| `moonshot-v1-8k` | 8,192 tokens | ❌ | Standard model for short conversations |
| `moonshot-v1-32k` | 32,768 tokens | ❌ | Mid-range model for longer contexts |
| `moonshot-v1-128k` | 131,072 tokens | ❌ | Large context model for extensive documents |
| `kimi-k2` | Variable | ❌ | Kimi K2 base model |
| `kimi-k2.5` | Variable | ✅ | Kimi K2.5 with thinking/reasoning |
| `kimi-k2-thinking` | Variable | ✅ | Specialized thinking model |
| `kimi-k2-thinking-turbo` | Variable | ✅ | Fast thinking model |
| `kimi-k2-turbo` | Variable | ❌ | Fast standard model |

## Installation

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
```

## Quick Start

### Basic Chat

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
)

func main() {
	// Create provider (uses MOONSHOT_API_KEY env var)
	cfg, _ := moonshot.NewConfig("")
	prov := moonshot.New(cfg)

	// Get model
	model, _ := prov.LanguageModel("moonshot-v1-32k")

	// Generate
	temperature := 0.7
	maxTokens := 1000
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: provider.Prompt{
			System: "You are a helpful assistant.",
			Text:   "Explain quantum computing briefly",
		},
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Text)
}
```

### Thinking/Reasoning (K2.5)

```go
// Use K2.5 model with thinking enabled
model, _ := prov.LanguageModel("kimi-k2.5")

temperature := 0.7
maxTokens := 2000
result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
	Prompt: provider.Prompt{
		System: "Show your reasoning process.",
		Text:   "Solve this math problem step by step: ...",
	},
	Temperature: &temperature,
	MaxTokens:   &maxTokens,
	ProviderOptions: map[string]interface{}{
		"moonshot": map[string]interface{}{
			"thinking": map[string]interface{}{
				"type":          "enabled",      // Enable thinking
				"budget_tokens": 500,            // Max reasoning tokens
			},
			"reasoning_history": "interleaved", // How to include reasoning
		},
	},
})

// Check reasoning token usage
if result.Usage.OutputDetails != nil && result.Usage.OutputDetails.ReasoningTokens != nil {
	fmt.Printf("Reasoning tokens: %d\n", *result.Usage.OutputDetails.ReasoningTokens)
}
```

### Prompt Caching

```go
// Use 128k model for best caching performance
model, _ := prov.LanguageModel("moonshot-v1-128k")

largeSystemPrompt := "You are an expert in... [large prompt]"

// First request - cache miss
temperature := 0.7
maxTokens := 500
result1, _ := model.DoGenerate(context.Background(), &provider.GenerateOptions{
	Prompt: provider.Prompt{
		System: largeSystemPrompt,
		Text:   "Question 1",
	},
	Temperature: &temperature,
	MaxTokens:   &maxTokens,
})

// Second request with same system prompt - cache hit
result2, _ := model.DoGenerate(context.Background(), &provider.GenerateOptions{
	Prompt: provider.Prompt{
		System: largeSystemPrompt,
		Text:   "Question 2",
	},
	Temperature: &temperature,
	MaxTokens:   &maxTokens,
})

// Check cache usage
if result2.Usage.InputDetails != nil && result2.Usage.InputDetails.CacheReadTokens != nil {
	fmt.Printf("Cache hits: %d tokens\n", *result2.Usage.InputDetails.CacheReadTokens)
}
```

### Tool Calling

```go
// Define tools
tools := []provider.Tool{
	{
		Type: "function",
		Function: provider.ToolFunction{
			Name:        "get_weather",
			Description: "Get current weather",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{
						"type": "string",
						"description": "City name",
					},
				},
				"required": []string{"location"},
			},
		},
	},
}

temperature := 0.7
maxTokens := 1000
result, _ := model.DoGenerate(context.Background(), &provider.GenerateOptions{
	Prompt: provider.Prompt{
		System: "You have access to weather information.",
		Text:   "What's the weather in Tokyo?",
	},
	Temperature: &temperature,
	MaxTokens:   &maxTokens,
	Tools:       tools,
	ToolChoice: provider.ToolChoice{
		Type: "auto",
	},
})

// Process tool calls
for _, toolCall := range result.ToolCalls {
	fmt.Printf("Tool: %s, Args: %+v\n", toolCall.ToolName, toolCall.Arguments)
	// Execute tool and continue conversation
}
```

## Configuration

### Environment Variables

- `MOONSHOT_API_KEY`: Your Moonshot API key (required)

### Config Options

```go
cfg := moonshot.Config{
	APIKey:  "your-api-key",              // Required
	BaseURL: "https://api.moonshot.cn/v1", // Optional, default shown
}
```

## Provider Options

### Thinking/Reasoning Options

For K2.5 and thinking models:

```go
ProviderOptions: map[string]interface{}{
	"moonshot": map[string]interface{}{
		"thinking": map[string]interface{}{
			"type":          "enabled",  // "enabled" or "disabled"
			"budget_tokens": 500,        // Max tokens for reasoning (min: 1024)
		},
		"reasoning_history": "interleaved", // "interleaved", "preserved", or "disabled"
	},
}
```

- **type**: Enable or disable thinking mode
- **budget_tokens**: Maximum tokens allocated for reasoning (minimum 1024)
- **reasoning_history**: How to include reasoning in response
  - `interleaved`: Mix reasoning with final answer
  - `preserved`: Keep reasoning separate
  - `disabled`: Omit reasoning from response

## Usage Tracking

The provider offers detailed token usage tracking:

### Standard Usage

```go
if result.Usage.InputTokens != nil {
	fmt.Printf("Input: %d\n", *result.Usage.InputTokens)
}
if result.Usage.OutputTokens != nil {
	fmt.Printf("Output: %d\n", *result.Usage.OutputTokens)
}
if result.Usage.TotalTokens != nil {
	fmt.Printf("Total: %d\n", *result.Usage.TotalTokens)
}
```

### Cache Usage

```go
if result.Usage.InputDetails != nil {
	if result.Usage.InputDetails.CacheReadTokens != nil {
		fmt.Printf("Cache hits: %d\n", *result.Usage.InputDetails.CacheReadTokens)
	}
	if result.Usage.InputDetails.NoCacheTokens != nil {
		fmt.Printf("No cache: %d\n", *result.Usage.InputDetails.NoCacheTokens)
	}
}
```

### Reasoning Tokens

```go
if result.Usage.OutputDetails != nil {
	if result.Usage.OutputDetails.ReasoningTokens != nil {
		fmt.Printf("Reasoning: %d\n", *result.Usage.OutputDetails.ReasoningTokens)
	}
	if result.Usage.OutputDetails.TextTokens != nil {
		fmt.Printf("Text: %d\n", *result.Usage.OutputDetails.TextTokens)
	}
}
```

## Error Handling

```go
result, err := model.DoGenerate(ctx, opts)
if err != nil {
	// Check for specific error types
	if providerErr, ok := err.(*providererrors.ProviderError); ok {
		fmt.Printf("Provider error: %s\n", providerErr.Message)
	}
	return err
}
```

## Examples

See the `/examples/providers/moonshot/` directory for complete examples:

- `basic-chat/` - Basic conversation
- `thinking-reasoning/` - Using K2.5 with thinking
- `prompt-caching/` - Demonstrating cache hits
- `tool-calling/` - Function calling

## Limitations

- ❌ **Image Input**: Models don't support vision
- ❌ **Embeddings**: Use separate embedding provider

## API Reference

### Provider

#### `New(cfg Config) *Provider`

Create a new Moonshot provider.

#### `LanguageModel(modelID string) (provider.LanguageModel, error)`

Get a language model by ID. Returns error if model ID is invalid.

### Config

#### `NewConfig(apiKey string) (Config, error)`

Create configuration. If `apiKey` is empty, reads from `MOONSHOT_API_KEY` environment variable.

#### `Validate() error`

Validate configuration. Returns error if API key is missing.

### LanguageModel

#### `DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error)`

Generate text response. See usage examples above for options.

#### `DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error)`

Stream text response. Returns a TextStream that yields chunks as they arrive.

## Testing

```bash
cd pkg/providers/moonshot
go test -v
```

Test coverage: 100% (8 test functions, all passing)

## License

Apache 2.0

## Links

- [Moonshot AI Platform](https://platform.moonshot.cn/)
- [Go-AI SDK Documentation](../../README.md)
- [Examples](../../../examples/providers/moonshot/)
