# Google Generative AI - Image Generation

This package provides image generation capabilities for Google Generative AI using **Gemini** image models.

> **Note:** Imagen (`:predict` API) models are no longer supported, matching the
> upstream TypeScript AI SDK (`ai@7.0.113`). `ImageModel()` accepts only model
> IDs starting with `gemini-`; other IDs return an error at `DoGenerate` time.

## Features

- ✅ **Gemini Image Models**: `gemini-2.5-flash-image`, `gemini-3-pro-image-preview`
- ✅ **Text-to-Image Generation**: Create images from text prompts
- ✅ **Aspect Ratio Control**: Automatic conversion from size to aspect ratio
- ✅ **Multiple Aspect Ratios**: 1:1, 4:3, 3:4, 16:9, 9:16

## Installation

```bash
go get github.com/digitallysavvy/go-ai/pkg/providers/google
```

## Authentication

You need a Google API key from [Google AI Studio](https://makersuite.google.com/app/apikey).

Set it as an environment variable:
```bash
export GOOGLE_GENERATIVE_AI_API_KEY=your-api-key
```

## Supported Models

### Gemini Image Models
- `gemini-2.5-flash-image` - Fast image generation with Gemini
- `gemini-3-pro-image-preview` - Advanced Gemini image generation

## Usage

### Text-to-Image with Gemini

```go
// Create Gemini image model
model, _ := prov.ImageModel("gemini-2.5-flash-image")

// Generate image
result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
    Prompt: "Abstract colorful art with geometric shapes",
    Size:   "1920x1080", // Converts to 16:9 aspect ratio
})
```

### Different Aspect Ratios

```go
// Square (1:1)
result, _ := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
    Prompt: "A cute robot",
    Size:   "1024x1024",
})

// Landscape (16:9)
result, _ := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
    Prompt: "Wide mountain panorama",
    Size:   "1920x1080",
})

// Portrait (9:16)
result, _ := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
    Prompt: "Tall building architecture",
    Size:   "1080x1920",
})
```

## Size to Aspect Ratio Conversion

The provider automatically converts size parameters to aspect ratios:

| Size | Aspect Ratio |
|------|--------------|
| `1024x1024`, `512x512`, `256x256` | `1:1` (Square) |
| `1024x768` | `4:3` |
| `768x1024` | `3:4` |
| `1920x1080`, `1792x1024` | `16:9` (Landscape) |
| `1080x1920`, `1024x1792` | `9:16` (Portrait) |

## Response Format

```go
type ImageResult struct {
    Image    []byte           // Raw image data (PNG/JPEG)
    MimeType string           // MIME type (e.g., "image/png")
    URL      string           // URL (if available)
    Usage    ImageUsage       // Usage statistics
}

type ImageUsage struct {
    ImageCount int             // Number of images generated
}
```

## Model Comparison

| Model | Speed | Quality | Best For |
|-------|-------|---------|----------|
| `gemini-2.5-flash-image` | Very Fast | Good | Quick iterations |
| `gemini-3-pro-image-preview` | Fast | High | Advanced generation |

## Limitations

- **Gemini**: Generates one image per request (N parameter not supported)
- **Rate Limits**: Subject to Google API quotas

## Examples

See the [examples/image-generation](../../../examples/image-generation) directory for complete examples:

- `google_gemini.go` - Gemini image generation

## Error Handling

```go
result, err := model.DoGenerate(ctx, opts)
if err != nil {
    // Check error type
    if providerErr, ok := err.(*providererrors.ProviderError); ok {
        fmt.Printf("Provider error: %s (status: %d)\n",
            providerErr.Message, providerErr.StatusCode)
    }
}
```

## API Reference

### Provider Configuration

```go
type Config struct {
    APIKey  string  // Required: Google API key
    BaseURL string  // Optional: Custom base URL
}
```

### Image Model Methods

```go
// ImageModel interface
type ImageModel interface {
    SpecificationVersion() string  // Returns "v3"
    Provider() string              // Returns "google"
    ModelID() string               // Returns the model ID
    DoGenerate(ctx context.Context, opts *ImageGenerateOptions) (*types.ImageResult, error)
}
```

## Resources

- [Google AI Studio](https://makersuite.google.com/)
- [Gemini API Documentation](https://ai.google.dev/gemini-api/docs)

## License

Apache 2.0
