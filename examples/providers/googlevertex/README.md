# Google Vertex AI Examples

This directory contains examples demonstrating various features of the Google Vertex AI provider.

## Prerequisites

Before running these examples, you need:

1. A Google Cloud project with Vertex AI API enabled
2. Authentication credentials

### Setup

```bash
# Enable Vertex AI API
gcloud services enable aiplatform.googleapis.com

# Set environment variables
export GOOGLE_VERTEX_PROJECT=your-project-id
export GOOGLE_VERTEX_LOCATION=us-central1
export GOOGLE_VERTEX_ACCESS_TOKEN=$(gcloud auth print-access-token)
```

## Examples

### 01-basic-chat.go
Basic text generation with Gemini models.

**Features:**
- Simple text prompts
- Token usage tracking
- Error handling

**Run:**
```bash
go run 01-basic-chat.go
```

### 02-streaming.go
Real-time text streaming using the `Next()` method.

**Features:**
- Streaming responses
- Chunk-by-chunk processing
- Finish reason and usage tracking

**Run:**
```bash
go run 02-streaming.go
```

### 03-tool-calling.go
Function calling (tool use) with Gemini models.

**Features:**
- Tool/function definitions
- Tool call detection
- Multi-step reasoning

**Run:**
```bash
go run 03-tool-calling.go
```

### 04-reasoning.go
Reasoning/thinking capabilities with Gemini 2.5 models.

**Features:**
- Thinking/reasoning output
- Reasoning token tracking
- Colored output (reasoning in blue)

**Run:**
```bash
go run 04-reasoning.go
```

**Note:** Requires Gemini 2.5 model access.

### 05-multimodal.go
Vision capabilities with image inputs.

**Features:**
- Image URL input
- Google Cloud Storage (GCS) URLs (Vertex AI specific!)
- Base64-encoded images
- Multi-modal prompts

**Run:**
```bash
# Basic usage with URL
go run 05-multimodal.go

# With GCS URL
export GOOGLE_VERTEX_GCS_IMAGE_URL=gs://my-bucket/image.jpg
go run 05-multimodal.go

# With local image file
export IMAGE_PATH=/path/to/image.jpg
go run 05-multimodal.go
```

## Common Patterns

### Error Handling

All examples include proper error handling:

```go
result, err := model.DoGenerate(ctx, options)
if err != nil {
    log.Fatal(err)
}
```

### Streaming Pattern

The SDK uses a `Next()`-based streaming pattern:

```go
for {
    chunk, err := stream.Next()
    if err != nil {
        if err.Error() == "EOF" {
            break
        }
        return err
    }

    switch chunk.Type {
    case provider.ChunkTypeText:
        fmt.Print(chunk.Text)
    case provider.ChunkTypeFinish:
        fmt.Println("\nDone!")
    }
}
```

**Note:** The `io.Reader` pattern is not supported. See [Streaming Guide](../../../docs/guides/STREAMING.md).

### Token Usage

All examples demonstrate token usage tracking:

```go
fmt.Printf("Token usage: %d total\n", result.Usage.GetTotalTokens())

// For detailed breakdown
if result.Usage.InputDetails != nil {
    fmt.Printf("Input tokens: %d\n", *result.Usage.InputDetails.TextTokens)
}
if result.Usage.OutputDetails != nil {
    fmt.Printf("Reasoning tokens: %d\n", *result.Usage.OutputDetails.ReasoningTokens)
}
```

## Available Models

- `gemini-1.5-pro` - Most capable, best for complex tasks
- `gemini-1.5-flash` - Fast and efficient (recommended for most use cases)
- `gemini-1.5-flash-8b` - Lightweight, high-volume tasks
- `gemini-2.0-flash-exp` - Experimental next-gen model
- `gemini-2.5-flash-preview-04-17` - Preview with reasoning capabilities

## Vertex AI vs Google AI

Key differences when using Vertex AI:

1. **Authentication**: Uses Google Cloud credentials (OAuth2 tokens)
2. **Endpoints**: Regional endpoints (`{region}-aiplatform.googleapis.com`)
3. **GCS URLs**: Supports `gs://` URLs for file inputs
4. **Enterprise features**: Access to grounding, code execution, etc.

## Resources

- [Provider Documentation](../../../pkg/providers/googlevertex/README.md)
- [Streaming Guide](../../../docs/guides/STREAMING.md)
- [Google Vertex AI Docs](https://cloud.google.com/vertex-ai/docs)

## Troubleshooting

### Access Token Expired

Access tokens expire after 1 hour. Refresh with:

```bash
export GOOGLE_VERTEX_ACCESS_TOKEN=$(gcloud auth print-access-token)
```

### Permission Denied

Ensure the Vertex AI API is enabled and you have the necessary IAM permissions:

```bash
gcloud services enable aiplatform.googleapis.com
gcloud projects add-iam-policy-binding YOUR_PROJECT_ID \
    --member="user:YOUR_EMAIL" \
    --role="roles/aiplatform.user"
```

### Model Not Found

Ensure you're using a valid model ID and the model is available in your region:

```bash
# List available models
gcloud ai models list --region=us-central1
```
