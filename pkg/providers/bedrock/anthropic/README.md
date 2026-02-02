# Amazon Bedrock Anthropic Provider

Native Anthropic Messages API implementation for AWS Bedrock, providing full access to Anthropic's latest features including computer use tools, extended thinking, and prompt caching.

## Overview

This provider calls the Anthropic Messages API directly via AWS Bedrock's `InvokeModel` endpoint, bypassing the simplified Converse API to enable complete feature parity with Anthropic's offerings.

### Why Use This Provider?

**vs Standard Bedrock Provider:**
- ✅ Computer use tools (mouse, keyboard, screenshot)
- ✅ Extended thinking capabilities
- ✅ Full prompt caching support
- ✅ Immediate access to new Anthropic features
- ✅ Native Anthropic Messages API format

**vs Direct Anthropic Provider:**
- ✅ Data stays within AWS infrastructure
- ✅ Leverage AWS pricing and credits
- ✅ Enterprise compliance requirements
- ✅ AWS IAM authentication

## Installation

```bash
go get github.com/digitallysavvy/go-ai
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    bedrockAnthropic "github.com/digitallysavvy/go-ai/pkg/providers/bedrock/anthropic"
)

func main() {
    ctx := context.Background()

    // Create provider with AWS credentials
    provider := bedrockAnthropic.New(bedrockAnthropic.Config{
        Region: "us-east-1",
        Credentials: &bedrockAnthropic.AWSCredentials{
            AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
            SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
            SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
        },
    })

    // Get language model
    model, _ := provider.LanguageModel("us.anthropic.claude-sonnet-4-5-20250929-v1:0")

    // Generate text
    result, err := ai.GenerateText(ctx, ai.GenerateOptions{
        Model:  model,
        Prompt: "Explain quantum computing in simple terms.",
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Text)
}
```

## Authentication

### Option 1: AWS Credentials (SigV4)

```go
provider := bedrockAnthropic.New(bedrockAnthropic.Config{
    Region: "us-east-1",
    Credentials: &bedrockAnthropic.AWSCredentials{
        AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
        SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
        SessionToken:    os.Getenv("AWS_SESSION_TOKEN"), // Optional
    },
})
```

### Option 2: Bearer Token

```go
provider := bedrockAnthropic.New(bedrockAnthropic.Config{
    Region:      "us-east-1",
    BearerToken: os.Getenv("AWS_BEARER_TOKEN_BEDROCK"),
})
```

## Model IDs

### Standard Format
```
anthropic.claude-{model}-{version}-v{n}:0
```

Examples:
- `anthropic.claude-sonnet-4-5-20250929-v1:0`
- `anthropic.claude-3-5-sonnet-20241022-v2:0`
- `anthropic.claude-3-haiku-20240307-v1:0`

### Inference Profile Format
```
us.anthropic.claude-{model}-{version}-v{n}:0
```

Examples:
- `us.anthropic.claude-sonnet-4-5-20250929-v1:0`
- `us.anthropic.claude-3-5-sonnet-20241022-v2:0`

## Tool Version Mapping

The provider automatically upgrades Anthropic tool versions for Bedrock compatibility:

| Standard Anthropic | Bedrock Compatible |
|-------------------|-------------------|
| `bash_20241022` | `bash_20250124` |
| `text_editor_20241022` | `text_editor_20250728` |
| `computer_20241022` | `computer_20250124` |

### Tool Name Mapping

Some tools require different names on Bedrock:

| Standard Name | Bedrock Name |
|--------------|-------------|
| `text_editor_20250728` | `str_replace_based_edit_tool` |

### Beta Headers

Computer use tools automatically add the required `anthropic_beta` headers:

| Tool Version | Beta Header |
|-------------|-------------|
| `bash_20250124`, `bash_20241022` | `computer-use-2025-01-24`, `computer-use-2024-10-22` |
| `text_editor_*` | `computer-use-2025-01-24`, `computer-use-2024-10-22` |
| `computer_20250124`, `computer_20241022` | `computer-use-2025-01-24`, `computer-use-2024-10-22` |

## Features

### Streaming

```go
stream, err := ai.StreamText(ctx, ai.StreamOptions{
    Model:  model,
    Prompt: "Write a story about...",
})
defer stream.Close()

for {
    chunk, err := stream.Next()
    if err == io.EOF {
        break
    }
    fmt.Print(chunk.Text)
}
```

### Computer Use Tools

```go
computerTool := types.Tool{
    Name:        "computer_20241022", // Automatically upgraded
    Description: "Control the computer",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "action": map[string]interface{}{
                "type": "string",
                "enum": []string{"key", "type", "mouse_move", "left_click", "screenshot"},
            },
        },
    },
    Execute: yourExecuteFunc,
}

result, err := ai.GenerateText(ctx, ai.GenerateOptions{
    Model:  model,
    Prompt: "Take a screenshot",
    Tools:  []types.Tool{computerTool},
})
```

