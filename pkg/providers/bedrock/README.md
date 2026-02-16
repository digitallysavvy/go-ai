# Amazon Bedrock Provider

AWS Bedrock provider for the Go-AI SDK, offering access to multiple AI models through AWS infrastructure.

## Overview

The Bedrock provider package offers two approaches for working with Anthropic's Claude models on AWS Bedrock:

1. **Standard Bedrock** - Uses AWS Bedrock's Converse API
2. **[Bedrock Anthropic](anthropic/README.md)** - Direct Anthropic Messages API with full feature support

## Quick Start

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
)

func main() {
    // Create provider
    provider := bedrock.New(bedrock.Config{
        Region:            "us-east-1",
        AWSAccessKeyID:    os.Getenv("AWS_ACCESS_KEY_ID"),
        AWSSecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
    })

    // Get model
    model, err := provider.LanguageModel("anthropic.claude-sonnet-4")
    if err != nil {
        log.Fatal(err)
    }

    // Generate text
    result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
        Model:  model,
        Prompt: "Explain quantum computing",
    })
    if err != nil {
        log.Fatal(err)
    }

    log.Println(result.Text)
}
```

## Authentication

### AWS Credentials

```go
provider := bedrock.New(bedrock.Config{
    Region:             "us-east-1",
    AWSAccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
    AWSSecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    SessionToken:       "optional-session-token", // For temporary credentials
})
```

### Environment Variables

```bash
export AWS_ACCESS_KEY_ID=your_access_key
export AWS_SECRET_ACCESS_KEY=your_secret_key
export AWS_SESSION_TOKEN=your_session_token  # Optional
export AWS_REGION=us-east-1                  # Optional
```

## Message Validation

Validate message structures before sending to Bedrock to catch errors early.

### Basic Validation

```go
import bedrock "github.com/digitallysavvy/go-ai/pkg/providers/bedrock"

message := types.Message{
    Role: types.RoleUser,
    Content: []types.ContentPart{
        types.TextContent{Text: "Hello!"},
    },
}

// Validate single message
if err := bedrock.ValidateMessage(message); err != nil {
    log.Fatalf("Invalid message: %v", err)
}

// Validate multiple messages
messages := []types.Message{message, assistantMessage}
if err := bedrock.ValidateMessages(messages); err != nil {
    log.Fatalf("Invalid messages: %v", err)
}
```

### Multi-Modal Validation

The validator handles all content types:

```go
message := types.Message{
    Role: types.RoleUser,
    Content: []types.ContentPart{
        types.TextContent{Text: "Analyze this image:"},
        types.ImageContent{
            Image:    imageBytes,
            MimeType: "image/png",
        },
        types.FileContent{
            Data:     pdfBytes,
            MimeType: "application/pdf",
            Filename: "document.pdf",
        },
    },
}

if err := bedrock.ValidateMessage(message); err != nil {
    // Detailed error with field path and index
    // e.g., "image content at index 1 missing MimeType (role: user)"
    log.Fatal(err)
}
```

### Validation Rules

**All Messages:**
- Role must not be empty
- Must have at least one content part

**Text Content:**
- Text must not be empty

**Image Content:**
- Must have either Image data OR URL
- If using Image data, must have MimeType
- Valid MIME types: image/png, image/jpeg, image/gif, image/webp

**File Content:**
- Must have Data
- Must have MimeType
- Filename is optional

**Tool Result Content:**
- Must have ToolCallID
- Must have ToolName

**Reasoning Content:**
- Text must not be empty

### Error Messages

Validation provides detailed error messages:

```go
// Examples of validation errors:
// "message role is required"
// "message content cannot be empty (role: user)"
// "text content at index 0 is empty (role: user)"
// "image content at index 1 must have either Image data or URL (role: user)"
// "image content at index 1 with Image data missing MimeType (role: user)"
// "file content at index 2 has empty Data (role: user)"
// "file content at index 2 missing MimeType (role: user)"
// "tool result content at index 0 missing tool call ID (role: tool)"
// "reasoning content at index 0 is empty (role: assistant)"
```

### When to Use

✅ **Validate when:**
- Constructing messages dynamically
- Processing user input
- Creating multi-modal messages
- Debugging message issues
- Building message templates

❌ **Skip when:**
- Using SDK-generated messages
- Performance is critical (though overhead is minimal)
- Messages are hardcoded and tested

### Validation Functions

```go
// Validate single message
func ValidateMessage(msg types.Message) error

