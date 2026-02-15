# Alibaba Cloud (Qwen & Wan) Examples

This directory contains examples demonstrating the Alibaba Cloud provider for the Go-AI SDK.

## Prerequisites

1. **API Key**: Get your Alibaba Cloud API key from [DashScope Console](https://dashscope.console.aliyun.com/)
2. **Environment Variable**: Set your API key:
   ```bash
   export ALIBABA_API_KEY="your-api-key-here"
   ```

## Examples

### Chat Models (Qwen)

1. **01-basic-chat.go** - Simple text generation
   ```bash
   go run 01-basic-chat.go
   ```
   Demonstrates: Basic chat completion with `qwen-plus`

2. **02-thinking-reasoning.go** - Complex reasoning tasks
   ```bash
   go run 02-thinking-reasoning.go
   ```
   Demonstrates: Using `qwen-qwq-32b-preview` for logical reasoning with thinking budget

3. **03-prompt-caching.go** - Cost optimization with caching
   ```bash
   go run 03-prompt-caching.go
   ```
   Demonstrates: Prompt caching to reduce costs on repeated queries

### Video Models (Wan)

4. **04-text-to-video.go** - Generate videos from text
   ```bash
   go run 04-text-to-video.go
   ```
   Demonstrates: Text-to-video generation with `wan2.6-t2v`

5. **05-image-to-video.go** - Animate static images
   ```bash
   go run 05-image-to-video.go
   ```
   Demonstrates: Image-to-video with `wan2.6-i2v-flash`

### Advanced Features

6. **06-vision-chat.go** - Image understanding and multi-turn conversation
   ```bash
   go run 06-vision-chat.go
   ```
   Demonstrates: Vision capabilities with `qwen-vl-max`, image analysis, multi-turn chat

7. **07-tool-calling.go** - Function calling capabilities
   ```bash
   go run 07-tool-calling.go
   ```
   Demonstrates: Parallel tool calls, function execution, tool result handling

8. **08-reference-to-video.go** - Style transfer video generation
   ```bash
   go run 08-reference-to-video.go
   ```
   Demonstrates: Reference-to-video with `wan2.6-r2v`, style transfer, flash variant

## Supported Models

### Qwen Chat Models
- `qwen-plus` - Balanced performance (recommended for most use cases)
- `qwen-turbo` - Fast and economical
- `qwen-max` - Most capable
- `qwen-qwq-32b-preview` - Reasoning model with thinking tokens
- `qwen-vl-max` - Vision + language (image understanding)

### Wan Video Models
- `wan2.5-t2v` - Text-to-video (preview, 720p)
- `wan2.6-t2v` - Text-to-video (higher quality)
- `wan2.6-i2v` - Image-to-video
- `wan2.6-i2v-flash` - Image-to-video (faster)
- `wan2.6-r2v` - Reference-to-video (style transfer)
- `wan2.6-r2v-flash` - Reference-to-video (faster)

## Features Demonstrated

✅ Basic text generation
✅ Thinking/reasoning with token tracking
✅ Prompt caching for cost savings
✅ Text-to-video generation
✅ Image-to-video animation
✅ Reference-to-video style transfer
✅ Vision and image understanding
✅ Multi-turn conversations with images
✅ Tool calling (function execution)
✅ Parallel tool calls
✅ Aspect ratio control (16:9, 9:16, 1:1)
✅ Duration control (5-6 seconds)
✅ Provider-specific options
✅ Flash variants for faster generation

## Notes

- **Video generation** typically takes 1-2 minutes per video
- **Prompt caching** works automatically - the second call with the same system prompt will use cached tokens
- **Thinking tokens** are only available with `qwen-qwq-32b-preview`
- **Vision support** requires `qwen-vl-max` model

## Cost Optimization Tips

1. Use `qwen-turbo` for simple tasks (faster and cheaper)
2. Enable prompt caching for repeated system prompts
3. Use `wan2.6-i2v-flash` instead of `wan2.6-i2v` when speed is more important than quality
4. Set thinking budget limits for reasoning models to control costs

## API Reference

For full API documentation, see:
- [Provider Documentation](../../pkg/providers/alibaba/README.md)
- [Alibaba Cloud Docs](https://help.aliyun.com/zh/dashscope/)
- [Qwen Models](https://qwen.readthedocs.io/)
