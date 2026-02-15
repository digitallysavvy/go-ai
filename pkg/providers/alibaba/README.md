# Alibaba Cloud Provider (Qwen & Wan)

This package provides support for Alibaba Cloud's AI services through the Go-AI SDK, including:
- **Qwen language models** (chat and vision)
- **Wan video generation models** (text-to-video, image-to-video, reference-to-video)
- **Prompt caching** for cost optimization
- **Thinking/reasoning** capabilities
- **Tool calling** for function execution

## Installation

```bash
go get github.com/digitallysavvy/go-ai
```

## Quick Start

### Authentication

Get your API key from the [Alibaba Cloud DashScope Console](https://dashscope.console.aliyun.com/).

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/alibaba"

// Option 1: Direct API key
config := alibaba.Config{
    APIKey: "your-api-key-here",
}

// Option 2: Environment variable
// Set ALIBABA_API_KEY environment variable
config, err := alibaba.NewConfig("")
if err != nil {
    log.Fatal(err)
}

provider := alibaba.New(config)
```

## Supported Models

### Chat Models (Qwen)

| Model ID | Description | Capabilities |
|----------|-------------|--------------|
| `qwen-plus` | Balanced performance | Text generation, tool calling |
| `qwen-turbo` | Fast and economical | Text generation, tool calling |
| `qwen-max` | Most capable | Text generation, tool calling |
| `qwen-qwq-32b-preview` | Reasoning model | Text generation, thinking/reasoning |
| `qwen-vl-max` | Vision + language | Text generation, image understanding |

### Video Models (Wan)

| Model ID | Type | Description |
|----------|------|-------------|
| `wan2.5-t2v` | Text-to-video | Preview version, 5-6s, 720p |
| `wan2.6-t2v` | Text-to-video | Higher quality, 5-6s |
| `wan2.6-i2v` | Image-to-video | Animate static images |
| `wan2.6-i2v-flash` | Image-to-video | Faster generation |
| `wan2.6-r2v` | Reference-to-video | Style transfer from reference |
| `wan2.6-r2v-flash` | Reference-to-video | Faster style transfer |

## Features

### Text Generation

```go
model, _ := provider.LanguageModel("qwen-plus")
result, err := model.Generate(ctx, "Write a haiku about Go programming")
```

### Vision (Image Understanding)

```go
model, _ := provider.LanguageModel("qwen-vl-max")
prompt := types.Prompt{
    Messages: []types.Message{
        {
            Role: "user",
            Content: []types.ContentPart{
                {
                    Type: "image",
                    Image: &types.ImagePart{
                        Type: "url",
                        URL:  "https://example.com/photo.jpg",
                    },
                },
                {Type: "text", Text: "What's in this image?"},
            },
        },
    },
}
result, err := model.DoGenerate(ctx, &provider.GenerateOptions{Prompt: prompt})
```

### Thinking/Reasoning

```go
model, _ := provider.LanguageModel("qwen-qwq-32b-preview")
result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
    Prompt: types.Prompt{Text: "Solve this logic puzzle..."},
    ProviderOptions: map[string]interface{}{
        "alibaba": map[string]interface{}{
            "enable_thinking":  true,
            "thinking_budget": 1000,
        },
    },
})

// Access thinking tokens
if result.Usage.OutputDetails != nil && result.Usage.OutputDetails.ReasoningTokens != nil {
    fmt.Printf("Reasoning tokens: %d\n", *result.Usage.OutputDetails.ReasoningTokens)
}
```

### Tool Calling

```go
tools := []types.Tool{
    {
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
}

result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
    Prompt: types.Prompt{Text: "What's the weather in Paris?"},
    Tools:  tools,
})

// Handle tool calls
if len(result.ToolCalls) > 0 {
    // Execute tools and send results back
}
```

### Prompt Caching

```go
// Caching is automatic for system prompts and repeated content
prompt := types.Prompt{
    System: "You are a helpful assistant...", // This will be cached
    Text:   "User question",
}

result, err := model.DoGenerate(ctx, &provider.GenerateOptions{Prompt: prompt})

// Check cache hit/miss
if result.Usage.InputDetails != nil {
    if result.Usage.InputDetails.CacheReadTokens != nil {
        fmt.Printf("Cache hit: %d tokens\n", *result.Usage.InputDetails.CacheReadTokens)
    }
    if result.Usage.InputDetails.CacheWriteTokens != nil {
        fmt.Printf("Cache write: %d tokens\n", *result.Usage.InputDetails.CacheWriteTokens)
    }
}
```

### Video Generation

```go
// Text-to-video
model, _ := provider.VideoModel("wan2.6-t2v")
duration := 5.0
result, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt:      "A dog playing in a park",
    AspectRatio: "16:9",
    Duration:    &duration,
})

// Image-to-video
model, _ = provider.VideoModel("wan2.6-i2v")
result, err = model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt:      "Add motion to the scene",
    AspectRatio: "16:9",
    Duration:    &duration,
    Image: &provider.VideoModelV3File{
        Type: "url",
        URL:  "https://example.com/image.jpg",
    },
})

// Reference-to-video (style transfer)
model, _ = provider.VideoModel("wan2.6-r2v")
result, err = model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt:      "A person walking",
    AspectRatio: "16:9",
    Duration:    &duration,
    Image: &provider.VideoModelV3File{
        Type: "url",
        URL:  "https://example.com/reference-style.jpg",
    },
})
```

## API Endpoints

- **Chat API**: `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` (OpenAI-compatible)
- **Video API**: `https://dashscope-intl.aliyuncs.com` (DashScope native)

## Status

✅ **Complete** - This provider is fully implemented with all features from PRD P0-1.

Features:
- ✅ All 5 Qwen chat models (plus, turbo, max, qwq-32b-preview, vl-max)
- ✅ All 6 Wan video models (text-to-video, image-to-video, reference-to-video)
- ✅ Thinking/reasoning with token tracking
- ✅ Prompt caching with hit/miss reporting
- ✅ Tool calling (single and parallel)
- ✅ Vision support (images)
- ✅ Streaming support
- ✅ Comprehensive test coverage
- ✅ Complete documentation with 8 examples

## Resources

- [Alibaba Cloud DashScope Documentation](https://help.aliyun.com/zh/dashscope/)
- [Qwen Model Cards](https://qwen.readthedocs.io/)
- [API Reference](https://dashscope.console.aliyun.com/api-reference)

## License

Apache-2.0 (same as parent Go-AI SDK)