### Prompt Caching

Bedrock Anthropic supports Anthropic's prompt caching for reduced latency and costs:

```go
result, err := ai.GenerateText(ctx, ai.GenerateOptions{
    Model:  model,
    Prompt: largeContext + "\n\nQuestion: ...",
})

// Check cache statistics
if result.Usage.InputDetails != nil {
    fmt.Printf("Cache write tokens: %d\n",
        *result.Usage.InputDetails.CacheWriteTokens)
    fmt.Printf("Cache read tokens: %d\n",
        *result.Usage.InputDetails.CacheReadTokens)
}
```

### Image Input

```go
import "encoding/base64"

imageData, _ := os.ReadFile("image.jpg")
imageBase64 := base64.StdEncoding.EncodeToString(imageData)

prompt := types.Prompt{
    Messages: []types.Message{
        {
            Role: types.RoleUser,
            Content: []types.ContentPart{
                types.NewImagePart(imageBase64, "image/jpeg"),
                types.NewTextPart("What's in this image?"),
            },
        },
    },
}

result, err := ai.GenerateText(ctx, ai.GenerateOptions{
    Model:  model,
    Prompt: prompt,
})
```

### PDF Support

```go
pdfData, _ := os.ReadFile("document.pdf")
pdfBase64 := base64.StdEncoding.EncodeToString(pdfData)

prompt := types.Prompt{
    Messages: []types.Message{
        {
            Role: types.RoleUser,
            Content: []types.ContentPart{
                types.NewFilePart(pdfBase64, "application/pdf"),
                types.NewTextPart("Summarize this document"),
            },
        },
    },
}
```

## Examples

See the [examples](../../../../examples/providers/bedrock-anthropic/) directory for complete working examples:

- **basic**: Simple text generation
- **streaming**: Streaming text output
- **tools**: Tool calling with automatic upgrades
- **computer-use**: Computer control tools
- **text-editor**: File editing tool
- **bash**: Shell command execution
- **prompt-caching**: Prompt caching for cost reduction
- **image**: Image analysis
- **pdf**: PDF document processing
- **multi-step**: Multi-step agent with tools

## Configuration Options

```go
type Config struct {
    // AWS region (required)
    Region string

    // AWS credentials for SigV4 authentication
    Credentials *AWSCredentials

    // Bearer token for alternative authentication
    BearerToken string

    // Base URL override (optional)
    // Default: https://bedrock-runtime.{region}.amazonaws.com
    BaseURL string

    // Custom HTTP client (optional)
    HTTPClient *http.Client
}

type AWSCredentials struct {
    AccessKeyID     string
    SecretAccessKey string
    SessionToken    string // Optional for temporary credentials
}
```

## Architecture

### Event Stream Transformation

Bedrock returns responses in AWS EventStream binary format (`application/vnd.amazon.eventstream`). This provider:

1. Decodes the EventStream binary format
2. Extracts base64-encoded Anthropic events from chunks
3. Transforms to SSE format for compatibility with SDK streaming
4. Handles all event types (content_block_delta, message_delta, etc.)

### Request Transformation

Before sending to Bedrock, requests are transformed:

1. Remove `model` field (included in URL)
2. Remove `stream` field (determined by endpoint)
3. Add `anthropic_version: "bedrock-2023-05-31"`
4. Upgrade tool versions
5. Map tool names
6. Add `anthropic_beta` headers for computer use tools
7. Strip unsupported fields from `tool_choice`

## Limitations

- **Structured Output**: Not supported (beta header not available on Bedrock)
- **Parallel Tool Use**: `disable_parallel_tool_use` is stripped from requests

## Comparison with Converse API

| Feature | Bedrock Anthropic Provider | Bedrock Converse API |
|---------|---------------------------|---------------------|
| Computer Use Tools | ✅ Full support | ❌ Not available |
| Extended Thinking | ✅ Supported | ❌ Not available |
| Prompt Caching | ✅ Full support | ⚠️ Limited |
| Tool Calling | ✅ Native format | ✅ Simplified format |
| Streaming | ✅ SSE format | ✅ EventStream format |
| API Format | Anthropic Messages API | AWS Converse API |
| Feature Updates | ✅ Immediate | ⏳ Delayed |

## Troubleshooting

### Authentication Errors

```
Error: no credentials provided
```

**Solution**: Provide either `Credentials` or `BearerToken` in the config.

### Model Not Found

```
Error: model not found
```

**Solution**: Verify the model ID format and ensure Bedrock access is enabled in your AWS account for the specified region.

### Event Stream Errors

```
Error: prelude CRC mismatch
```

**Solution**: This indicates network corruption. Retry the request or check network connectivity.

## License

Apache-2.0

## Related

- [Standard Anthropic Provider](../../anthropic/)
- [Bedrock Converse Provider](../)
- [Anthropic Documentation](https://docs.anthropic.com/)
- [AWS Bedrock Documentation](https://docs.aws.amazon.com/bedrock/)
