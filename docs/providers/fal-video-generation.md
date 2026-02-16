# FAL Video Generation

This guide covers video generation using the FAL provider in the Go-AI SDK.

## Overview

FAL provides high-quality video generation models through their API, including:
- Text-to-video generation
- Image-to-video animation
- Various quality and speed tiers (pro, standard, turbo)

## Setup

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/providers/fal"
)

cfg := fal.Config{
    APIKey: os.Getenv("FAL_API_KEY"),
}

prov := fal.New(cfg)
model, err := prov.VideoModel("fal-ai/kling-video/v2.5-turbo/pro/image-to-video")
```

## Image-to-Video

### Using URL

```go
result, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt:      "The cat slowly turns its head and blinks",
    AspectRatio: "16:9",
    Image: &provider.VideoModelV3File{
        Type: "url",
        URL:  "https://example.com/cat.jpg",
    },
})
```

### Using Local File

```go
// Read image file
imageData, err := os.ReadFile("cat.png")
if err != nil {
    log.Fatal(err)
}

result, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt:      "The cat slowly turns its head and blinks",
    AspectRatio: "16:9",
    Image: &provider.VideoModelV3File{
        Type:      "file",
        Data:      imageData,
        MediaType: "image/png", // Required for file uploads
    },
})
```

## Supported Image Formats

For file-based uploads, FAL supports:
- JPEG (`image/jpeg`)
- PNG (`image/png`)
- WebP (`image/webp`)
- GIF (`image/gif`)

## Implementation Details

When using file uploads:
- Image data is converted to a base64-encoded data URI
- Format: `data:<mime-type>;base64,<base64-data>`
- No separate upload step required
- Data is sent directly in the API request body

## Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `Prompt` | string | Text description of desired video animation |
| `AspectRatio` | string | Video aspect ratio (e.g., "16:9", "9:16", "1:1") |
| `Resolution` | string | Video resolution (e.g., "1920x1080", "1280x720") |
| `Duration` | *float64 | Video duration in seconds |
| `FPS` | *int | Frames per second (24, 30, 60) |
| `Seed` | *int | Seed for reproducible generation |
| `Image` | *VideoModelV3File | Input image (URL or file data) |

## Example

See complete example: `examples/providers/fal/image-to-video-file.go`

## Memory Considerations

- Large images (>10MB) are loaded into memory and base64 encoded
- Base64 encoding increases size by ~33%
- This is acceptable for typical image sizes but should be considered for very large images

## Best Practices

1. **File Size**: Keep images under 10MB for optimal performance
2. **Format**: Use JPEG or PNG for best compatibility
3. **MIME Type**: Always provide correct `MediaType` for file uploads
4. **Error Handling**: Handle encoding failures gracefully

## Troubleshooting

### "Invalid image data" error
- Verify the image file is not corrupted
- Check that the file size is reasonable (<10MB)
- Ensure the MIME type matches the actual file format

### Request timeout
- Reduce image file size
- Try a different model tier (turbo vs pro)
- Increase timeout in provider options
