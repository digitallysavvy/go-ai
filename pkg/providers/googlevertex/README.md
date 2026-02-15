# Google Vertex AI Provider

This package provides access to Google Vertex AI models, including **Imagen** and **Gemini** image generation capabilities.

## Features

- ✅ **Imagen 3.0 Models**: `imagen-3.0-generate-001`, `imagen-3.0-fast-generate-001`
- ✅ **Gemini Image Models**: `gemini-2.5-flash-image`, `gemini-3-pro-image-preview`
- ✅ **Text-to-Image Generation**: Create images from text prompts
- ✅ **Aspect Ratio Control**: 1:1, 4:3, 3:4, 16:9, 9:16
- ⚠️ **Image Editing**: Structure prepared, full implementation pending (see TODO comments)

## Installation

```bash
go get github.com/digitallysavvy/go-ai/pkg/providers/googlevertex
```

## Authentication

Google Vertex AI requires Google Cloud authentication with project and location.

### 1. Set up Google Cloud Project

```bash
# Enable Vertex AI API
gcloud services enable aiplatform.googleapis.com

# Set your project
export GOOGLE_VERTEX_PROJECT=your-project-id
export GOOGLE_VERTEX_LOCATION=us-central1
```

### 2. Get Access Token

```bash
# Get access token (valid for 1 hour)
export GOOGLE_VERTEX_ACCESS_TOKEN=$(gcloud auth print-access-token)
```

Or use a service account:
```bash
gcloud auth activate-service-account --key-file=key.json
export GOOGLE_VERTEX_ACCESS_TOKEN=$(gcloud auth print-access-token)
```

## Supported Models

### Imagen Models (Vertex AI)
- `imagen-3.0-generate-001` - High quality image generation
- `imagen-3.0-fast-generate-001` - Faster generation
- `imagen-3.0-capability-001` - Enhanced capabilities

### Gemini Image Models
- `gemini-2.5-flash-image` - Fast Gemini image generation
- `gemini-3-pro-image-preview` - Advanced Gemini generation

## Usage

### Basic Text-to-Image (Imagen)

```go
package main

import (
    "context"
    "os"

    "github.com/digitallysavvy/go-ai/pkg/provider"
    "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
)

func main() {
    // Create Vertex AI provider
    prov, err := googlevertex.New(googlevertex.Config{
        Project:     os.Getenv("GOOGLE_VERTEX_PROJECT"),
        Location:    os.Getenv("GOOGLE_VERTEX_LOCATION"),
        AccessToken: os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN"),
    })
    if err != nil {
        panic(err)
    }

    // Create image model
    model, _ := prov.ImageModel("imagen-3.0-generate-001")

    // Generate image
    n := 1
    result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
        Prompt: "A futuristic cityscape at night",
        N:      &n,
        Size:   "1920x1080", // Converts to 16:9 aspect ratio
    })
    if err != nil {
        panic(err)
    }

    // Save image
    os.WriteFile("vertex_output.png", result.Image, 0644)
}
```

### Text-to-Image with Gemini

```go
// Create Gemini image model
model, _ := prov.ImageModel("gemini-2.5-flash-image")

// Generate image
result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
    Prompt: "A magical forest with glowing mushrooms",
    Size:   "1024x1024",
})
```

### Different Aspect Ratios

```go
// Square (1:1)
result, _ := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
    Prompt: "A centered portrait",
    Size:   "1024x1024",
})

// Wide landscape (16:9)
result, _ := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
    Prompt: "Expansive desert landscape",
    Size:   "1920x1080",
})

// Tall portrait (9:16)
result, _ := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
    Prompt: "Skyscraper from ground level",
    Size:   "1080x1920",
})
```

## Configuration

### Provider Config

```go
type Config struct {
    Project     string  // Required: Google Cloud project ID
    Location    string  // Required: GCP location (e.g., "us-central1")
    AccessToken string  // Required: OAuth2 access token
    BaseURL     string  // Optional: Custom base URL
}
```

### Creating a Provider

```go
prov, err := googlevertex.New(googlevertex.Config{
    Project:     "my-project-id",
    Location:    "us-central1",
    AccessToken: "ya29.a0AfH6SMB...",
})
```

## Image Editing (Coming Soon)

The structure for image editing is prepared but not fully implemented. Once the `provider.ImageGenerateOptions` interface is extended, Vertex AI will support:

### Planned Editing Features

- **Inpainting**: Insert objects into masked regions
- **Outpainting**: Extend images beyond boundaries
- **Background Replacement**: Swap backgrounds
- **Object Removal**: Remove unwanted objects
- **Controlled Editing**: Guided image modifications

### Example Code Structure (Future)

```go
// This is the planned API structure (not yet working)
result, err := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
    Prompt: "Add a sunset sky",
    Files: []provider.ImageFile{
        {Data: originalImage, MediaType: "image/png"},
    },
    Mask: &provider.ImageFile{
        Data: maskImage, MediaType: "image/png"},
    },
    ProviderOptions: map[string]interface{}{
        "vertex": map[string]interface{}{
            "edit": map[string]interface{}{
                "mode": "EDIT_MODE_INPAINT_INSERTION",
                "baseSteps": 50,
                "maskMode": "MASK_MODE_USER_PROVIDED",
                "maskDilation": 0.01,
            },
        },
    },
})
```

See `image_model.go` TODO comments for full implementation details.

## Locations

Vertex AI is available in multiple regions:

- `us-central1` - Iowa, USA
- `us-west1` - Oregon, USA
- `europe-west4` - Netherlands
- `asia-southeast1` - Singapore

## Response Format

```go
type ImageResult struct {
    Image    []byte      // Raw image data
    MimeType string      // MIME type (e.g., "image/png")
    Usage    ImageUsage  // Usage statistics
}

type ImageUsage struct {
    ImageCount int       // Number of images generated
}
```

## Model Comparison

| Model | Speed | Quality | Best For |
|-------|-------|---------|----------|
| `imagen-3.0-generate-001` | Medium | High | Production use |
| `imagen-3.0-fast-generate-001` | Fast | Good | Rapid iteration |
| `imagen-3.0-capability-001` | Medium | Very High | Enhanced features |
| `gemini-2.5-flash-image` | Very Fast | Good | Quick generation |

## Examples

See the [examples/image-generation](../../../examples/image-generation) directory:

- `vertex_imagen.go` - Vertex AI Imagen generation
- `vertex_gemini.go` - Vertex AI Gemini generation

## Error Handling

```go
result, err := model.DoGenerate(ctx, opts)
if err != nil {
    if providerErr, ok := err.(*providererrors.ProviderError); ok {
        fmt.Printf("Error: %s (status: %d)\n",
            providerErr.Message, providerErr.StatusCode)
    }
}
```

## Common Errors

- **401 Unauthorized**: Invalid or expired access token
- **403 Forbidden**: Insufficient permissions or API not enabled
- **404 Not Found**: Invalid model ID or endpoint
- **429 Too Many Requests**: Rate limit exceeded

## Pricing

Vertex AI image generation pricing varies by model and region. See [Google Cloud Pricing](https://cloud.google.com/vertex-ai/pricing) for details.

## Resources

- [Vertex AI Documentation](https://cloud.google.com/vertex-ai/docs)
- [Imagen Documentation](https://cloud.google.com/vertex-ai/generative-ai/docs/image/overview)
- [Gemini API Documentation](https://cloud.google.com/vertex-ai/generative-ai/docs/multimodal/overview)

## License

Apache 2.0
