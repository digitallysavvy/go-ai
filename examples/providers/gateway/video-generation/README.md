# Gateway Video Generation Example

This example demonstrates how to generate videos using the AI Gateway provider.

## Features

- Text-to-video generation
- Provider selection and routing
- Batch video generation
- Custom video parameters (aspect ratio, duration, FPS)
- Provider-specific options

## Prerequisites

1. Set your AI Gateway API key:
```bash
export AI_GATEWAY_API_KEY="your-api-key-here"
```

2. Ensure you have credits available in your Gateway account

## Running the Example

```bash
go run main.go
```

## What This Example Shows

### Example 1: Basic Text-to-Video
Generates a single video from a text prompt using Google Veo through the gateway.

### Example 2: Video with Provider Options
Demonstrates using provider-specific options like `enhancePrompt` and custom aspect ratios.

### Example 3: Batch Generation
Shows how to generate multiple videos in a single request.

## Video Parameters

- **Prompt**: Text description of the video to generate
- **AspectRatio**: Video aspect ratio (e.g., "16:9", "1:1", "9:16")
- **Duration**: Video duration in seconds
- **FPS**: Frames per second (24, 30, 60)
- **N**: Number of videos to generate

## Gateway Features

The gateway automatically:
- Selects the best available provider
- Handles provider failover
- Optimizes for cost and performance
- Manages rate limiting and retries

## Supported Providers

The gateway supports video generation through:
- Google Vertex AI (Veo)
- FAL AI
- Replicate
- Other providers as configured

## Timeout Considerations

Video generation can take several minutes. Consider using longer timeouts:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
defer cancel()

result, err := ai.GenerateVideo(ctx, ai.GenerateVideoOptions{
    // ... options
})
```

## Error Handling

The gateway provides helpful error messages for common issues:
- Timeout errors with troubleshooting guidance
- Rate limit errors with retry-after information
- Authentication errors
- Provider-specific errors

## Output

Videos are returned as:
- **URL**: Direct link to the hosted video
- **Base64**: Encoded video data for immediate use

The provider determines which format is returned.
