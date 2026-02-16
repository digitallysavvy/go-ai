# Alibaba Video Generation

This guide covers video generation using the Alibaba DashScope provider in the Go-AI SDK.

## Overview

Alibaba provides video generation through their Wan models:
- **Text-to-video** (t2v): Generate videos from text descriptions
- **Image-to-video** (i2v): Animate static images
- **Reference-to-video** (r2v): Generate videos using character references

## Setup

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
)

cfg, err := alibaba.NewConfig(os.Getenv("ALIBABA_API_KEY"))
if err != nil {
    log.Fatal(err)
}

prov := alibaba.New(cfg)
```

## Available Models

| Model | Type | Description |
|-------|------|-------------|
| `wan2.6-t2v` | Text-to-video | Generate videos from text |
| `wan2.6-i2v` | Image-to-video | Animate static images (standard) |
| `wan2.6-i2v-flash` | Image-to-video | Animate static images (faster) |
| `wan2.6-r2v` | Reference-to-video | Generate with character references |
| `wan2.6-r2v-flash` | Reference-to-video | Fast reference-based generation |

## Image-to-Video

### Using URL

```go
model, err := prov.VideoModel("wan2.6-i2v-flash")
if err != nil {
    log.Fatal(err)
}

duration := 6.0
result, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt:      "Add gentle camera movement and natural motion",
    AspectRatio: "16:9",
    Duration:    &duration,
    Image: &provider.VideoModelV3File{
        Type: "url",
        URL:  "https://example.com/landscape.jpg",
    },
})
```

### Using Local File

**Important**: File uploads are only supported for Image-to-Video (i2v) models.

```go
// Read image file
imageData, err := os.ReadFile("landscape.png")
if err != nil {
    log.Fatal(err)
}

model, err := prov.VideoModel("wan2.6-i2v-flash")
if err != nil {
    log.Fatal(err)
}

duration := 6.0
result, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt:      "Add gentle camera movement and natural motion",
    AspectRatio: "16:9",
    Duration:    &duration,
    Image: &provider.VideoModelV3File{
        Type:      "file",
        Data:      imageData,
        MediaType: "image/png", // Required for file uploads
    },
})
```

## Supported Image Formats

For file-based uploads (i2v models only):
- JPEG (`image/jpeg`)
- PNG (`image/png`)
- WebP (`image/webp`)

## Implementation Details

When using file uploads with i2v models:
- Image data is converted to raw base64 encoding
- **Not** wrapped in a data URI (unlike FAL)
- Base64 string sent directly in the `img_url` field
- No separate upload step required

## Model-Specific Limitations

| Model Type | URL Support | File Upload Support |
|------------|-------------|---------------------|
| t2v | N/A | N/A |
| i2v | ✅ Yes | ✅ Yes |
| i2v-flash | ✅ Yes | ✅ Yes |
| r2v | ✅ Yes | ❌ No |
| r2v-flash | ✅ Yes | ❌ No |

Attempting to use file uploads with non-i2v models will return an error.

## Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `Prompt` | string | Text description for video generation/animation |
| `AspectRatio` | string | Video aspect ratio (e.g., "16:9", "9:16", "1:1") |
| `Duration` | *float64 | Video duration in seconds (typically 5-10s) |
| `Image` | *VideoModelV3File | Input image (URL or file for i2v models) |

## Provider-Specific Options

Access Alibaba-specific options through `ProviderOptions`:

```go
result, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt: "A cat walking",
    ProviderOptions: map[string]interface{}{
        "alibaba": map[string]interface{}{
            "negative_prompt": "blurry, distorted",
            "watermark":       false,
            "pollIntervalMs":  2000,  // Poll every 2 seconds
            "pollTimeoutMs":   300000, // 5 minute timeout
        },
    },
})
```

## Example

See complete example: `examples/providers/alibaba/09-image-to-video-file.go`

## Memory Considerations

- Large images (>10MB) are loaded into memory and base64 encoded
- Base64 encoding increases size by ~33%
- This is acceptable for typical image sizes

## Best Practices

1. **Model Selection**:
   - Use `i2v-flash` for faster generation with slightly lower quality
   - Use `i2v` for higher quality (slower generation)

2. **File Uploads**:
   - Only use with i2v models
   - Keep images under 10MB
   - Provide correct `MediaType`

3. **Prompts**:
   - Be descriptive about desired motion/animation
   - Use negative prompts to avoid unwanted effects

## Troubleshooting

### "file-based images only supported for i2v models" error
- You're trying to use file upload with a t2v or r2v model
- Solution: Use URL-based input for r2v, or switch to an i2v model

### "Invalid image data" error
- Verify the image file is not corrupted
- Check that the file size is reasonable (<10MB)
- Ensure the MIME type matches the actual file format

### Request timeout
- Reduce image file size
- Use `i2v-flash` instead of `i2v`
- Increase `pollTimeoutMs` in provider options

## Comparison with FAL

| Aspect | Alibaba | FAL |
|--------|---------|-----|
| **Format** | Raw base64 | Data URI |
| **Field Name** | `img_url` | `image_url` |
| **Model Support** | i2v models only | All image-to-video models |
| **API Efficiency** | Direct base64 | Data URI wrapper |
