# Google Generative AI - Image Generation

This package provides image generation capabilities for Google Generative AI, supporting both **Imagen** and **Gemini** image models.

## Features

- ✅ **Imagen Models**: `imagen-4.0-generate-001`, `imagen-4.0-ultra-generate-001`, `imagen-4.0-fast-generate-001`
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

### Imagen Models
- `imagen-4.0-generate-001` - Latest Imagen model, highest quality
- `imagen-4.0-ultra-generate-001` - Ultra high quality
- `imagen-4.0-fast-generate-001` - Faster generation

### Gemini Image Models
- `gemini-2.5-flash-image` - Fast image generation with Gemini
- `gemini-3-pro-image-preview` - Advanced Gemini image generation

## Usage

### Basic Text-to-Image (Imagen)

```go
package main

import (
    "context"
    "os"

    "github.com/digitallysavvy/go-ai/pkg/provider"
    "github.com/digitallysavvy/go-ai/pkg/providers/google"
)

func main() {
    // Create provider
    prov := google.New(google.Config{
        APIKey: os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY"),
    })

    // Create image model
    model, _ := prov.ImageModel("imagen-4.0-generate-001")

    // Generate image
    n := 1
    result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
        Prompt: "A serene mountain landscape at sunset",
        N:      &n,
        Size:   "1024x1024", // Automatically converts to 1:1 aspect ratio
    })
    if err != nil {
        panic(err)
    }

    // Save image
    os.WriteFile("output.png", result.Image, 0644)
}
```

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
| `imagen-4.0-ultra-generate-001` | Slower | Highest | Professional/creative work |
| `imagen-4.0-generate-001` | Medium | High | General purpose |
| `imagen-4.0-fast-generate-001` | Fastest | Good | Rapid prototyping |
| `gemini-2.5-flash-image` | Very Fast | Good | Quick iterations |
| `gemini-3-pro-image-preview` | Fast | High | Advanced generation |

## Limitations

- **Imagen**: Does not support image editing through Google Generative AI API (use Vertex AI for editing)
- **Gemini**: Generates one image per request (N parameter not supported)
- **Rate Limits**: Subject to Google API quotas

## Examples

See the [examples/image-generation](../../../examples/image-generation) directory for complete examples:

- `google_imagen.go` - Basic Imagen generation
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
- [Imagen API Documentation](https://ai.google.dev/gemini-api/docs/imagen)
- [Gemini API Documentation](https://ai.google.dev/gemini-api/docs)

## License

Apache 2.0
