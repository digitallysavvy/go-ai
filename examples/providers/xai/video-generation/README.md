# XAI Video Generation Examples

Examples demonstrating video generation capabilities with the XAI (Grok) provider.

## Prerequisites

```bash
export XAI_API_KEY="your-xai-api-key"
```

## Examples

### Text-to-Video

Generate videos from text descriptions.

```bash
go run text-to-video.go
```

**Features demonstrated:**
- Basic text-to-video generation
- Custom duration control
- Aspect ratio configuration
- Resolution selection (720p)
- Custom polling intervals

### Image-to-Video

Animate static images into videos.

```bash
export SOURCE_IMAGE_URL="https://example.com/your-image.png"
go run image-to-video.go
```

**Features demonstrated:**
- Image-to-video animation
- URL-based image input
- Custom aspect ratios
- Resolution control

**Note:** You can also use base64-encoded images instead of URLs.

### Video Editing

Modify existing videos with text instructions.

```bash
export SOURCE_VIDEO_URL="https://example.com/your-video.mp4"
go run video-editing.go
```

**Features demonstrated:**
- Video editing with text prompts
- Custom polling configuration
- Metadata extraction
- Warning handling

## Common Options

### Polling Configuration

Control how often the SDK checks video generation status:

```go
ProviderOptions: map[string]interface{}{
    "xai": map[string]interface{}{
        "pollIntervalMs": 5000,  // Check every 5 seconds
        "pollTimeoutMs":  600000, // Timeout after 10 minutes
    },
}
```

### Resolution Control

Choose output resolution:

```go
ProviderOptions: map[string]interface{}{
    "xai": map[string]interface{}{
        "resolution": "720p", // or "480p"
    },
}
```

### Duration Control

Specify video length (only for generation, not editing):

```go
duration := 5.0 // seconds
opts := &provider.VideoModelV3CallOptions{
    Duration: &duration,
    // ...
}
```

## Important Notes

1. **Async Generation**: Video generation is asynchronous. The SDK automatically polls for completion.

2. **Timeouts**: Video generation can take several minutes. Adjust `pollTimeoutMs` accordingly.

3. **Single Video**: XAI API generates one video per call. Setting N > 1 will generate a warning.

4. **Editing Limitations**: Video editing doesn't support duration, aspect ratio, or resolution changes.

## Troubleshooting

**Generation Times Out:**
- Increase `pollTimeoutMs` to allow more time
- Check your API quota and limits

**Invalid Image URL:**
- Ensure image URL is publicly accessible
- Consider using base64-encoded images for private content

**Video Quality Issues:**
- Try using 720p instead of 480p
- Adjust prompt for better results
- Ensure source image/video is high quality

## Model Information

- **Model ID**: `grok-imagine-video`
- **Supported Formats**: MP4
- **Resolutions**: 480p, 720p
- **Max Duration**: Varies by API tier

## Next Steps

- Try different prompts for various video styles
- Experiment with different aspect ratios (16:9, 9:16, 1:1)
- Combine with image generation for end-to-end workflows

## Related Documentation

- [XAI Provider README](../../../pkg/providers/xai/README.md)
- [Video Model API Reference](../../../pkg/provider/video_model_v3.go)
- [Video Generation Guide](../../../../docs/03-ai-sdk-core/38-video-generation.mdx)
