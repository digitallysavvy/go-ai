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

Bedrock Anthropic supports Anthropic's prompt caching with configurable Time-To-Live (TTL) for reduced latency and costs.

#### Cache TTL Options

- **5 minutes (default)**: Ideal for short interactive sessions
- **1 hour**: Ideal for longer sessions (requires Claude 4.5 Sonnet v2, Opus, or Haiku)

#### Basic Usage with Default 5m TTL

```go
provider := bedrockAnthropic.New(bedrockAnthropic.Config{
    Region: "us-east-1",
    Credentials: credentials,
    CacheConfig: bedrockAnthropic.NewCacheConfig(
        bedrockAnthropic.WithSystemCache(),
    ),
})

model, _ := provider.LanguageModel("us.anthropic.claude-sonnet-4-5-20250929-v1:0")

result, err := ai.GenerateText(ctx, ai.GenerateOptions{
    Model:  model,
    System: largeContext, // This will be cached with 5m TTL
    Prompt: "Question: ...",
})

// Check cache statistics
if result.Usage.InputDetails != nil {
    fmt.Printf("Cache write tokens: %d\n",
        *result.Usage.InputDetails.CacheWriteTokens)
    fmt.Printf("Cache read tokens: %d\n",
        *result.Usage.InputDetails.CacheReadTokens)
}
```

#### Extended 1-Hour Cache

```go
ttl := bedrockAnthropic.CacheTTL1Hour
provider := bedrockAnthropic.New(bedrockAnthropic.Config{
    Region: "us-east-1",
    Credentials: credentials,
    CacheConfig: bedrockAnthropic.NewCacheConfig(
        bedrockAnthropic.WithCacheTTL(ttl),
        bedrockAnthropic.WithSystemCache(),
    ),
})
```

#### Cache Tools

```go
provider := bedrockAnthropic.New(bedrockAnthropic.Config{
    Region: "us-east-1",
    Credentials: credentials,
    CacheConfig: bedrockAnthropic.NewCacheConfig(
        bedrockAnthropic.WithCacheTTL(bedrockAnthropic.CacheTTL1Hour),
        bedrockAnthropic.WithSystemCache(),
        bedrockAnthropic.WithToolCache(),
    ),
})

// Tool definitions will be cached for 1 hour
result, err := ai.GenerateText(ctx, ai.GenerateOptions{
    Model:  model,
    System: "You are a helpful assistant.",
    Prompt: "What's the weather?",
    Tools:  []ai.Tool{weatherTool, calculatorTool},
})
```

#### Cache Specific Messages

```go
provider := bedrockAnthropic.New(bedrockAnthropic.Config{
    Region: "us-east-1",
    Credentials: credentials,
    CacheConfig: bedrockAnthropic.NewCacheConfig(
        bedrockAnthropic.WithCacheTTL(bedrockAnthropic.CacheTTL1Hour),
        bedrockAnthropic.WithMessageCacheIndices(0, 2), // Cache messages at index 0 and 2
    ),
})
```

#### Model Support for 1-Hour Cache

The 1-hour cache TTL is supported by:
- `us.anthropic.claude-4-5-sonnet-v2:0` (Claude Sonnet 4.5 v2)
- `us.anthropic.claude-4-5-opus-20250514:0` (Claude Opus 4.5)
- `us.anthropic.claude-4-5-haiku-20250510:0` (Claude Haiku 4.5)

All caching-capable models support the default 5-minute TTL.

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

## Message Validation

Validate message structures before sending to Bedrock to catch errors early and get detailed feedback.

### Quick Start

```go
import bedrock "github.com/digitallysavvy/go-ai/pkg/providers/bedrock"

// Validate a single message
message := types.Message{
    Role: types.RoleUser,
    Content: []types.ContentPart{
        types.TextContent{Text: "Hello!"},
    },
}

if err := bedrock.ValidateMessage(message); err != nil {
    log.Fatal(err)
}
```

### Validate Multiple Messages

