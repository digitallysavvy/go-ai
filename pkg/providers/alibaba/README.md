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
// Image handling coming in ALI-T11
```

### Thinking/Reasoning

```go
model, _ := provider.LanguageModel("qwen-qwq-32b-preview")
// Thinking support coming in ALI-T10
```

### Tool Calling

```go
// Tool calling support coming in ALI-T12
```

### Prompt Caching

```go
// Caching support coming in ALI-T15-T19
```

### Video Generation

```go
model, _ := provider.VideoModel("wan2.6-t2v")
// Video generation coming in ALI-T20-T31
```

## API Endpoints

- **Chat API**: `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` (OpenAI-compatible)
- **Video API**: `https://dashscope-intl.aliyuncs.com` (DashScope native)

## Status

🚧 **Under Development** - This provider is currently being implemented as part of PRD P0-1.

Progress:
- [x] ALI-T01: Package structure
- [ ] ALI-T02: Build tooling
- [ ] ALI-T03: Provider registration
- [ ] ALI-T04: Configuration
- [ ] ALI-T05-T14: Chat language model
- [ ] ALI-T15-T19: Prompt caching
- [ ] ALI-T20-T31: Video generation
- [ ] ALI-T32-T41: Documentation & examples

## Resources

- [Alibaba Cloud DashScope Documentation](https://help.aliyun.com/zh/dashscope/)
- [Qwen Model Cards](https://qwen.readthedocs.io/)
- [API Reference](https://dashscope.console.aliyun.com/api-reference)

## License

Apache-2.0 (same as parent Go-AI SDK)