// Validate multiple messages
func ValidateMessages(messages []types.Message) error
```

## Available Models

### Anthropic Claude
- `anthropic.claude-opus-4-20250514`
- `anthropic.claude-sonnet-4-20250514`
- `anthropic.claude-3-5-sonnet-20241022`
- `anthropic.claude-3-opus-20240229`

### Other Providers
- Amazon Titan models
- Meta Llama models
- Cohere models
- AI21 Labs models

## Features

- ✅ Text generation (streaming and non-streaming)
- ✅ Multi-modal support (text, images, documents)
- ✅ Tool calling
- ✅ Message validation
- ✅ AWS IAM authentication
- ✅ Multiple model providers
- ✅ Embedding models
- ✅ Image generation

## Providers

### Bedrock Anthropic

For full Anthropic feature support including computer use, extended thinking, and advanced caching:

```go
import bedrockAnthropic "github.com/digitallysavvy/go-ai/pkg/providers/bedrock/anthropic"

provider := bedrockAnthropic.New(bedrockAnthropic.Config{
    Region: "us-east-1",
    // ... credentials
})
```

See [bedrock/anthropic/README.md](anthropic/README.md) for complete documentation.

## Configuration

### Config Options

```go
type Config struct {
    // AWS Region (required)
    Region string

    // AWS Credentials (required if not using IAM role)
    AWSAccessKeyID     string
    AWSSecretAccessKey string
    SessionToken       string // Optional, for temporary credentials

    // Optional overrides
    BaseURL string // Custom Bedrock endpoint
}
```

### IAM Permissions

Required IAM permissions:
```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "bedrock:InvokeModel",
                "bedrock:InvokeModelWithResponseStream"
            ],
            "Resource": "arn:aws:bedrock:*:*:model/*"
        }
    ]
}
```

## Streaming

```go
stream, err := ai.StreamText(context.Background(), ai.StreamTextOptions{
    Model:  model,
    Prompt: "Write a story",
})
if err != nil {
    log.Fatal(err)
}

for chunk := range stream.TextStream() {
    fmt.Print(chunk)
}

if err := stream.Err(); err != nil {
    log.Fatal(err)
}
```

**Note:** The `io.Reader` pattern (e.g., `stream.Read(buf)`) is not supported. Use the `Next()` method or `TextStream()` channel pattern shown above. See the [Streaming Guide](../../../docs/guides/STREAMING.md) for more details.

## Embeddings

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/bedrock"

// Create embedding model
embeddingModel, err := provider.EmbeddingModel("cohere.embed-english-v3")
if err != nil {
    log.Fatal(err)
}

// Generate embeddings
result, err := ai.Embed(context.Background(), ai.EmbedOptions{
    Model:  embeddingModel,
    Values: []string{"Hello world", "Goodbye world"},
})
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Embeddings: %d dimensions\n", len(result.Embeddings[0]))
```

### Embedding Models

**Cohere:**
- `cohere.embed-english-v3` - English text embeddings
- `cohere.embed-multilingual-v3` - Multilingual embeddings

**Amazon Titan:**
- `amazon.titan-embed-text-v1` - Text embeddings
- `amazon.titan-embed-text-v2` - Enhanced text embeddings

### Cohere Embedding Options

Control embedding dimensions for Cohere models:

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/bedrock"

embeddingModel, err := provider.EmbeddingModelWithOptions(
    "cohere.embed-english-v3",
    &bedrock.CohereEmbeddingOptions{
        OutputDimension: bedrock.CohereOutputDimension1024,
        InputType:       bedrock.CohereInputTypeSearchQuery,
    },
)

// Available dimensions
// - CohereOutputDimension256
// - CohereOutputDimension512
// - CohereOutputDimension1024
// - CohereOutputDimension1536

// Available input types
// - CohereInputTypeSearchQuery
// - CohereInputTypeSearchDocument
// - CohereInputTypeClassification
// - CohereInputTypeClustering
```

## Examples

See the `/examples/providers/bedrock/` and `/examples/providers/bedrock-anthropic/` directories.

## Testing

```bash
cd pkg/providers/bedrock
go test -v ./...
```

## Documentation

- [AWS Bedrock Documentation](https://docs.aws.amazon.com/bedrock/)
- [Bedrock Anthropic Provider](anthropic/README.md)
- [Anthropic API Docs](https://docs.anthropic.com)

## License

See the main repository LICENSE file.
