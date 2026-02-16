# XAI Image Generation Examples

Examples demonstrating image generation and editing capabilities with the XAI (Grok) provider.

## Prerequisites

```bash
export XAI_API_KEY="your-xai-api-key"
```

## Examples

### Text-to-Image

Generate images from text descriptions.

```bash
go run text-to-image.go
```

**Features demonstrated:**
- Basic text-to-image generation
- Aspect ratio control
- Output format selection
- Synchronous mode
- Image file saving

### Image Editing

Modify existing images with text instructions.

```bash
export SOURCE_IMAGE_URL="https://example.com/your-image.png"
go run image-editing.go
```

**Features demonstrated:**
- JSON-based image editing
- URL-based image input
- Natural language editing instructions
- Image file saving

### Image Inpainting

Fill masked areas with new content.

```bash
export SOURCE_IMAGE_URL="https://example.com/your-image.png"
export MASK_IMAGE_URL="https://example.com/your-mask.png"
go run image-inpainting.go
```

**Features demonstrated:**
- Inpainting with masks
- Selective area modification
- Mask-based editing

**Mask Requirements:**
- White pixels (255,255,255) indicate areas to modify
- Black pixels (0,0,0) indicate areas to preserve
- Mask should match source image dimensions

### Image Variations

Generate multiple variations of a source image.

```bash
export SOURCE_IMAGE_URL="https://example.com/your-image.png"
go run image-variations.go
```

**Features demonstrated:**
- Multiple variation generation
- Style exploration
- Composition alternatives

**Note:** Go's ImageResult returns only the first image, but Usage.ImageCount shows the total generated.

## Common Options

### Aspect Ratio

Control image dimensions:

```go
opts := &provider.ImageGenerateOptions{
    AspectRatio: "16:9", // or "1:1", "9:16", "4:3", etc.
}
```

### Output Format

Choose image format:

```go
ProviderOptions: map[string]interface{}{
    "xai": map[string]interface{}{
        "output_format": "png", // or "jpeg"
    },
}
```

### Synchronous Mode

Control generation mode:

```go
ProviderOptions: map[string]interface{}{
    "xai": map[string]interface{}{
        "sync_mode": true, // Wait for completion
    },
}
```

### Multiple Images

Generate multiple images (though Go returns first only):

```go
n := 4
opts := &provider.ImageGenerateOptions{
    N: &n,
}
```

## Image Formats

### URL-Based Images

```go
Files: []provider.ImageFile{
    {
        Type: "url",
        URL:  "https://example.com/image.png",
    },
}
```

### Base64-Encoded Images

```go
// Read image file
imageData, _ := os.ReadFile("image.png")

Files: []provider.ImageFile{
    {
        Type:      "file",
        Data:      imageData,
        MediaType: "image/png",
    },
}
```

## Important Notes

1. **Single Image Return**: Go's ImageResult only contains the first image due to interface limitations. Check `Usage.ImageCount` for total generated.

2. **Dedicated Model**: XAI has a separate dedicated image model (`grok-image-1`) different from the chat model.

3. **JSON-Based Editing**: Unlike chat-based editing, the dedicated model uses structured JSON requests.

4. **File Size Limits**: Check XAI API documentation for maximum image sizes.

## Troubleshooting

**Image Not Generating:**
- Check your API key and quota
- Verify prompt is descriptive enough
- Try different aspect ratios

**Editing Not Working:**
- Ensure source image URL is accessible
- Check image format is supported (PNG, JPEG)
- Verify image size is within limits

**Inpainting Issues:**
- Verify mask dimensions match source image
- Check mask is properly formatted (white/black pixels)
- Ensure mask clearly indicates edit areas

**Quality Issues:**
- Use PNG format for better quality
- Try more detailed prompts
- Ensure source images are high quality

## Model Information

- **Model ID**: `grok-image-1`
- **Supported Formats**: PNG, JPEG
- **Aspect Ratios**: Flexible (16:9, 1:1, 9:16, 4:3, etc.)
- **Max Images Per Call**: Varies by API tier

## Next Steps

- Experiment with different prompts and styles
- Combine editing operations for complex modifications
- Try various aspect ratios for different use cases
- Integrate with video generation for multimedia workflows

## Related Documentation

- [XAI Provider README](../../../pkg/providers/xai/README.md)
- [Image Model API Reference](../../../pkg/provider/language_model.go)
- [Image Generation Guide](../../../../docs/03-ai-sdk-core/35-image-generation.mdx)
