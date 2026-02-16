# Google Vertex AI Provider

This package provides access to Google Vertex AI models, including **Gemini language models**, **Imagen**, and **Gemini** image generation capabilities.

## Features

- ✅ **Gemini Language Models**: `gemini-1.5-pro`, `gemini-1.5-flash`, `gemini-1.5-flash-8b`, `gemini-2.0-flash-exp`
- ✅ **Text Generation**: Chat, streaming, function calling, JSON mode
- ✅ **Multi-modal Inputs**: Text, images, files (including Google Cloud Storage URLs)
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

### Gemini Language Models
- `gemini-1.5-pro` - Most capable model for complex tasks
- `gemini-1.5-flash` - Fast and efficient for most use cases
- `gemini-1.5-flash-8b` - Lightweight model for high-volume tasks
- `gemini-2.0-flash-exp` - Experimental next-generation model
- `gemini-pro` - Legacy model (deprecated, use gemini-1.5-pro)
- `gemini-pro-vision` - Legacy vision model (deprecated, use gemini-1.5-pro)

### Imagen Models (Vertex AI)
- `imagen-3.0-generate-001` - High quality image generation
- `imagen-3.0-fast-generate-001` - Faster generation
- `imagen-3.0-capability-001` - Enhanced capabilities

### Gemini Image Models
- `gemini-2.5-flash-image` - Fast Gemini image generation
- `gemini-3-pro-image-preview` - Advanced Gemini generation

## Usage

### Language Models (Chat)

#### Basic Text Generation

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/digitallysavvy/go-ai/pkg/provider"
    "github.com/digitallysavvy/go-ai/pkg/provider/types"
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

    // Create language model
    model, _ := prov.LanguageModel(googlevertex.ModelGemini15Flash)

    // Generate text
    result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
        Prompt: types.Prompt{
            Text: "Explain quantum computing in simple terms",
        },
    })
    if err != nil {
        panic(err)
    }

    fmt.Println(result.Text)
    fmt.Printf("Tokens used: %d\n", result.Usage.GetTotalTokens())
}
```

#### Streaming Text Generation

```go
// Create language model
model, _ := prov.LanguageModel(googlevertex.ModelGemini15Pro)

// Stream text
stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
    Prompt: types.Prompt{
        Messages: []types.Message{
            {
                Role: types.RoleUser,
                Content: []types.ContentPart{
                    types.TextContent{Text: "Write a story about AI"},
                },
            },
        },
    },
})
if err != nil {
    panic(err)
}
defer stream.Close()

// Process chunks using the Next() method
for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        panic(err)
    }

    if chunk.Type == provider.ChunkTypeText {
        fmt.Print(chunk.Text)
    }
}
```

**Note:** The `io.Reader` pattern (e.g., `stream.Read(buf)`) is not supported. Use the `Next()` method shown above. See the [Streaming Guide](../../../docs/guides/STREAMING.md) for more details.

#### Google Cloud Storage URLs

Vertex AI supports Google Cloud Storage (`gs://`) URLs for file inputs:

```go
result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
    Prompt: types.Prompt{
        Messages: []types.Message{
            {
                Role: types.RoleUser,
                Content: []types.ContentPart{
                    types.FileContent{
                        Type:     "url",
                        URL:      "gs://my-bucket/document.pdf",
                        MimeType: "application/pdf",
                    },
                    types.TextContent{Text: "Summarize this document"},
                },
            },
        },
    },
})
```

#### Function Calling

```go
// Define tools
tools := []types.Tool{
    {
        Type: "function",
        Function: types.ToolFunction{
            Name:        "get_weather",
            Description: "Get the weather for a location",
            Parameters: map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "location": map[string]interface{}{
                        "type":        "string",
                        "description": "The city and state",
                    },
                },
                "required": []string{"location"},
            },
        },
    },
}

// Generate with tools
result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
    Prompt: types.Prompt{
        Messages: []types.Message{
            {
                Role: types.RoleUser,
                Content: []types.ContentPart{
                    types.TextContent{Text: "What's the weather in San Francisco?"},
                },
            },
        },
    },
    Tools: tools,
})

// Check for tool calls
for _, toolCall := range result.ToolCalls {
    fmt.Printf("Tool: %s, Args: %v\n", toolCall.ToolName, toolCall.Arguments)
}
```

#### JSON Mode

```go
jsonType := "json_object"
result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
    Prompt: types.Prompt{
        Messages: []types.Message{
            {
                Role: types.RoleUser,
                Content: []types.ContentPart{
                    types.TextContent{Text: "Generate a person object with name and age"},
                },
            },
        },
    },
    ResponseFormat: &provider.ResponseFormat{
        Type: jsonType,
    },
})

// result.Text contains valid JSON
```

### Image Generation

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

See the [examples/providers/googlevertex](../../../examples/providers/googlevertex) directory:

- `01-basic-chat.go` - Basic text generation with Gemini

See also [examples/image-generation](../../../examples/image-generation):

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