```go
messages := []types.Message{
    {
        Role: types.RoleUser,
        Content: []types.ContentPart{
            types.TextContent{Text: "Analyze this image"},
            types.ImageContent{
                Image:    imageData,
                MimeType: "image/png",
            },
        },
    },
    {
        Role: types.RoleAssistant,
        Content: []types.ContentPart{
            types.TextContent{Text: "I can see..."},
        },
    },
}

if err := bedrock.ValidateMessages(messages); err != nil {
    log.Fatalf("Invalid messages: %v", err)
}
```

### Validation Rules

The validator checks all content types for Bedrock compatibility:

**Text Content:**
- ✅ Text must not be empty
- ✅ Must have non-empty type

**Image Content:**
- ✅ Must have either Image data or URL
- ✅ Image data requires MimeType
- ✅ Supported MIME types: image/png, image/jpeg, image/gif, image/webp

**File Content:**
- ✅ Must have Data
- ✅ Must have MimeType
- ✅ Supported for PDF documents

**Tool Result Content:**
- ✅ Must have ToolCallID
- ✅ Must have ToolName
- ✅ Result can be any type

**Reasoning Content:**
- ✅ Text must not be empty
- ✅ Used for extended thinking blocks

### Error Messages

Validation errors include detailed context for debugging:

```go
err := bedrock.ValidateMessage(message)
// Error examples:
// "message role is required"
// "text content at index 0 is empty (role: user)"
// "image content at index 1 must have either Image data or URL (role: user)"
// "file content at index 2 missing MimeType (role: user)"
// "tool result content at index 0 missing tool call ID (role: tool)"
```

### When to Use Validation

✅ **Use validation when:**
- Building dynamic message construction
- Handling user-provided content
- Creating multi-modal messages (text + images + files)
- Debugging message format issues
- Testing message structures

❌ **Skip validation when:**
- Using static, known-good messages
- Performance is critical (validation adds minimal overhead)
- Messages are generated by trusted SDK functions

### Validation in Practice

```go
func createMultiModalMessage(text string, imagePath string) (types.Message, error) {
    imageData, err := os.ReadFile(imagePath)
    if err != nil {
        return types.Message{}, err
    }

    message := types.Message{
        Role: types.RoleUser,
        Content: []types.ContentPart{
            types.TextContent{Text: text},
            types.ImageContent{
                Image:    imageData,
                MimeType: "image/png",
            },
        },
    }

    // Validate before using
    if err := bedrock.ValidateMessage(message); err != nil {
        return types.Message{}, fmt.Errorf("invalid message: %w", err)
    }

    return message, nil
}
```

### Content Type Reference

| Content Type | Required Fields | Optional Fields | Notes |
|--------------|----------------|-----------------|-------|
| `text` | Text | - | Must not be empty |
| `image` | Image OR URL | MimeType (if using Image data) | Supports base64 or URL |
| `file` | Data, MimeType | Filename | For PDF documents |
| `tool-result` | ToolCallID, ToolName | Result, Error | Tool execution results |
| `reasoning` | Text | - | Extended thinking blocks |

### Performance

Validation is lightweight:
- **Runtime:** O(n) where n = number of content parts
- **Memory:** No allocations during validation
- **Overhead:** <1ms for typical messages
- **Fail-fast:** Returns on first error

## Examples

See the [examples](../../../../examples/providers/bedrock-anthropic/) directory for complete working examples:

- **basic**: Simple text generation
- **streaming**: Streaming text output
- **tools**: Tool calling with automatic upgrades
- **computer-use**: Computer control tools
- **text-editor**: File editing tool
- **bash**: Shell command execution
- **prompt-caching**: Prompt caching for cost reduction (default 5m TTL)
- **cache-ttl-5m**: Prompt caching with 5-minute TTL
- **cache-ttl-1h**: Prompt caching with 1-hour TTL
- **cache-with-tools**: Tool definition caching
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

    // Cache configuration for prompt caching (optional)
    CacheConfig *CacheConfig
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
